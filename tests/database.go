package tests

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/db"
	"github.com/navidrome/navidrome/utils/singleton"
)

var databaseUsed atomic.Bool
var databaseSchemas sync.Map
var testDatabaseURL string

func registerDatabaseCleanup(t testing.TB) {
	t.Cleanup(func() {
		if !databaseUsed.Swap(false) {
			return
		}
		ctx := context.Background()
		admin, err := sql.Open(db.Driver, testDatabaseURL)
		if err != nil {
			t.Errorf("opening test cleanup connection: %v", err)
			return
		}
		defer admin.Close()
		databaseSchemas.Range(func(key, _ any) bool {
			schema := key.(string)
			for _, name := range []string{schema + "_snapshot", schema} {
				if _, err := admin.ExecContext(ctx, "DROP SCHEMA IF EXISTS "+pgx.Identifier{name}.Sanitize()+" CASCADE"); err != nil {
					t.Errorf("cleaning test schema: %v", err)
				}
			}
			databaseSchemas.Delete(key)
			return true
		})
		db.Close(ctx)
		singleton.DeleteInstance[*sql.DB]()
	})
}

// InitDB initializes the process's isolated PostgreSQL schema. The suite owns
// the shared pool; individual examples must not close it while others need it.
func InitDB(ctx context.Context) {
	if !strings.HasPrefix(conf.Server.DatabaseSchema, "navidrome_test_") && !strings.HasPrefix(conf.Server.DatabaseSchema, "navidrome_e2e_") {
		panic("database tests require an isolated PostgreSQL schema")
	}
	databaseSchemas.Store(conf.Server.DatabaseSchema, true)
	databaseUsed.Store(true)
	db.Init(ctx)
	if _, err := db.Db().ExecContext(ctx, "UPDATE library SET path=$1 WHERE id=1 AND path=''", conf.Server.MusicFolder); err != nil {
		panic(err)
	}
}

// ClearDB resets only explicitly isolated test schemas.
func ClearDB() error {
	if !strings.HasPrefix(conf.Server.DatabaseSchema, "navidrome_test_") && !strings.HasPrefix(conf.Server.DatabaseSchema, "navidrome_e2e_") {
		return fmt.Errorf("database reset requires an isolated test schema")
	}
	schema := pgx.Identifier{conf.Server.DatabaseSchema}.Sanitize()
	_, err := db.Db().ExecContext(context.Background(), "DROP SCHEMA IF EXISTS "+schema+" CASCADE; CREATE SCHEMA "+schema)
	return err
}

type snapshotTable struct {
	name, columns string
	identities    []string
}

// DatabaseSnapshot keeps fixtures in an isolated PostgreSQL schema. Generated
// columns are rebuilt and identity sequences follow the restored rows.
type DatabaseSnapshot struct {
	source string
	schema string
	tables []snapshotTable
}

func CaptureDatabase(ctx context.Context) (*DatabaseSnapshot, error) {
	if !strings.HasPrefix(conf.Server.DatabaseSchema, "navidrome_test_") && !strings.HasPrefix(conf.Server.DatabaseSchema, "navidrome_e2e_") {
		return nil, fmt.Errorf("database snapshots require an isolated test schema")
	}
	var source string
	if err := db.Db().QueryRowContext(ctx, "SELECT current_schema()").Scan(&source); err != nil {
		return nil, err
	}
	if source != conf.Server.DatabaseSchema {
		return nil, fmt.Errorf("snapshot schema %q differs from configured schema %q", source, conf.Server.DatabaseSchema)
	}
	snapshot := &DatabaseSnapshot{source: source, schema: source + "_snapshot"}
	rows, err := db.Db().QueryContext(ctx, "SELECT tablename FROM pg_tables WHERE schemaname=current_schema() AND tablename <> 'goose_db_version' ORDER BY tablename")
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var table snapshotTable
		if err := rows.Scan(&table.name); err != nil {
			rows.Close()
			return nil, err
		}
		snapshot.tables = append(snapshot.tables, table)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if _, err = db.Db().ExecContext(ctx, "CREATE SCHEMA "+pgx.Identifier{snapshot.schema}.Sanitize()); err != nil {
		return nil, err
	}
	for i := range snapshot.tables {
		table := &snapshot.tables[i]
		columns, err := db.Db().QueryContext(ctx, `SELECT column_name,is_identity FROM information_schema.columns WHERE table_schema=current_schema() AND table_name=$1 AND is_generated='NEVER' ORDER BY ordinal_position`, table.name)
		if err != nil {
			return nil, err
		}
		var names []string
		for columns.Next() {
			var name, identity string
			if err := columns.Scan(&name, &identity); err != nil {
				columns.Close()
				return nil, err
			}
			names = append(names, pgx.Identifier{name}.Sanitize())
			if identity == "YES" {
				table.identities = append(table.identities, name)
			}
		}
		err = columns.Err()
		columns.Close()
		if err != nil {
			return nil, err
		}
		table.columns = strings.Join(names, ",")
		if _, err = db.Db().ExecContext(ctx, "CREATE TABLE "+pgx.Identifier{snapshot.schema, table.name}.Sanitize()+" AS SELECT * FROM "+pgx.Identifier{table.name}.Sanitize()); err != nil {
			return nil, err
		}
	}
	return snapshot, nil
}

func (s *DatabaseSnapshot) Restore(ctx context.Context) error {
	tx, err := db.Db().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "SET LOCAL session_replication_role = replica"); err != nil {
		return err
	}
	var deletes []string
	for _, table := range s.tables {
		deletes = append(deletes, "DELETE FROM "+pgx.Identifier{s.source, table.name}.Sanitize())
	}
	if _, err = tx.ExecContext(ctx, strings.Join(deletes, ";")); err != nil {
		return err
	}
	for _, table := range s.tables {
		name := pgx.Identifier{s.source, table.name}.Sanitize()
		if _, err = tx.ExecContext(ctx, "INSERT INTO "+name+" ("+table.columns+") OVERRIDING SYSTEM VALUE SELECT "+table.columns+" FROM "+pgx.Identifier{s.schema, table.name}.Sanitize()); err != nil {
			return err
		}
		for _, identity := range table.identities {
			column := pgx.Identifier{identity}.Sanitize()
			if _, err = tx.ExecContext(ctx, "SELECT setval(pg_get_serial_sequence($1,$2),coalesce(max("+column+"),1),count(*)>0) FROM "+name, name, identity); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
