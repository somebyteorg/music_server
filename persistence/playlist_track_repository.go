package persistence

import (
	"context"
	"fmt"
	"slices"

	. "github.com/Masterminds/squirrel"
	"github.com/deluan/rest"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/utils/slice"
)

type playlistTrackRepository struct {
	sqlRepository
	playlistId   string
	playlist     *model.Playlist
	playlistRepo *playlistRepository
}

type dbPlaylistTrack struct {
	dbMediaFile
	*model.PlaylistTrack `structs:",flatten"`
}

func (t *dbPlaylistTrack) PostScan() error {
	if err := t.dbMediaFile.PostScan(); err != nil {
		return err
	}
	t.PlaylistTrack.MediaFile = *t.dbMediaFile.MediaFile
	t.PlaylistTrack.MediaFile.ID = t.MediaFileID
	return nil
}

type dbPlaylistTracks []dbPlaylistTrack

func (t dbPlaylistTracks) toModels() model.PlaylistTracks {
	return slice.Map(t, func(trk dbPlaylistTrack) model.PlaylistTrack {
		return *trk.PlaylistTrack
	})
}

func (r *playlistRepository) Tracks(ctx context.Context, playlistId string, refreshSmartPlaylist bool) model.PlaylistTrackRepository {
	p := &playlistTrackRepository{}
	p.playlistRepo = r
	p.playlistId = playlistId
	p.db = r.db
	p.tableName = "playlist_tracks"
	p.registerModel(&model.PlaylistTrack{}, map[string]filterFunc{
		"missing":    booleanFilter,
		"library_id": libraryIdFilter,
	})
	p.setSortMappings(
		map[string]string{
			"id":           "playlist_tracks.sort_key, playlist_tracks.id",
			"artist":       "order_artist_name",
			"album_artist": "order_album_artist_name",
			"album":        "order_album_name, album_id, disc_number, track_number, order_artist_name, " + naturalSort("f.title"),
			"title":        "order_title",
			"random":       "random()",
			// To make sure these fields will be whitelisted
			"duration": "duration",
			"year":     "year",
			"bpm":      "bpm",
			"channels": "channels",
		},
		"f") // TODO I don't like this solution, but I won't change it now as it's not the focus of BFR.

	pls, err := r.Get(ctx, playlistId)
	if err != nil {
		log.Warn(ctx, "Error getting playlist's tracks", "playlistId", playlistId, err)
		return nil
	}
	if refreshSmartPlaylist {
		r.refreshSmartPlaylist(ctx, pls)
	}
	p.playlist = pls
	return p
}

func (r *playlistTrackRepository) CountAll(ctx context.Context, options ...model.QueryOptions) (int64, error) {
	query := Select().
		Join("media_file f on f.id = media_file_id").
		Where(Eq{"playlist_id": r.playlistId})
	query = r.applyLibraryFilter(ctx, query, "f")
	return r.count(ctx, query, options...)
}

func (r *playlistTrackRepository) Count(ctx context.Context, options ...rest.QueryOptions) (int64, error) {
	query := Select().
		LeftJoin("media_file f on f.id = media_file_id").
		Where(Eq{"playlist_id": r.playlistId})
	query = r.applyLibraryFilter(ctx, query, "f")
	return r.count(ctx, query, r.parseRestOptions(ctx, options...))
}

func (r *playlistTrackRepository) Read(ctx context.Context, id string) (*model.PlaylistTrack, error) {
	userID := loggedUser(ctx).ID
	sel := r.newSelect(ctx).
		LeftJoin("annotation on (annotation.item_id = media_file_id"+
			" AND annotation.item_type = 'media_file' AND annotation.user_id = ?)", userID).
		Columns(
			"coalesce(starred, false) as starred",
			"coalesce(play_count, 0) as play_count",
			"coalesce(rating, 0) as rating",
			"starred_at",
			"play_date",
			"rated_at",
			"f.*",
			"playlist_tracks.*",
			"(SELECT count(*) FROM playlist_tracks preceding WHERE preceding.playlist_id = playlist_tracks.playlist_id AND (preceding.sort_key, preceding.id) <= (playlist_tracks.sort_key, playlist_tracks.id)) AS position",
		).
		Join("media_file f on f.id = media_file_id").
		Where(And{Eq{"playlist_id": r.playlistId}, Eq{"playlist_tracks.id": id}})
	sel = r.applyLibraryFilter(ctx, sel, "f")
	var trk dbPlaylistTrack
	err := r.queryOne(ctx, sel, &trk)
	return trk.PlaylistTrack, err
}

