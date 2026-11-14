package jellyfin

import (
	"context"
	"errors"
	"net/http"

	"github.com/navidrome/navidrome/core/stream"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/server/imghttp"
)

func (api *Router) getItemImage(w http.ResponseWriter, r *http.Request) {
	// Public, like Jellyfin's own image routes: clients build cover URLs without credentials, and
	// upstream resolves them with no visibility check either (LibraryManager.ItemIsVisible, null user).
	ctx := request.WithUser(r.Context(), model.User{IsAdmin: true})
	itemId, ok := itemIDParam(w, r, "itemId")
	if !ok {
		return
	}
	served, err := stream.ServeCommunityCover(ctx, w, r, itemId)
	if err != nil {
		api.internalError(w, r, err)
		return
	}
	if served {
		return
	}
	artID := api.resolveArtworkID(ctx, itemId)
	img, err := api.artwork.GetOrPlaceholder(ctx, artID, 0, false)
	switch {
	case errors.Is(err, context.Canceled):
		return
	case err != nil:
		log.Warn(ctx, "Error retrieving artwork", "id", itemId, err)
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}
	defer img.Close()
	if err := imghttp.ServeImage(w, r, img, ""); err != nil {
		api.internalError(w, r, err)
	}
}

// resolveArtworkID maps a Jellyfin item id to a Navidrome ArtworkID, probing
// album -> artist -> media file -> playlist.
func (api *Router) resolveArtworkID(ctx context.Context, itemId string) string {
	if al, err := api.ds.Album().Get(ctx, itemId); err == nil {
		return al.CoverArtID().String()
	}
	if ar, err := api.ds.Artist().Get(ctx, itemId); err == nil {
		return ar.CoverArtID().String()
	}
	if mf, err := api.ds.MediaFile().Get(ctx, itemId); err == nil {
		return mf.CoverArtID().String()
	}
	if pl, err := api.ds.Playlist().Get(ctx, itemId); err == nil {
		return pl.CoverArtID().String()
	}
	return (model.ArtworkID{}).String()
}

func (api *Router) postItemImage(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "Manage artwork in the music library", http.StatusForbidden)
}

func (api *Router) deleteItemImage(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "Manage artwork in the music library", http.StatusForbidden)
}
