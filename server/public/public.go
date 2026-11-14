package public

import (
	"context"
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"
	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/core/artwork"
	"github.com/navidrome/navidrome/core/publicurl"
	"github.com/navidrome/navidrome/core/stream"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/server"
)

type Router struct {
	http.Handler
	artwork  artwork.Artwork
	streamer stream.MediaStreamer
	decider  stream.TranscodeDecider
	archiver core.Archiver
	share    core.Share
	ds       model.DataStore
}

func New(ds model.DataStore, artwork artwork.Artwork, streamer stream.MediaStreamer, decider stream.TranscodeDecider, share core.Share, archiver core.Archiver) *Router {
	p := &Router{ds: ds, artwork: artwork, streamer: streamer, decider: decider, share: share, archiver: archiver}
	p.Handler = p.routes()

	return p
}

func (pub *Router) routes() http.Handler {
	r := chi.NewRouter()

	r.Group(func(r chi.Router) {
		r.Use(server.URLParamsMiddleware)
		r.Group(func(r chi.Router) {
			r.Use(server.ThrottleBacklog(conf.Server.DevArtworkMaxRequests, conf.Server.DevArtworkThrottleBacklogLimit,
				conf.Server.DevArtworkThrottleBacklogTimeout))
			r.HandleFunc("/img/{id}", pub.handleImages)
		})
		if conf.Server.EnableSharing {
			r.HandleFunc("/s/{id}", pub.handleStream)
			if conf.Server.EnableDownloads {
				r.HandleFunc("/d/{id}", pub.handleDownloads)
			}
			r.HandleFunc("/{id}/m3u", pub.handleM3U)
			r.HandleFunc("/{id}", pub.handleShares)
		}
	})
	return r
}

func ShareURL(ctx context.Context, id string) string {
	return publicurl.WebURL(ctx, url.Values{"share": {id}})
}
