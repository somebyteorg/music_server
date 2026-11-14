package persistence

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	. "github.com/Masterminds/squirrel"
	"github.com/deluan/rest"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/utils/slice"
	"github.com/pocketbase/dbx"
)

type playlistRepository struct {
	sqlRepository
}

type dbPlaylist struct {
	model.Playlist `structs:",flatten"`
	Rules          sql.NullString `structs:"-"`
}

func (p *dbPlaylist) PostScan() error {
	if p.Rules.String != "" {
		return json.Unmarshal([]byte(p.Rules.String), &p.Playlist.Rules)
	}
	return nil
}

func (p dbPlaylist) PostMapArgs(args map[string]any) error {
	var err error
	if p.Playlist.IsSmartPlaylist() {
		args["rules"], err = json.Marshal(p.Playlist.Rules)
		if err != nil {
			return fmt.Errorf("invalid criteria expression: %w", err)
		}
		// Smart playlist counters are owned by refreshCounters (evaluation), never by callers
		delete(args, "song_count")
		delete(args, "duration")
		delete(args, "size")
		return nil
	}
	delete(args, "rules")
	return nil
}

func NewPlaylistRepository(db dbx.Builder) model.PlaylistRepository {
	r := &playlistRepository{}
	r.db = db
	r.registerModel(&model.Playlist{}, map[string]filterFunc{
		"id":      idFilter("playlist"),
		"q":       playlistFilter,
		"smart":   smartPlaylistFilter,
		"starred": annotationBoolFilter("starred"),
	})
	r.setSortMappings(map[string]string{
		"name":       naturalSort("playlist.name"),
		"owner_name": naturalSort("app_user.user_name"),
	})
	return r
}

func playlistFilter(_ string, value any) Sqlizer {
	return Or{
		substringFilter("playlist.name", value),
		substringFilter("playlist.comment", value),
	}
}

func smartPlaylistFilter(string, any) Sqlizer {
	return Or{
		Eq{"rules": ""},
		Eq{"rules": nil},
	}
}

func (r *playlistRepository) userFilter(ctx context.Context) Sqlizer {
	user := loggedUser(ctx)
	return And{Eq{"playlist.deleted_at": nil}, Or{Eq{"public": true}, Eq{"owner_id": user.ID}, Expr("playlist.id IN (SELECT playlist_id FROM playlist_collaborator WHERE user_id = ?)", user.ID)}}
}

func (r *playlistRepository) CountAll(ctx context.Context, options ...model.QueryOptions) (int64, error) {
	query := Select().Where(r.userFilter(ctx))
	if filtersNeedAnnotation(r.applyFilters(query, options...)) {
		query = r.withAnnotation(ctx, query, "playlist.id")
	}
	return r.count(ctx, query, options...)
}

func (r *playlistRepository) Exists(ctx context.Context, id string) (bool, error) {
	return r.exists(ctx, And{Eq{"id": id}, r.userFilter(ctx)})
}

