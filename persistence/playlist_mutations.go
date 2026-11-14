package persistence

import (
	"context"
	"encoding/json"
	"reflect"

	. "github.com/Masterminds/squirrel"
	"github.com/navidrome/navidrome/model"
)

// withLock serializes changes to one playlist, keeping different playlists independent.
func (r *playlistRepository) withLock(ctx context.Context, id string, block func(*playlistRepository) error) error {
	return r.withTx(ctx, func(tx sqlRepository) error {
		var item struct{ ID string }
		if err := tx.queryOne(ctx, Select("id").From("playlist").Where(Eq{"id": id}).Suffix("FOR UPDATE"), &item); err != nil {
			return err
		}
		scoped := *r
		scoped.sqlRepository = tx
		return block(&scoped)
	})
}

func (r *playlistRepository) Put(ctx context.Context, playlist *model.Playlist, columns ...string) error {
	return r.withTx(ctx, func(tx sqlRepository) error {
		scoped := *r
		scoped.sqlRepository = tx
		var before *model.Playlist
		action := "created"
		if playlist.ID != "" {
			var err error
			before, err = scoped.Get(ctx, playlist.ID)
			if err != nil {
				return err
			}
			// Cover sources are managed by music_api.
			playlist.CoverID = before.CoverID
			playlist.UploadedImage = before.UploadedImage
			if !before.CanEdit && (playlistMetadataChanged(before, playlist) || playlist.Tracks == nil) {
				return model.ErrNotAuthorized
			}
			if playlist.Tracks != nil && (!before.CanEditTracks || !before.TracksEditable()) {
				return model.ErrNotAuthorized
			}
			action = "updated"
		} else {
			playlist.OwnerID = loggedUser(ctx).ID
			playlist.CoverID = nil
			playlist.UploadedImage = ""
		}
		if err := scoped.putPlaylist(ctx, playlist, columns...); err != nil {
			return err
		}
		if before == nil || playlist.Tracks != nil {
			if err := scoped.refreshCounters(ctx, playlist); err != nil {
				return err
			}
		}
		if before == nil || playlistMetadataChanged(before, playlist) {
			return scoped.recordActivity(ctx, action, before, playlist)
		}
		return nil
	})
}

func (r *playlistTrackRepository) mutate(ctx context.Context, block func(*playlistTrackRepository) error, refreshStats ...bool) error {
	return r.playlistRepo.withLock(ctx, r.playlistId, func(repo *playlistRepository) error {
		playlist, err := repo.Get(ctx, r.playlistId)
		if err != nil {
			return err
		}
		if !playlist.CanEditTracks {
			return model.ErrNotAuthorized
		}
		if !playlist.TracksEditable() {
			return model.ErrPlaylistNotEditable
		}
		scoped := *r
		scoped.sqlRepository = repo.sqlRepository
		scoped.playlistRepo = repo
		scoped.playlist = playlist
		if err := block(&scoped); err != nil {
			return err
		}
		if len(refreshStats) != 0 && !refreshStats[0] {
			_, err := repo.executeSQL(ctx, Update("playlist").Set("updated_at", databaseNow()).Where(Eq{"id": playlist.ID}))
			return err
		}
		if err := repo.refreshCounters(ctx, playlist); err != nil {
			return err
		}
		return nil
	})
}

func playlistMetadataChanged(before, after *model.Playlist) bool {
	return before.Name != after.Name || before.Comment != after.Comment || before.Public != after.Public || before.OwnerID != after.OwnerID || before.UploadedImage != after.UploadedImage || !reflect.DeepEqual(before.CoverID, after.CoverID) || !reflect.DeepEqual(before.Rules, after.Rules)
}

func playlistSnapshot(playlist *model.Playlist) map[string]any {
	if playlist == nil {
		return nil
	}
	return map[string]any{"name": playlist.Name, "description": playlist.Comment, "is_public": playlist.Public, "cover_id": playlist.CoverID, "deleted_at": playlist.DeletedAt}
}

func (r *playlistRepository) recordActivity(ctx context.Context, action string, before, after *model.Playlist) error {
	var previous any
	if before != nil {
		state, err := json.Marshal(playlistSnapshot(before))
		if err != nil {
			return err
		}
		previous = string(state)
	}
	state, err := json.Marshal(playlistSnapshot(after))
	if err != nil {
		return err
	}
	_, err = r.executeSQL(ctx, Insert("playlist_activity").Columns("playlist_id", "actor_id", "action", "title", "before", "after", "is_private").Values(after.ID, loggedUser(ctx).ID, action, after.Name, previous, string(state), !after.Public || before != nil && !before.Public))
	return err
}

func (r *playlistTrackRepository) Add(ctx context.Context, ids []string) (count int, err error) {
	if len(ids) == 0 {
		return 0, nil
	}
	err = r.mutate(ctx, func(scoped *playlistTrackRepository) error { var e error; count, e = scoped.add(ctx, ids); return e })
	return
}

func (r *playlistTrackRepository) Insert(ctx context.Context, ids []string, position int) (count int, err error) {
	if len(ids) == 0 {
		return 0, nil
	}
	err = r.mutate(ctx, func(scoped *playlistTrackRepository) error {
		var e error
		count, e = scoped.insert(ctx, ids, position)
		return e
	})
	return
}

func (r *playlistTrackRepository) Delete(ctx context.Context, ids ...string) error {
	return r.mutate(ctx, func(scoped *playlistTrackRepository) error { return scoped.deleteTracks(ctx, ids...) })
}

func (r *playlistTrackRepository) DeleteAll(ctx context.Context) error {
	return r.mutate(ctx, func(scoped *playlistTrackRepository) error { return scoped.deleteAllTracks(ctx) })
}

func (r *playlistTrackRepository) Reorder(ctx context.Context, position, target int) error {
	return r.mutate(ctx, func(scoped *playlistTrackRepository) error { return scoped.reorder(ctx, position, target) }, false)
}
