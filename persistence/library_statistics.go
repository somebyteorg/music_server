package persistence

import (
	"context"
	"time"

	. "github.com/Masterminds/squirrel"
	"github.com/navidrome/navidrome/model"
)

func (r *libraryRepository) ScanBegin(ctx context.Context, id int, fullScan bool) error {
	sq := Update(r.tableName).
		Set("last_scan_started_at", databaseNow()).
		Set("full_scan_in_progress", fullScan).
		Where(Eq{"id": id})
	_, err := r.executeSQL(ctx, sq)
	return err
}

func (r *libraryRepository) ScanEnd(ctx context.Context, id int) error {
	sq := Update(r.tableName).
		Set("last_scan_at", databaseNow()).
		Set("full_scan_in_progress", false).
		Set("last_scan_started_at", time.Time{}).
		Where(Eq{"id": id})
	_, err := r.executeSQL(ctx, sq)
	return err
}

func (r *libraryRepository) SetScannedPID(ctx context.Context, id int, pid model.PIDConfig) error {
	sq := Update(r.tableName).
		Set("scanned_pid_album", pid.Album).
		Set("scanned_pid_track", pid.Track).
		Where(Eq{"id": id})
	_, err := r.executeSQL(ctx, sq)
	return err
}

func (r *libraryRepository) ScanInProgress(ctx context.Context) (bool, error) {
	query := r.newSelect(ctx).Where(NotEq{"last_scan_started_at": time.Time{}})
	count, err := r.count(ctx, query)
	return count > 0, err
}

func (r *libraryRepository) RefreshStats(ctx context.Context, id int) error {
	// One statement gives every statistic the same snapshot and avoids sharing a
	// transaction connection between concurrent queries.
	_, err := r.executeSQL(ctx, Expr(`UPDATE library SET
        (total_songs, total_missing_files) = (
            SELECT count(*) FILTER (WHERE NOT missing), count(*) FILTER (WHERE missing)
            FROM media_file WHERE library_id = ?),
        (total_albums, total_size, total_duration) = (
            SELECT count(*), coalesce(sum(size), 0), coalesce(sum(duration), 0)
            FROM album WHERE library_id = ? AND NOT missing),
        (total_folders, total_files) = (
            SELECT count(*) FILTER (WHERE num_audio_files > 0),
                coalesce(sum(num_audio_files + num_playlists + jsonb_array_length(image_files)), 0)
            FROM folder WHERE library_id = ? AND NOT missing),
        total_artists = (
            SELECT count(*) FROM library_artist la JOIN artist a ON a.id = la.artist_id
            WHERE la.library_id = ? AND NOT a.missing),
        updated_at = CURRENT_TIMESTAMP
        WHERE id = ?`, id, id, id, id, id))
	return err
}
