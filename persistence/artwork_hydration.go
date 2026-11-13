package persistence

import (
	"context"
	"iter"

	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/pocketbase/dbx"
)

// hydrateItemImages returns per-item artwork info in one batched query per kind. On error it logs
// and returns an empty map, so the page still renders.
func hydrateItemImages(ctx context.Context, db dbx.Builder, kind model.Kind, ids []string) map[string]model.ItemArtworkInfo {
	if len(ids) == 0 {
		return map[string]model.ItemArtworkInfo{}
	}
	infos, err := NewArtworkRepository(db).GetInfoForItems(ctx, kind, ids)
	if err != nil {
		log.Error(ctx, "Failed to hydrate artwork info onto page", "kind", kind, err)
		return map[string]model.ItemArtworkInfo{}
	}
	return infos
}

// applyItemImage copies a hydration entry onto img; a missing entry leaves it zero (unresolved).
func applyItemImage(infos map[string]model.ItemArtworkInfo, id string, img *model.ItemImage) {
	if info, ok := infos[id]; ok {
		*img = info.Image()
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

// hydrateMediaFileArtwork mirrors MediaFile.CoverArtID: an embedded-eligible file with resolved own
// art uses it, else it falls back to the album's.
func hydrateMediaFileArtwork(ctx context.Context, db dbx.Builder, mfs model.MediaFiles) {
	if len(mfs) == 0 {
		return
	}
	albumIDs := make([]string, len(mfs))
	var eligibleIDs []string
	for i := range mfs {
		albumIDs[i] = mfs[i].AlbumID
		if mfs[i].HasCoverArt && conf.Server.EnableMediaFileCoverArt {
			eligibleIDs = append(eligibleIDs, mfs[i].ID)
		}
	}
	albumInfos := hydrateItemImages(ctx, db, model.KindAlbumArtwork, albumIDs)
	mfInfos := hydrateItemImages(ctx, db, model.KindMediaFileArtwork, eligibleIDs)
	for i := range mfs {
		mf := &mfs[i]
		applyItemImage(albumInfos, mf.AlbumID, &mf.AlbumImage)
		eligible := mf.HasCoverArt && conf.Server.EnableMediaFileCoverArt
		ownInfo, ownResolved := mfInfos[mf.ID]
		if eligible && ownResolved && !ownInfo.Absent() {
			mf.ItemImage = ownInfo.Image()
			continue
		}
		ownWontResolve := !eligible || (ownResolved && ownInfo.Absent())
		// Inherit the album hash only when serving returns those exact bytes: a multi-disc track is
		// served disc art, and an eligible-but-unresolved one still extracts its own embedded image.
		if album, ok := albumInfos[mf.AlbumID]; ok && !album.Absent() {
			if mf.DiscNumber == 0 && ownWontResolve {
				mf.ItemImage = album.Image()
			}
			continue
		}
		// Mark absent only when serving would definitively yield a placeholder; disc art and
		// still-extractable embedded art both keep a track requestable.
		if mf.DiscNumber > 0 {
			continue
		}
		if album, ok := albumInfos[mf.AlbumID]; ok && album.Absent() && ownWontResolve {
			mf.ImageAbsent = true
		}
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
