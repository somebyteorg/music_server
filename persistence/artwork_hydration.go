package persistence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"iter"
	"slices"

	. "github.com/Masterminds/squirrel"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/pocketbase/dbx"
)

// Cover availability comes from API-managed source paths and URLs, without thumbnail state.
func hydrateItemImages(ctx context.Context, db dbx.Builder, kind model.Kind, ids []string) map[string]model.ItemArtworkInfo {
	infos := make(map[string]model.ItemArtworkInfo, len(ids))
	repo := sqlRepository{db: db, tableName: "music_cover"}
	for chunk := range slices.Chunk(ids, artworkBatchSize) {
		var rows []struct {
			ID   string
			Path string
			URL  string
		}
		query := Select("id", "path", "url").From("music_cover").Where(Eq{"id": chunk})
		if err := repo.queryAll(ctx, query, &rows); err != nil {
			log.Error(ctx, "Failed to hydrate source cover info", "kind", kind, err)
			return map[string]model.ItemArtworkInfo{}
		}
		for _, row := range rows {
			source := row.Path
			if source == "" {
				source = row.URL
			}
			if source == "" {
				infos[row.ID] = model.ItemArtworkInfo{ItemID: row.ID}
				continue
			}
			// Tags identify the stored source; no image bytes are read.
			digest := sha256.Sum256([]byte(source))
			infos[row.ID] = model.ItemArtworkInfo{ItemID: row.ID, Hash: hex.EncodeToString(digest[:8])}
		}
	}
	return infos
}

func applyItemImage(infos map[string]model.ItemArtworkInfo, id string, img *model.ItemImage) {
	if info, ok := infos[id]; ok {
		*img = info.Image()
	} else {
		*img = model.ItemImage{ImageAbsent: true}
	}
}

// hydrateItems fills every item's embedded ItemImage from one batched lookup. ref yields an
// item's id and the ItemImage to fill, which is all that differs between the entity kinds.
func hydrateItems[T any](ctx context.Context, db dbx.Builder, kind model.Kind, items []T,
	ref func(*T) (string, *model.ItemImage)) {
	ids := make([]string, len(items))
	for i := range items {
		ids[i], _ = ref(&items[i])
	}
	infos := hydrateItemImages(ctx, db, kind, ids)
	for i := range items {
		id, img := ref(&items[i])
		applyItemImage(infos, id, img)
	}
}

// API has already resolved track and album cover sources, including fallback artwork.
func hydrateMediaFileArtwork(ctx context.Context, db dbx.Builder, mfs model.MediaFiles) {
	if len(mfs) == 0 {
		return
	}
	albumIDs := make([]string, len(mfs))
	mediaIDs := make([]string, len(mfs))
	for i := range mfs {
		albumIDs[i] = mfs[i].AlbumID
		mediaIDs[i] = mfs[i].ID
	}
	albumInfos := hydrateItemImages(ctx, db, model.KindAlbumArtwork, albumIDs)
	mfInfos := hydrateItemImages(ctx, db, model.KindMediaFileArtwork, mediaIDs)
	for i := range mfs {
		mf := &mfs[i]
		applyItemImage(albumInfos, mf.AlbumID, &mf.AlbumImage)
		applyItemImage(mfInfos, mf.ID, &mf.ItemImage)
	}
}

// hydrateCursor hydrates a streamed cursor in batches, avoiding a per-row query.
func hydrateCursor[T any](cursor iter.Seq2[T, error], hydrate func([]T)) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		buf := make([]T, 0, artworkBatchSize)
		flush := func() bool {
			hydrate(buf)
			for i := range buf {
				if !yield(buf[i], nil) {
					return false
				}
			}
			buf = buf[:0]
			return true
		}
		for row, err := range cursor {
			if err != nil {
				var zero T
				yield(zero, err)
				return
			}
			buf = append(buf, row)
			if len(buf) == artworkBatchSize && !flush() {
				return
			}
		}
		if len(buf) > 0 {
			flush()
		}
	}
}

// hydratePlaylistTrackArtwork hydrates the MediaFile embedded in each playlist track.
func hydratePlaylistTrackArtwork(ctx context.Context, db dbx.Builder, tracks model.PlaylistTracks) {
	if len(tracks) == 0 {
		return
	}
	mfs := make(model.MediaFiles, len(tracks))
	for i := range tracks {
		mfs[i] = tracks[i].MediaFile
	}
	hydrateMediaFileArtwork(ctx, db, mfs)
	for i := range tracks {
		tracks[i].MediaFile = mfs[i]
	}
}
