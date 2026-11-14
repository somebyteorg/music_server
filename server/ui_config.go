package server

import (
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/deluan/rest"
	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/conf/mime"
	"github.com/navidrome/navidrome/consts"
	"github.com/navidrome/navidrome/core/quickconnect"
	"github.com/navidrome/navidrome/utils/str"
)

// uiConfig exposes only the settings needed to initialize the independently deployed web UI.
func uiConfig(w http.ResponseWriter, r *http.Request) {
	appConfig := map[string]any{
		"version":                   consts.Version,
		"emosLoginURL":              conf.Server.EmosURLLink,
		"variousArtistsId":          consts.VariousArtistsID,
		"baseURL":                   str.SanitizeText(strings.TrimSuffix(conf.Server.BasePath, "/")),
		"loginBackgroundURL":        str.SanitizeText(conf.Server.UILoginBackgroundURL),
		"welcomeMessage":            str.SanitizeHTML(conf.Server.UIWelcomeMessage),
		"maxSidebarPlaylists":       conf.Server.MaxSidebarPlaylists,
		"enableTranscodingConfig":   false,
		"enableDownloads":           conf.Server.EnableDownloads,
		"enableFavourites":          conf.Server.EnableFavourites,
		"enableStarRating":          conf.Server.EnableStarRating,
		"defaultTheme":              conf.Server.DefaultTheme,
		"defaultLanguage":           conf.Server.DefaultLanguage,
		"defaultUIVolume":           conf.Server.DefaultUIVolume,
		"uiSearchDebounceMs":        conf.Server.UISearchDebounceMs,
		"uiCoverArtSize":            conf.Server.UICoverArtSize,
		"enableCoverAnimation":      conf.Server.EnableCoverAnimation,
		"pidAlbum":                  conf.Server.PID.Album,
		"pidTrack":                  conf.Server.PID.Track,
		"enableNowPlaying":          conf.Server.EnableNowPlaying,
		"playbackReportIntervalMs":  conf.Server.UIPlaybackReportInterval.Milliseconds(),
		"gaTrackingId":              conf.Server.GATrackingID,
		"losslessFormats":           strings.ToUpper(strings.Join(mime.LosslessFormats, ",")),
		"devActivityPanel":          conf.Server.DevActivityPanel,
		"enableUserEditing":         conf.Server.EnableUserEditing,
		"enableArtworkUpload":       conf.Server.EnableArtworkUpload,
		"enableSharing":             conf.Server.EnableSharing,
		"shareURL":                  conf.Server.ShareURL,
		"defaultDownloadableShare":  conf.Server.DefaultDownloadableShare,
		"devSidebarPlaylists":       conf.Server.DevSidebarPlaylists,
		"lastFMEnabled":             conf.Server.LastFM.Enabled,
		"devShowArtistPage":         conf.Server.DevShowArtistPage,
		"devUIShowConfig":           conf.Server.DevUIShowConfig,
		"devNewEventStream":         conf.Server.DevNewEventStream,
		"listenBrainzEnabled":       conf.Server.ListenBrainz.Enabled,
		"enableExternalServices":    conf.Server.EnableExternalServices,
		"enableReplayGain":          conf.Server.EnableReplayGain,
		"enableQuickConnect":        quickconnect.Enabled(),
		"defaultDownsamplingFormat": conf.Server.DefaultDownsamplingFormat,
		"separator":                 string(os.PathSeparator),
		"enableInspect":             conf.Server.Inspect.Enabled,
		"pluginsEnabled":            conf.Server.Plugins.Enabled,
	}
	if strings.HasPrefix(conf.Server.UILoginBackgroundURL, "/") {
		appConfig["loginBackgroundURL"] = path.Join(conf.Server.BasePath, conf.Server.UILoginBackgroundURL)
	}
	_ = rest.RespondWithJSON(w, http.StatusOK, appConfig)
}