func (r *playlistTrackRepository) GetAll(ctx context.Context, options ...model.QueryOptions) (model.PlaylistTracks, error) {
	tracks, err := r.playlistRepo.loadTracks(ctx, r.newSelect(ctx, options...), r.playlistId)
	if err != nil {
		return nil, err
	}
	return tracks, err
}

func (r *playlistTrackRepository) GetCursor(ctx context.Context, options ...model.QueryOptions) (model.PlaylistTrackCursor, error) {
	sel := r.playlistRepo.tracksQuery(ctx, r.newSelect(ctx, options...), r.playlistId)
	cursor, err := queryCursor[dbPlaylistTrack](ctx, r.sqlRepository, sel)
	if err != nil {
		return nil, err
	}
	tracks := wrapCursor(cursor, func(t dbPlaylistTrack) *model.PlaylistTrack {
		return t.PlaylistTrack
	})
	return model.PlaylistTrackCursor(hydrateCursor(tracks, func(batch []model.PlaylistTrack) {
		hydratePlaylistTrackArtwork(ctx, r.db, batch)
	})), nil
}

// GetMediaFileIDs returns the tracks' song ids, for callers that need every id but no track data.
func (r *playlistTrackRepository) GetMediaFileIDs(ctx context.Context, options ...model.QueryOptions) ([]string, error) {
	query := r.newSelect(ctx, options...).Columns("media_file_id").
		Join("media_file f on f.id = media_file_id").
		Where(Eq{"playlist_id": r.playlistId})
	query = r.applyLibraryFilter(ctx, query, "f")
	query = query.OrderBy("playlist_tracks.sort_key", "playlist_tracks.id")
	var ids []string
	if err := r.queryAllSlice(ctx, query, &ids); err != nil {
		return nil, err
	}
	return ids, nil
}

func (r *playlistTrackRepository) GetAlbumIDs(ctx context.Context, options ...model.QueryOptions) ([]string, error) {
	albums := r.newSelect(ctx).Columns("DISTINCT mf.album_id AS id").
		Join("media_file mf on mf.id = media_file_id").
		Where(Eq{"playlist_id": r.playlistId})
	albums = r.applyLibraryFilter(ctx, r.applyFilters(albums, options...), "mf")
	// Deduplicate before sampling: PostgreSQL requires every DISTINCT sort
	// expression in the projection, and random() would make every row distinct.
	scoped := r.sqlRepository
	scoped.tableName = "playlist_albums"
	scoped.sortMappings = map[string]string{"id": "id", "random": "random()"}
	query := scoped.applyOptions(Select("id").FromSelect(albums, scoped.tableName), options...)
	var ids []string
	err := r.queryAllSlice(ctx, query, &ids)
	if err != nil {
		return nil, err
	}
	return ids, nil
}

func (r *playlistTrackRepository) ReadAll(ctx context.Context, options ...rest.QueryOptions) ([]model.PlaylistTrack, error) {
	return r.GetAll(ctx, r.parseRestOptions(ctx, options...))
}

func (r *playlistTrackRepository) add(ctx context.Context, ids []string) (int, error) {
	return r.playlistRepo.addTracks(ctx, r.playlistId, ids)
}

// insert reserves spaced ordering keys between the neighboring entries.
func (r *playlistTrackRepository) insert(ctx context.Context, ids []string, position int) (int, error) {
	ids, err := r.playlistRepo.keepAccessible(ctx, ids)
	if err != nil || len(ids) == 0 {
		return 0, err
	}
	before, after, err := r.neighbors(ctx, "", position)
	if err != nil {
		return 0, err
	}
	step := playlistSortGap
	if after != 0 {
		step = (after - before) / int64(len(ids)+1)
	}
	if step < 1 {
		if err := r.playlistRepo.renumber(ctx, r.playlistId); err != nil {
			return 0, err
		}
		before, after, err = r.neighbors(ctx, "", position)
		if err != nil {
			return 0, err
		}
		step = (after - before) / int64(len(ids)+1)
		if step < 1 {
			return 0, fmt.Errorf("too many tracks for an insertion")
		}
	}
	return r.playlistRepo.insertTracks(ctx, r.playlistId, before, step, ids)
}

