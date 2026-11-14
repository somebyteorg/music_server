package stream

import (
	"context"
	"strings"

	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/model"
)

// buildLegacyClientInfo translates legacy Subsonic stream/download parameters
// into a ClientInfo for use with MakeDecision.
func buildLegacyClientInfo(mf *model.MediaFile, reqFormat string, reqBitRate int, playerMaxBitRate int) *ClientInfo {
	ci := &ClientInfo{Name: "legacy"}

	// Determine target format for transcoding
	var targetFormat string
	switch {
	case reqFormat != "":
		targetFormat = reqFormat
	case reqBitRate > 0 && reqBitRate < mf.BitRate && conf.Server.DefaultDownsamplingFormat != "":
		targetFormat = conf.Server.DefaultDownsamplingFormat
	case playerMaxBitRate > 0 && playerMaxBitRate < mf.BitRate && conf.Server.DefaultDownsamplingFormat != "":
		// Server-side player MaxBitRate alone forces downsampling, even when the
		// client sent no format/bitrate params (issue #5583, legacy /stream path).
		targetFormat = conf.Server.DefaultDownsamplingFormat
	}

	if targetFormat != "" {
		// Add a direct play profile for the source format when no explicit
		// format was requested (bitrate-only downsampling) or when the
		// requested format matches the source. When the client explicitly
		// requests a different format, direct play must not match the
		// source — otherwise the source is returned untranscoded.
		if reqFormat == "" || strings.EqualFold(reqFormat, mf.Suffix) {
			ci.DirectPlayProfiles = []DirectPlayProfile{
				{Containers: []string{mf.Suffix}, AudioCodecs: []string{mf.AudioCodec()}, Protocols: []string{ProtocolHTTP}},
			}
		}
		ci.TranscodingProfiles = []Profile{
			{Container: targetFormat, AudioCodec: targetFormat, Protocol: ProtocolHTTP},
		}
		if reqBitRate > 0 {
			ci.MaxAudioBitrate = reqBitRate
			ci.MaxTranscodingAudioBitrate = reqBitRate
		}
	} else {
		// No transcoding requested — direct play everything
		ci.DirectPlayProfiles = []DirectPlayProfile{
			{Protocols: []string{ProtocolHTTP}},
		}
	}

	return ci
}

// ResolveRequest ignores conversion settings: playback always uses an original file.
func (s *deciderService) ResolveRequest(ctx context.Context, mf *model.MediaFile, reqFormat string, reqBitRate int, offset int) Request {
	return Request{Format: "raw"}
}

// ResolveClientRequest also ignores client transcoding profiles and bitrate limits.
func (s *deciderService) ResolveClientRequest(ctx context.Context, mf *model.MediaFile, clientInfo *ClientInfo, offset int) Request {
	return Request{Format: "raw"}
}
