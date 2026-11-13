package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/navidrome/navidrome/conf"
)

// IntegrityCheck inspects PostgreSQL index validity. Physical page checks belong
// to PostgreSQL's amcheck tooling and are not inferred from this catalog check.
func IntegrityCheck(ctx context.Context, database *sql.DB) ([]string, bool, error) {
	rows, err := database.QueryContext(ctx, `SELECT indexrelid::regclass::text FROM pg_index i
        JOIN pg_class c ON c.oid=i.indexrelid JOIN pg_namespace n ON n.oid=c.relnamespace
        WHERE n.nspname=$1 AND NOT i.indisvalid`, conf.Server.DatabaseSchema)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	var issues []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, false, err
		}
		issues = append(issues, "Invalid index: "+name)
	}
	return issues, false, rows.Err()
}

type FKViolation struct {
	Table, Parent string
	Count         int64
}

func ForeignKeyCheck(ctx context.Context, database *sql.DB) ([]FKViolation, error) {
	rows, err := database.QueryContext(ctx, `SELECT child.relname,pns.nspname,parent.relname,
        array_to_json(ARRAY(SELECT a.attname FROM unnest(c.conkey) WITH ORDINALITY k(num,pos)
            JOIN pg_attribute a ON a.attrelid=c.conrelid AND a.attnum=k.num ORDER BY k.pos))::text,
        array_to_json(ARRAY(SELECT a.attname FROM unnest(c.confkey) WITH ORDINALITY k(num,pos)
            JOIN pg_attribute a ON a.attrelid=c.confrelid AND a.attnum=k.num ORDER BY k.pos))::text
        FROM pg_constraint c JOIN pg_class child ON child.oid=c.conrelid
        JOIN pg_namespace ns ON ns.oid=child.relnamespace JOIN pg_class parent ON parent.oid=c.confrelid
        JOIN pg_namespace pns ON pns.oid=parent.relnamespace WHERE c.contype='f' AND ns.nspname=$1`, conf.Server.DatabaseSchema)
	if err != nil {
		return nil, err
	}
	type relation struct{ table, parentSchema, parent, columns, parentColumns string }
	var relations []relation
	for rows.Next() {
		var rel relation
		if err := rows.Scan(&rel.table, &rel.parentSchema, &rel.parent, &rel.columns, &rel.parentColumns); err != nil {
			rows.Close()
			return nil, err
		}
		relations = append(relations, rel)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	var violations []FKViolation
	for _, rel := range relations {
		var columns, parentColumns []string
		if err := json.Unmarshal([]byte(rel.columns), &columns); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(rel.parentColumns), &parentColumns); err != nil {
			return nil, err
		}
		var present, matches []string
		for i, column := range columns {
			source := "child." + pgx.Identifier{column}.Sanitize()
			present = append(present, source+" IS NOT NULL")
			matches = append(matches, "parent."+pgx.Identifier{parentColumns[i]}.Sanitize()+" = "+source)
		}
		query := "SELECT count(*) FROM " + pgx.Identifier{conf.Server.DatabaseSchema, rel.table}.Sanitize() + " child WHERE " + strings.Join(present, " AND ") + " AND NOT EXISTS (SELECT 1 FROM " + pgx.Identifier{rel.parentSchema, rel.parent}.Sanitize() + " parent WHERE " + strings.Join(matches, " AND ") + ")"
		var count int64
		if err := database.QueryRowContext(ctx, query).Scan(&count); err != nil {
			return nil, err
		}
		if count > 0 {
			violations = append(violations, FKViolation{Table: rel.table, Parent: rel.parent, Count: count})
		}
	}
	return violations, nil
}

func IsFTSCorruptionOnly(issues []string) bool {
	if len(issues) == 0 {
		return false
	}
	for _, issue := range issues {
		if !strings.Contains(issue, "_search_") && !strings.Contains(issue, "_trgm_idx") {
			return false
		}
	}
	return true
}

func VerifyFTS(ctx context.Context, database *sql.DB) error {
	for _, table := range []string{"artist", "album", "media_file"} {
		var valid bool
		err := database.QueryRowContext(ctx, `SELECT i.indisvalid FROM pg_index i JOIN pg_class c ON c.oid=i.indexrelid
            JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1 AND c.relname=$2`, conf.Server.DatabaseSchema, table+"_search_document_idx").Scan(&valid)
		if err != nil {
			return err
		}
		if !valid {
			return fmt.Errorf("invalid search index for %s", table)
		}
	}
	return nil
}

func RebuildFTS(ctx context.Context, database *sql.DB) error {
	rows, err := database.QueryContext(ctx, `SELECT indexname FROM pg_indexes WHERE schemaname=$1
        AND (indexname LIKE '%_search_%' OR indexname LIKE '%_trgm_idx') ORDER BY indexname`, conf.Server.DatabaseSchema)
	if err != nil {
		return err
	}
	var indexes []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		indexes = append(indexes, name)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, name := range indexes {
		if _, err := database.ExecContext(ctx, "REINDEX INDEX "+pgx.Identifier{conf.Server.DatabaseSchema, name}.Sanitize()); err != nil {
			return err
		}
	}
	return VerifyFTS(ctx, database)
}