func (r *playlistTrackRepository) neighbors(ctx context.Context, exclude string, position int) (int64, int64, error) {
	position = max(position, 1)
	query := Select("sort_key").From("playlist_tracks").Where(Eq{"playlist_id": r.playlistId}).OrderBy("sort_key", "id")
	if exclude != "" {
		query = query.Where(NotEq{"id": exclude})
	}
	query = query.Offset(uint64(max(position-2, 0))).Limit(2)
	var keys []int64
	if err := r.queryAllSlice(ctx, query, &keys); err != nil {
		return 0, 0, err
	}
	if position == 1 {
		if len(keys) > 0 {
			return 0, keys[0], nil
		}
		return 0, 0, nil
	}
	if len(keys) == 2 {
		return keys[0], keys[1], nil
	}
	if len(keys) == 1 {
		return keys[0], 0, nil
	}
	var last struct{ Key int64 }
	err := r.queryOne(ctx, Select("coalesce(max(sort_key), 0) AS key").From("playlist_tracks").Where(Eq{"playlist_id": r.playlistId}), &last)
	return last.Key, 0, err
}

func (r *playlistTrackRepository) addMediaFileIds(ctx context.Context, cond Sqlizer) (int, error) {
	sq := Select("id").From("media_file").Where(cond).OrderBy("album_artist, album, release_date, disc_number, track_number")
	var ids []string
	err := r.queryAllSlice(ctx, sq, &ids)
	if err != nil {
		log.Error(ctx, "Error getting tracks to add to playlist", err)
		return 0, err
	}
	return r.Add(ctx, ids)
}

func (r *playlistTrackRepository) AddAlbums(ctx context.Context, albumIds []string) (int, error) {
	return r.addMediaFileIds(ctx, Eq{"album_id": albumIds})
}

func (r *playlistTrackRepository) AddArtists(ctx context.Context, artistIds []string) (int, error) {
	// Match by album-artist participation, not the deprecated album_artist_id
	// column, which only holds the first album artist.
	return r.addMediaFileIds(ctx, ParticipantIDFilter("media_file", artistIds, model.RoleAlbumArtist))
}

func (r *playlistTrackRepository) AddDiscs(ctx context.Context, discs []model.DiscID) (int, error) {
	if len(discs) == 0 {
		return 0, nil
	}
	var clauses Or
	for _, d := range discs {
		clauses = append(clauses, And{Eq{"album_id": d.AlbumID}, Eq{"release_date": d.ReleaseDate}, Eq{"disc_number": d.DiscNumber}})
	}
	return r.addMediaFileIds(ctx, clauses)
}

// deleteChunkSize bounds each DELETE, matching addTracks.
const deleteChunkSize = 200

func (r *playlistTrackRepository) deleteTracks(ctx context.Context, ids ...string) error {
	for chunk := range slices.Chunk(ids, deleteChunkSize) {
		if err := r.delete(ctx, And{Eq{"playlist_id": r.playlistId}, Eq{"id": chunk}}); err != nil {
			return err
		}
	}
	return nil
}

func (r *playlistTrackRepository) deleteAllTracks(ctx context.Context) error {
	return r.delete(ctx, Eq{"playlist_id": r.playlistId})
}

// reorder moves a stable entry id to a 1-based list position.
func (r *playlistTrackRepository) reorder(ctx context.Context, entryID int, position int) error {
	var entry struct{ ID string }
	if err := r.queryOne(ctx, Select("id").From("playlist_tracks").Where(Eq{"playlist_id": r.playlistId, "id": entryID}), &entry); err != nil {
		return err
	}
	var count struct{ Count int }
	if err := r.queryOne(ctx, Select("count(*) AS count").From("playlist_tracks").Where(Eq{"playlist_id": r.playlistId}), &count); err != nil {
		return err
	}
	position = min(max(position, 1), count.Count)
	before, after, err := r.neighbors(ctx, entry.ID, position)
	if err != nil {
		return err
	}
	if after != 0 && after-before < 2 {
		if err := r.playlistRepo.renumber(ctx, r.playlistId); err != nil {
			return err
		}
		before, after, err = r.neighbors(ctx, entry.ID, position)
		if err != nil {
			return err
		}
	}
	key := before + playlistSortGap
	if after != 0 {
		key = before + (after-before)/2
	}
	_, err = r.executeSQL(ctx, Update("playlist_tracks").Set("sort_key", key).Where(Eq{"playlist_id": r.playlistId, "id": entryID}))
	return err
}

var _ model.PlaylistTrackRepository = (*playlistTrackRepository)(nil)