func (r *playlistRepository) Delete(ctx context.Context, ids ...string) error {
	return r.withTx(ctx, func(tx sqlRepository) error {
		scoped := *r
		scoped.sqlRepository = tx
		for _, id := range ids {
			current, err := scoped.Get(ctx, id)
			if err != nil {
				return err
			}
			if current.OwnerID != loggedUser(ctx).ID {
				return model.ErrNotAuthorized
			}
			now := databaseNow()
			before := *current
			current.DeletedAt = &now
			if _, err := scoped.executeSQL(ctx, Update("playlist").Set("deleted_at", now).Set("updated_at", now).Where(Eq{"id": id})); err != nil {
				return err
			}
			if err := scoped.recordActivity(ctx, "deleted", &before, current); err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *playlistRepository) putPlaylist(ctx context.Context, p *model.Playlist, cols ...string) error {
	pls := dbPlaylist{Playlist: *p}
	if len(cols) > 0 {
		if pls.ID == "" {
			return errors.New("playlist id is required for partial update")
		}
		_, err := r.put(ctx, pls.ID, pls, cols...)
		return err
	}
	isNew := pls.ID == ""
	if isNew {
		pls.CreatedAt = databaseNow()
	}
	pls.UpdatedAt = databaseNow()

	id, err := r.put(ctx, pls.ID, pls)
	if err != nil {
		return err
	}
	p.ID = id

	if p.IsSmartPlaylist() {
		// Do not update tracks at this point, as it may take a long time and lock the DB, breaking the scan process
		return nil
	}
	// Only update tracks if they were specified
	if pls.Tracks != nil {
		return r.updateTracks(ctx, id, p.MediaFiles())
	}
	pls.ID = id // r.put assigns the generated id to p, not to this copy
	return r.refreshCounters(ctx, &pls.Playlist)
}

func (r *playlistRepository) Get(ctx context.Context, id string) (*model.Playlist, error) {
	return r.findBy(ctx, And{Eq{"playlist.id": id}, r.userFilter(ctx)})
}

func (r *playlistRepository) GetWithTracks(ctx context.Context, id string, refreshSmartPlaylist, includeMissing bool) (*model.Playlist, error) {
	pls, err := r.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if refreshSmartPlaylist {
		r.refreshSmartPlaylist(ctx, pls)
	}
	tracks, err := r.loadTracks(ctx, Select().From("playlist_tracks").
		Where(Eq{"missing": false}).
		OrderBy("playlist_tracks.sort_key", "playlist_tracks.id"), id)
	if err != nil {
		log.Error(ctx, "Error loading playlist tracks ", "playlist", pls.Name, "id", pls.ID, err)
		return nil, err
	}
	pls.SetTracks(tracks)
	return pls, nil
}

func (r *playlistRepository) FindByPath(ctx context.Context, path string) (*model.Playlist, error) {
	return r.findBy(ctx, Eq{"path": path})
}

func (r *playlistRepository) findBy(ctx context.Context, sql Sqlizer) (*model.Playlist, error) {
	sel := r.selectPlaylist(ctx).Where(sql)
	var pls []dbPlaylist
	err := r.queryAll(ctx, sel, &pls)
	if err != nil {
		return nil, err
	}
	if len(pls) == 0 {
		return nil, model.ErrNotFound
	}

	list := model.Playlists{pls[0].Playlist}
	r.hydrateArtwork(ctx, list)
	return &list[0], nil
}

func (r *playlistRepository) hydrateArtwork(ctx context.Context, playlists model.Playlists) {
	hydrateItems(ctx, r.db, model.KindPlaylistArtwork, playlists,
		func(p *model.Playlist) (string, *model.ItemImage) { return p.ID, &p.ItemImage })
}

func (r *playlistRepository) GetAll(ctx context.Context, options ...model.QueryOptions) (model.Playlists, error) {
	sel := r.selectPlaylist(ctx, options...).Where(r.userFilter(ctx))
	var res []dbPlaylist
	err := r.queryAll(ctx, sel, &res)
	if err != nil {
		return nil, err
	}
	playlists := make(model.Playlists, len(res))
	for i, p := range res {
		playlists[i] = p.Playlist
	}
	r.hydrateArtwork(ctx, playlists)
	return playlists, err
}

func (r *playlistRepository) GetCursor(ctx context.Context, options ...model.QueryOptions) (model.PlaylistCursor, error) {
	query := r.selectPlaylist(ctx, options...).Where(r.userFilter(ctx))
	cursor, err := queryCursor[dbPlaylist](ctx, r.sqlRepository, query)
	if err != nil {
		return nil, err
	}
	playlists := wrapCursor(cursor, func(row dbPlaylist) *model.Playlist { return &row.Playlist })
	return model.PlaylistCursor(hydrateCursor(playlists, func(batch []model.Playlist) {
		r.hydrateArtwork(ctx, batch)
	})), nil
}

func (r *playlistRepository) GetPlaylists(ctx context.Context, mediaFileId string) (model.Playlists, error) {
	sel := r.selectPlaylist(ctx, model.QueryOptions{Sort: "name"}).
		Where(And{Expr("EXISTS (SELECT 1 FROM playlist_tracks t WHERE t.playlist_id = playlist.id AND t.media_file_id = ?)", mediaFileId), r.userFilter(ctx)})
	var res []dbPlaylist
	err := r.queryAll(ctx, sel, &res)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return model.Playlists{}, nil
		}
		return nil, err
	}
	playlists := make(model.Playlists, len(res))
	for i, p := range res {
		playlists[i] = p.Playlist
	}
	r.hydrateArtwork(ctx, playlists)
	return playlists, nil
}

func (r *playlistRepository) selectPlaylist(ctx context.Context, options ...model.QueryOptions) SelectBuilder {
	sel := r.newSelect(ctx, options...).Join("app_user on app_user.id = owner_id").
		Columns(r.tableName+".*", "app_user.user_name as owner_name").
		Column(Expr("playlist.owner_id = ? AS can_edit", loggedUser(ctx).ID)).
		Column(Expr("(playlist.owner_id = ? OR EXISTS (SELECT 1 FROM playlist_collaborator c WHERE c.playlist_id = playlist.id AND c.user_id = ?)) AS can_edit_tracks", loggedUser(ctx).ID, loggedUser(ctx).ID))
	return r.withAnnotation(ctx, sel, r.tableName+".id")
}

func (r *playlistRepository) updateTracks(ctx context.Context, id string, tracks model.MediaFiles) error {
	ids := make([]string, len(tracks))
	for i := range tracks {
		ids[i] = tracks[i].ID
	}
	return r.updatePlaylist(ctx, id, ids)
}

func (r *playlistRepository) updatePlaylist(ctx context.Context, playlistId string, mediaFileIds []string) error {
	// Remove old tracks
	del := Delete("playlist_tracks").Where(Eq{"playlist_id": playlistId})
	_, err := r.executeSQL(ctx, del)
	if err != nil {
		return err
	}

	_, err = r.addTracks(ctx, playlistId, mediaFileIds)
	return err
}

// addTracks is the only path that writes playlist_tracks rows (smart playlists aside), so it owns
// the library and existence check: every caller, including a full replace through Put, goes through it.
const playlistSortGap int64 = 1048576

func (r *playlistRepository) addTracks(ctx context.Context, playlistId string, mediaFileIds []string) (int, error) {
	mediaFileIds, err := r.keepAccessible(ctx, mediaFileIds)
	if err != nil {
		return 0, err
	}
	var last struct{ Key int64 }
	if err := r.queryOne(ctx, Select("coalesce(max(sort_key), 0) AS key").From("playlist_tracks").Where(Eq{"playlist_id": playlistId}), &last); err != nil {
		return 0, err
	}
	return r.insertTracks(ctx, playlistId, last.Key, playlistSortGap, mediaFileIds)
}

func (r *playlistRepository) insertTracks(ctx context.Context, playlistId string, key, step int64, ids []string) (int, error) {
	for chunk := range slices.Chunk(ids, 200) {
		ins := Insert("playlist_tracks").Columns("playlist_id", "media_file_id", "sort_key")
		for _, id := range chunk {
			key += step
			ins = ins.Values(playlistId, id, key)
		}
		if _, err := r.executeSQL(ctx, ins); err != nil {
			return 0, err
		}
	}
	return len(ids), nil
}

// keepAccessible drops ids the caller cannot read or that match no track, preserving order and duplicates.
// Chunked because playlist imports can pass unbounded lists of ids.
func (r *playlistRepository) keepAccessible(ctx context.Context, mediaFileIds []string) ([]string, error) {
	accessible := make(map[string]struct{}, len(mediaFileIds))
	for chunk := range slices.Chunk(slice.Unique(mediaFileIds), 200) {
		sq := r.applyLibraryFilter(ctx, Select("id").From("media_file").Where(Eq{"id": chunk}), "media_file")
		var found []string
		if err := r.queryAllSlice(ctx, sq, &found); err != nil {
			return nil, err
		}
		for _, id := range found {
			accessible[id] = struct{}{}
		}
	}
	return slice.Filter(mediaFileIds, func(id string) bool {
		_, ok := accessible[id]
		return ok
	}), nil
}

// refreshCounters updates total playlist duration, size and count
func (r *playlistRepository) refreshCounters(ctx context.Context, pls *model.Playlist) error {
	statsSql := Select(
		"coalesce(sum(duration), 0) as duration",
		"coalesce(sum(size), 0) as size",
		"count(*) as count",
	).
		From("media_file").
		Join("playlist_tracks f on f.media_file_id = media_file.id").
		Where(Eq{"playlist_id": pls.ID, "media_file.missing": false})
	var res struct{ Duration, Size, Count float32 }
	err := r.queryOne(ctx, statsSql, &res)
	if err != nil {
		return err
	}

	// Update playlist's total duration, size and count
	now := databaseNow()
	upd := Update("playlist").
		Set("duration", res.Duration).
		Set("size", res.Size).
		Set("song_count", res.Count).
		Set("updated_at", now).
		Where(Eq{"id": pls.ID})
	_, err = r.executeSQL(ctx, upd)
	if err != nil {
		return err
	}
	pls.SongCount = int(res.Count)
	pls.Duration = res.Duration
	pls.Size = int64(res.Size)
	pls.UpdatedAt = now
	return nil
}

// tracksQuery is shared by loadTracks and GetCursor, so both hydrate rows identically.
func (r *playlistRepository) tracksQuery(ctx context.Context, query SelectBuilder, ids ...string) SelectBuilder {
	query = r.applyLibraryFilter(ctx, query, "f")
	userID := loggedUser(ctx).ID
	return query.
		Columns(
			"coalesce(starred, false) as starred",
			"starred_at",
			"coalesce(play_count, 0) as play_count",
			"play_date",
			"coalesce(rating, 0) as rating",
			"rated_at",
			"f.*",
			"playlist_tracks.*",
			"row_number() over (order by playlist_tracks.sort_key, playlist_tracks.id) AS position",
			"library.path as library_path",
			"library.name as library_name",
		).
		LeftJoin("annotation on (annotation.item_id = media_file_id"+
			" AND annotation.item_type = 'media_file' AND annotation.user_id = ?)", userID).
		Join("media_file f on f.id = media_file_id").
		Join("library on f.library_id = library.id").
		Where(stringIDsFilter("playlist_id", ids))
}

func (r *playlistRepository) loadTracks(ctx context.Context, query SelectBuilder, ids ...string) (model.PlaylistTracks, error) {
	tracks := dbPlaylistTracks{}
	err := r.queryAll(ctx, r.tracksQuery(ctx, query, ids...), &tracks)
	if err != nil {
		return nil, err
	}
	res := tracks.toModels()
	hydratePlaylistTrackArtwork(ctx, r.db, res)
	return res, err
}

func (r *playlistRepository) Count(ctx context.Context, options ...rest.QueryOptions) (int64, error) {
	return r.CountAll(ctx, r.parseRestOptions(ctx, options...))
}

func (r *playlistRepository) Read(ctx context.Context, id string) (*model.Playlist, error) {
	return r.Get(ctx, id)
}

func (r *playlistRepository) ReadAll(ctx context.Context, options ...rest.QueryOptions) ([]model.Playlist, error) {
	return r.GetAll(ctx, r.parseRestOptions(ctx, options...))
}

func (r *playlistRepository) Save(ctx context.Context, pls *model.Playlist) (string, error) {
	pls.ID = "" // Force new creation
	err := r.Put(ctx, pls)
	if err != nil {
		return "", err
	}
	return pls.ID, err
}

func (r *playlistRepository) Update(ctx context.Context, id string, entity model.Playlist, cols ...string) error {
	entity.ID = id
	return r.Put(ctx, &entity, cols...)
}

func (r *playlistRepository) removeOrphans(ctx context.Context) error {
	sel := Select("playlist_tracks.playlist_id as id", "p.name").From("playlist_tracks").
		Join("playlist p on playlist_tracks.playlist_id = p.id").
		LeftJoin("media_file mf on playlist_tracks.media_file_id = mf.id").
		Where(Eq{"mf.id": nil}).
		GroupBy("playlist_tracks.playlist_id", "p.name")

	var pls []struct{ Id, Name string }
	err := r.queryAll(ctx, sel, &pls)
	if err != nil {
		return fmt.Errorf("fetching playlists with orphan tracks: %w", err)
	}

	for _, pl := range pls {
		log.Debug(ctx, "Cleaning-up orphan tracks from playlist", "id", pl.Id, "name", pl.Name)
		del := Delete("playlist_tracks").Where(And{
			ConcatExpr("media_file_id not in (select id from media_file)"),
			Eq{"playlist_id": pl.Id},
		})
		n, err := r.executeSQL(ctx, del)
		if n == 0 || err != nil {
			return fmt.Errorf("deleting orphan tracks from playlist %s: %w", pl.Name, err)
		}
		log.Debug(ctx, "Deleted tracks, now reordering", "id", pl.Id, "name", pl.Name, "deleted", n)

		// Renumber the playlist if any track was removed
		if err := r.renumber(ctx, pl.Id); err != nil {
			return fmt.Errorf("renumbering playlist %s: %w", pl.Name, err)
		}
		if err := r.refreshCounters(ctx, &model.Playlist{ID: pl.Id}); err != nil {
			return err
		}
	}
	return nil
}

// renumber rebalances ordering keys without changing stable entry ids.
func (r *playlistRepository) renumber(ctx context.Context, id string) error {
	_, err := r.executeSQL(ctx, Expr(`WITH ranked AS (
        SELECT id, row_number() OVER (ORDER BY sort_key, id) * ? AS sort_key
        FROM playlist_tracks WHERE playlist_id = ?
    ) UPDATE playlist_tracks t SET sort_key = ranked.sort_key FROM ranked WHERE t.id = ranked.id`, playlistSortGap, id))
	return err
}

var _ model.PlaylistRepository = (*playlistRepository)(nil)
var _ rest.Repository[model.Playlist] = (*playlistRepository)(nil)
var _ rest.Persistable[model.Playlist] = (*playlistRepository)(nil)
