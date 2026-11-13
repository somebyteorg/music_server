package persistence

import (
	"context"
	"fmt"
	"slices"
	"time"

	. "github.com/Masterminds/squirrel"
	"github.com/deluan/rest"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/pocketbase/dbx"
)

type tagRepository struct {
	*baseTagRepository
}

func NewTagRepository(db dbx.Builder) model.TagRepository {
	return &tagRepository{
		baseTagRepository: newBaseTagRepository(db, nil), // nil = no filter, works with all tags
	}
}

func (r *tagRepository) Add(ctx context.Context, libraryID int, tags ...model.Tag) error {
	for chunk := range slices.Chunk(tags, 200) {
		sq := Insert(r.tableName).Columns("id", "tag_name", "tag_value").
			Suffix("on conflict (id) do nothing")
		for _, t := range chunk {
			sq = sq.Values(t.ID, t.TagName, t.TagValue)
		}
		_, err := r.executeSQL(ctx, sq)
		if err != nil {
			return err
		}

		// Create library_tag entries for library filtering
		libSq := Insert("library_tag").Columns("tag_id", "library_id", "album_count", "media_file_count").
			Suffix("on conflict (tag_id, library_id) do nothing")
		for _, t := range chunk {
			libSq = libSq.Values(t.ID, libraryID, 0, 0)
		}
		_, err = r.executeSQL(ctx, libSq)
		if err != nil {
			return fmt.Errorf("adding library_tag entries: %w", err)
		}
	}
	return nil
}

// UpdateCounts updates the library_tag table with per-library statistics.
// Only genres are being updated for now.
func (r *tagRepository) UpdateCounts(ctx context.Context) error {
	template := `
INSERT INTO library_tag (tag_id, library_id, %[1]s_count)
SELECT it.tag_id, %[1]s.library_id, count(distinct %[1]s.id) as %[1]s_count
FROM %[1]s
JOIN %[1]s_tags it ON it.%[1]s_id = %[1]s.id
JOIN tag ON tag.id = it.tag_id AND tag.tag_name = 'genre'
GROUP BY it.tag_id, %[1]s.library_id
ON CONFLICT (tag_id, library_id) 
DO UPDATE SET %[1]s_count = excluded.%[1]s_count;
`

	for _, table := range []string{"album", "media_file"} {
		start := time.Now()
		query := Expr(fmt.Sprintf(template, table))
		c, err := r.executeSQL(ctx, query)
		log.Debug(ctx, "Updated library tag counts", "table", table, "elapsed", time.Since(start), "updated", c)
		if err != nil {
			return fmt.Errorf("updating %s library tag counts: %w", table, err)
		}
	}
	return nil
}

func (r *tagRepository) GetAll(ctx context.Context, name model.TagName, options ...model.QueryOptions) (model.TagList, error) {
	sq := r.newSelect(ctx, options...).Where(Eq{"tag.tag_name": name})
	res := model.TagList{}
	err := r.queryAll(ctx, sq, &res)
	return res, err
}

func (r *tagRepository) purgeUnused(ctx context.Context) error {
	// Junction tables only materialize selected tag types. Collect every JSON
	// reference once so other metadata tags survive cleanup too.
	del := Delete(r.tableName).Prefix(`WITH used_tags AS MATERIALIZED (
        SELECT t.id FROM album
        CROSS JOIN LATERAL jsonb_each(album.tags) AS roles
        CROSS JOIN LATERAL jsonb_to_recordset(roles.value) AS t(id text)
        UNION
        SELECT t.id FROM media_file
        CROSS JOIN LATERAL jsonb_each(media_file.tags) AS roles
        CROSS JOIN LATERAL jsonb_to_recordset(roles.value) AS t(id text)
    )`).Where("NOT EXISTS (SELECT 1 FROM used_tags WHERE used_tags.id = tag.id)")

	c, err := r.executeSQL(ctx, del)
	if err != nil {
		return fmt.Errorf("error purging %s unused tags: %w", r.tableName, err)
	}
	if c > 0 {
		log.Debug(ctx, "Purged unused tags", "totalDeleted", c, "table", r.tableName)
	}
	return err
}

var _ rest.Repository[model.Tag] = &tagRepository{}
