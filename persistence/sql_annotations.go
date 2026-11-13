package persistence

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	. "github.com/Masterminds/squirrel"
	"github.com/fatih/structs"
	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/consts"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
)

const annotationTable = "annotation"

// annotationColumns are the columns withAnnotation's LEFT JOIN contributes, derived from
// model.Annotations so the set tracks schema changes. average_rating is excluded: it lives on the
// base table, not the annotation join.
var annotationColumns = sync.OnceValue(func() map[string]struct{} {
	cols := map[string]struct{}{}
	for name := range structs.Map(model.Annotations{}) {
		if name == "average_rating" {
			continue
		}
		cols[name] = struct{}{}
	}
	return cols
})

// annotationColumnRE matches any annotation column as a whole word. The word boundaries keep the
// base-table column average_rating from matching the annotation column rating (Go's \b treats '_'
// as a word char). Matching is case-insensitive so SQL keyword casing does not affect detection.
var annotationColumnRE = sync.OnceValue(func() *regexp.Regexp {
	cols := make([]string, 0, len(annotationColumns()))
	for col := range annotationColumns() {
		cols = append(cols, regexp.QuoteMeta(col))
	}
	sort.Strings(cols) // map iteration is random; sort for a stable pattern
	return regexp.MustCompile(`(?i)\b(?:` + strings.Join(cols, "|") + `)\b`)
})

// filtersNeedAnnotation reports whether the rendered query references an annotation column, i.e.
// whether the annotation LEFT JOIN must be kept. Scanning the rendered SQL catches every filter
// path. The placeholder column is needed because squirrel won't render a column-less SELECT; on a
// render error, keep the join to be safe.
func filtersNeedAnnotation(query SelectBuilder) bool {
	sql, _, err := query.Columns("1").ToSql()
	if err != nil {
		return true
	}
	return annotationColumnRE().MatchString(sql)
}

func (r sqlRepository) withAnnotation(ctx context.Context, query SelectBuilder, idField string) SelectBuilder {
	userID := loggedUser(ctx).ID
	if userID == invalidUserId {
		return query.Columns(fmt.Sprintf("%s.average_rating", r.tableName))
	}
	query = query.
		LeftJoin("annotation on ("+
			"annotation.item_id = "+idField+
			" AND annotation.item_type = ?"+
			" AND annotation.user_id = ?)", r.tableName, userID).
		Columns(
			"coalesce(starred, false) as starred",
			"coalesce(rating, 0) as rating",
			"starred_at",
			"play_date",
			"rated_at",
		)
	if conf.Server.AlbumPlayCountMode == consts.AlbumPlayCountModeNormalized && r.tableName == "album" {
		query = query.Columns(
			fmt.Sprintf("round(coalesce(round(cast(play_count as numeric) / nullif(%[1]s.song_count, 0), 1), 0)) as play_count", r.tableName),
		)
	} else {
		query = query.Columns("coalesce(play_count, 0) as play_count")
	}

	query = query.Columns(fmt.Sprintf("%s.average_rating", r.tableName))

	if r.tableName == "artist" {
		query = query.GroupBy("annotation.user_id", "annotation.item_id", "annotation.item_type")
	}
	return query
}

func annotationBoolFilter(field string) func(string, any) Sqlizer {
	return func(_ string, value any) Sqlizer {
		v, ok := value.(string)
		if !ok {
			return nil
		}
		if field == "starred" {
			return Eq{"coalesce(starred, false)": strings.EqualFold(v, "true")}
		}
		if strings.ToLower(v) == "true" {
			return Expr(fmt.Sprintf("COALESCE(%s, 0) > 0", field))
		}
		return Expr(fmt.Sprintf("COALESCE(%s, 0) = 0", field))
	}
}

func (r sqlRepository) annId(ctx context.Context, itemID ...string) And {
	userID := loggedUser(ctx).ID
	return And{
		Eq{annotationTable + ".user_id": userID},
		Eq{annotationTable + ".item_type": r.tableName},
		Eq{annotationTable + ".item_id": itemID},
	}
}

