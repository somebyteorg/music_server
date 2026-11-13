package persistence

import (
	"context"

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
		return scoped.putPlaylist(ctx, playlist, columns...)
	})
}

func (r *playlistTrackRepository) mutate(ctx context.Context, block func(*playlistTrackRepository) error) error {
	return r.playlistRepo.withLock(ctx, r.playlistId, func(playlist *playlistRepository) error {
		scoped := *r
		scoped.db = playlist.db
		scoped.playlistRepo = playlist
		return block(&scoped)
	})
}

func (r *playlistTrackRepository) Add(ctx context.Context, ids []string) (count int, err error) {
	err = r.mutate(ctx, func(scoped *playlistTrackRepository) error { var e error; count, e = scoped.add(ctx, ids); return e })
	return
}

func (r *playlistTrackRepository) Insert(ctx context.Context, ids []string, position int) (count int, err error) {
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
	return r.mutate(ctx, func(scoped *playlistTrackRepository) error { return scoped.reorder(ctx, position, target) })
}
