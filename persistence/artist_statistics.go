package persistence

import (
	"context"
	"fmt"
	"slices"

	. "github.com/Masterminds/squirrel"
	"github.com/navidrome/navidrome/log"
)

// Deduplicate tracks before aggregating across roles: the same artist can have
// several roles and sub-roles on a track, but its size must only count once.
const updateArtistStatisticsSQL = `
WITH selected_artists AS (
    SELECT unnest(CAST(? AS text[])) AS id
), tracks AS MATERIALIZED (
    SELECT DISTINCT mfa.artist_id, mf.library_id, mf.id, mf.album_id, mf.size, mfa.role
    FROM media_file_artists mfa
    JOIN selected_artists selected ON selected.id = mfa.artist_id
    JOIN media_file mf ON mf.id = mfa.media_file_id
    WHERE NOT mf.missing
), counters AS (
    SELECT artist_id, library_id, role, count(DISTINCT album_id) AS albums,
           count(*) AS media, sum(size) AS size
    FROM tracks GROUP BY artist_id, library_id, role
    UNION ALL
    SELECT artist_id, library_id, 'total', count(DISTINCT album_id), count(*), sum(size)
    FROM (SELECT DISTINCT artist_id, library_id, id, album_id, size FROM tracks) total_tracks
    GROUP BY artist_id, library_id
    UNION ALL
    SELECT artist_id, library_id, 'maincredit', count(DISTINCT album_id), count(*), sum(size)
    FROM (SELECT DISTINCT artist_id, library_id, id, album_id, size FROM tracks
          WHERE role IN ('albumartist', 'artist')) main_tracks
    GROUP BY artist_id, library_id
), library_counters AS (
    SELECT artist_id, library_id,
           jsonb_object_agg(role, jsonb_build_object('a', albums, 'm', media, 's', size)) AS stats
    FROM counters GROUP BY artist_id, library_id
)
UPDATE library_artist la
SET stats = coalesce((SELECT stats FROM library_counters lc
                      WHERE lc.artist_id = la.artist_id AND lc.library_id = la.library_id), '{}'::jsonb)
WHERE la.artist_id IN (SELECT id FROM selected_artists)`

// RefreshStats updates per-library artist statistics after a scan. One array
// parameter selects each batch, and its tracks are read once for all counters.
func (r *artistRepository) RefreshStats(ctx context.Context, allArtists bool) (int64, error) {
	query := Select("id").From("artist").Where(NotEq{"id": ""})
	if !allArtists {
		query = query.Where("updated_at > (SELECT min(last_scan_at) FROM library)")
	}
	var ids []string
	if err := r.queryAllSlice(ctx, query, &ids); err != nil {
		return 0, fmt.Errorf("fetching artists for statistics: %w", err)
	}
	if len(ids) == 0 {
		return 0, nil
	}
	var total int64
	for batch := range slices.Chunk(ids, 1000) {
		count, err := r.executeSQL(ctx, Expr(updateArtistStatisticsSQL, batch))
		if err != nil {
			return total, fmt.Errorf("updating artist statistics: %w", err)
		}
		total += count
	}
	removed, err := r.executeSQL(ctx, Delete("library_artist").Where(Eq{"stats": "{}"}))
	if err != nil {
		return total, fmt.Errorf("cleaning empty artist library statistics: %w", err)
	}
	if removed > 0 || allArtists {
		if err := r.markOrphansMissing(ctx); err != nil {
			return total, err
		}
	}
	log.Debug(ctx, "Refreshed artist statistics", "artists", len(ids), "updated", total, "removed", removed)
	return total, nil
}