func (r sqlRepository) annUpsert(ctx context.Context, values map[string]any, itemIDs ...string) error {
	assignments := make([]string, 0, len(values))
	for key := range values {
		assignments = append(assignments, key+" = EXCLUDED."+key)
	}
	sort.Strings(assignments)
	for _, itemID := range itemIDs {
		row := make(map[string]any, len(values)+3)
		for key, value := range values {
			row[key] = value
		}
		row["user_id"] = loggedUser(ctx).ID
		row["item_type"] = r.tableName
		row["item_id"] = itemID
		if _, err := r.executeSQL(ctx, Insert(annotationTable).SetMap(row).Suffix(
			"ON CONFLICT (user_id, item_id, item_type) DO UPDATE SET "+strings.Join(assignments, ", "))); err != nil {
			return err
		}
	}
	return nil
}

func (r sqlRepository) SetStar(ctx context.Context, starred bool, ids ...string) error {
	starredAt := databaseNow()
	return r.annUpsert(ctx, map[string]any{"starred": starred, "starred_at": starredAt}, ids...)
}

func (r sqlRepository) SetRating(ctx context.Context, rating int, itemID string) error {
	return r.withTx(ctx, func(tx sqlRepository) error {
		var item struct{ ID string }
		if err := tx.queryOne(ctx, Select("id").From(r.tableName).Where(Eq{"id": itemID}).Suffix("FOR UPDATE"), &item); err != nil {
			return err
		}
		if err := tx.annUpsert(ctx, map[string]any{"rating": rating, "rated_at": databaseNow()}, itemID); err != nil {
			return err
		}
		return tx.updateAvgRating(ctx, itemID)
	})
}

func (r sqlRepository) updateAvgRating(ctx context.Context, itemID string) error {
	upd := Update(r.tableName).
		Where(Eq{"id": itemID}).
		Set("average_rating", Expr(
			"coalesce((select round(avg(rating), 2) from annotation where item_id = ? and item_type = ? and rating > 0), 0)",
			itemID, r.tableName,
		))
	_, err := r.executeSQL(ctx, upd)
	return err
}

func (r sqlRepository) IncPlayCount(ctx context.Context, itemID string, ts time.Time) error {
	_, err := r.executeSQL(ctx, Insert(annotationTable).SetMap(map[string]any{
		"user_id": loggedUser(ctx).ID, "item_type": r.tableName, "item_id": itemID, "play_count": 1, "play_date": ts,
	}).Suffix(`ON CONFLICT (user_id, item_id, item_type) DO UPDATE SET
        play_count = coalesce(annotation.play_count, 0) + 1,
        play_date = greatest(annotation.play_date, EXCLUDED.play_date)`))
	return err
}

func (r sqlRepository) ReassignAnnotation(ctx context.Context, prevID string, newID string) error {
	if prevID == newID || prevID == "" || newID == "" {
		return nil
	}
	upd := Expr(`WITH moved AS (
        DELETE FROM annotation WHERE item_type = ? AND item_id = ? RETURNING *
    ) INSERT INTO annotation (user_id, item_id, item_type, play_count, play_date, rating, starred, starred_at, rated_at)
      SELECT user_id, ?, item_type, play_count, play_date, rating, starred, starred_at, rated_at FROM moved
      ON CONFLICT (user_id, item_id, item_type) DO NOTHING`, r.tableName, prevID, newID)
	if _, err := r.executeSQL(ctx, upd); err != nil {
		return err
	}
	// The moved rows change newID's rating population, so its cached average no longer matches
	return r.updateAvgRating(ctx, newID)
}

func (r sqlRepository) cleanAnnotations(ctx context.Context) error {
	del := Delete(annotationTable).Where(Eq{"item_type": r.tableName}).Where("item_id not in (select id from " + r.tableName + ")")
	c, err := r.executeSQL(ctx, del)
	if err != nil {
		return fmt.Errorf("error cleaning up %s annotations: %w", r.tableName, err)
	}
	if c > 0 {
		log.Debug(ctx, "Clean-up annotations", "table", r.tableName, "totalDeleted", c)
	}
	return nil
}
