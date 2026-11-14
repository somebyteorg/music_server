package jellyfin

import (
	"cmp"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/navidrome/navidrome/core/stream"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/server/jellyfin/dto"
	"github.com/navidrome/navidrome/utils/req"
)

// mediaFileForRequest resolves {itemId} to a MediaFile and verifies the user has access to its
// library, writing 404 (never 403, to avoid an existence oracle) and returning ok=false otherwise.
// Shared by getPlaybackInfo and streamAudio so a guessed id can't probe or stream another library.
func (api *Router) mediaFileForRequest(w http.ResponseWriter, r *http.Request) (*model.MediaFile, bool) {
	ctx := r.Context()
	id, ok := itemIDParam(w, r, "itemId")
	if !ok {
		return nil, false
	}
	mf, err := api.ds.MediaFile().Get(ctx, id)
	if err != nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return nil, false
	}
	u, _ := request.UserFrom(ctx)
	if !u.HasLibraryAccess(mf.LibraryID) {
		http.Error(w, "Not Found", http.StatusNotFound)
		return nil, false
	}
	return mf, true
}

// getPlaybackInfo describes the same preferred original file used by direct playback.
func (api *Router) getPlaybackInfo(w http.ResponseWriter, r *http.Request) {
	mf, ok := api.mediaFileForRequest(w, r)
	if !ok {
		return
	}
	selected, err := stream.CommunityMedia(r.Context(), mf)
	if err != nil {
		api.internalError(w, r, err)
		return
	}
	src := dto.MediaSourceFromMediaFile(*selected)
	// The mapper only sees embedded lyrics; per-track we can afford the full pipeline
	// (sidecars, plugins) so Finamp's Lyric-stream gate reflects every source.
	if !slices.ContainsFunc(src.MediaStreams, func(s dto.MediaStream) bool { return s.Type == "Lyric" }) {
		if _, found := servableLyric(api.cachedLyrics(r.Context(), mf)); found {
			src.MediaStreams = append(src.MediaStreams, dto.MediaStream{
				Type: "Lyric", Index: len(src.MediaStreams), IsExternal: true,
			})
		}
	}
	// Self-authenticating: native players fetch this without an auth header. Server-relative:
	// clients append it to a base URL already carrying /jellyfin.
	if token := tokenFromRequest(r); token != "" {
		src.DirectStreamUrl = "/Audio/" + src.Id + "/universal?static=true&api_key=" + url.QueryEscape(token)
	}
	api.ok(w, r, dto.PlaybackInfoResponse{MediaSources: []dto.MediaSourceInfo{src}, PlaySessionId: dto.EncodeID(mf.ID)})
}

// streamAudio serves /Audio/{itemId}/stream[.container], reusing the same transcode-decision +
// streaming pipeline as the Subsonic /stream endpoint.
func (api *Router) streamAudio(w http.ResponseWriter, r *http.Request) {
	mf, ok := api.mediaFileForRequest(w, r)
	if !ok {
		return
	}
	p := req.Params(r)
	format := p.StringOr("container", "")
	if format == "" {
		// The /stream.{container} route form carries the format as a path segment, not a query param.
		format = chi.URLParam(r, "container")
	}
	if format == "" {
		// Jellyfin's audioCodec param names the target codec when no container is given.
		format = p.StringOr("audiocodec", "")
	}
	api.serveAudio(w, r, mf, format)
}

// streamUniversal serves /Audio/{itemId}/universal, where Container lists the "container|codec"
// entries the client direct-plays, and TranscodingContainer/AudioCodec name the fallback target.
func (api *Router) streamUniversal(w http.ResponseWriter, r *http.Request) {
	mf, ok := api.mediaFileForRequest(w, r)
	if !ok {
		return
	}
	p := req.Params(r)
	var streamReq stream.Request
	if p.BoolOr("static", false) {
		streamReq = api.transcodeDecider.ResolveRequest(r.Context(), mf, "raw", 0, 0)
	} else {
		streamReq = api.transcodeDecider.ResolveClientRequest(r.Context(), mf, universalClientInfo(p), 0)
	}
	api.serveStream(w, r, mf, streamReq)
}

func universalClientInfo(p *req.Values) *stream.ClientInfo {
	ci := &stream.ClientInfo{Name: "jellyfin-universal", MaxAudioBitrate: bitRateParam(p)}
	for entry := range strings.SplitSeq(joinedStrings(p, "container"), ",") {
		container, codec, _ := strings.Cut(strings.TrimSpace(entry), "|")
		if container == "" {
			continue
		}
		profile := stream.DirectPlayProfile{Containers: []string{container}, Protocols: []string{stream.ProtocolHTTP}}
		if codec != "" {
			profile.AudioCodecs = []string{codec}
		}
		ci.DirectPlayProfiles = append(ci.DirectPlayProfiles, profile)
	}
	codec := p.StringOr("audiocodec", "")
	if container := cmp.Or(p.StringOr("transcodingcontainer", ""), codec); container != "" {
		ci.TranscodingProfiles = []stream.Profile{{Container: container, AudioCodec: cmp.Or(codec, container), Protocol: stream.ProtocolHTTP}}
		ci.MaxTranscodingAudioBitrate = ci.MaxAudioBitrate
	}
	return ci
}

// bitRateParam reads Jellyfin's bits/sec bitrate params as the kbps the stream package expects.
func bitRateParam(p *req.Values) int {
	return cmp.Or(p.IntOr("audiobitrate", 0), p.IntOr("maxstreamingbitrate", 0)) / 1000
}

func (api *Router) serveAudio(w http.ResponseWriter, r *http.Request, mf *model.MediaFile, format string) {
	if req.Params(r).BoolOr("static", false) {
		format = "raw"
	}
	api.serveStream(w, r, mf, api.transcodeDecider.ResolveRequest(r.Context(), mf, format, bitRateParam(req.Params(r)), 0))
}

func (api *Router) serveStream(w http.ResponseWriter, r *http.Request, mf *model.MediaFile, streamReq stream.Request) {
	ctx := r.Context()
	s, err := api.streamer.NewStream(ctx, mf, streamReq)
	if err != nil {
		api.internalError(w, r, err)
		return
	}
	defer s.Close()
	if _, err := s.Serve(ctx, w, r); err != nil {
		log.Error(ctx, "Jellyfin API: error streaming", "id", mf.ID, err)
	}
}

// streamHls refuses HLS conversion; clients must use the original-file stream.
func (api *Router) streamHls(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "Transcoding is disabled; use direct playback", http.StatusNotAcceptable)
}

// streamFile serves /Items/{itemId}/File and /Download, Jellyfin's direct-file endpoints. Some
// clients (Finamp's just_audio engine) fetch playback audio here instead of /Audio/{id}/stream, so
// it must always resolve to direct play ("raw"), never a forced transcode.
func (api *Router) streamFile(w http.ResponseWriter, r *http.Request) {
	mf, ok := api.mediaFileForRequest(w, r)
	if !ok {
		return
	}
	api.serveStream(w, r, mf, api.transcodeDecider.ResolveRequest(r.Context(), mf, "raw", 0, 0))
}
