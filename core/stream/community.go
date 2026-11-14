package stream

import (
	"context"
	"database/sql"
	"net/http"
	"strings"

	"github.com/navidrome/navidrome/core/filedelivery"
	"github.com/navidrome/navidrome/db"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
)

// CommunityResource is an original uploaded file. The logical MediaFile ID never
// changes when a listener changes their preferred quality.
type CommunityResource struct {
	Path, MIME, Suffix, Codec, Quality      string
	Bitrate, SampleRate, BitDepth, Channels int
	Size                                    int64
	Duration                                float32
}

func SelectCommunityResource(ctx context.Context, songID string) (*CommunityResource, error) {
	user, _ := request.UserFrom(ctx)
	resource := &CommunityResource{}
	err := db.Db().QueryRowContext(ctx, `
        SELECT path, mime_type, suffix, codec, quality, bitrate, sample_rate, bit_depth, channels, size, duration
        FROM music_resource
        WHERE song_id = (WITH RECURSIVE resolved(id) AS (
            SELECT $1::text UNION SELECT a.target_id FROM music_song_alias a JOIN resolved r ON a.source_id=r.id
        ) SELECT id FROM resolved r WHERE NOT EXISTS(SELECT 1 FROM music_song_alias a WHERE a.source_id=r.id) LIMIT 1)
        ORDER BY (quality = COALESCE((SELECT value FROM user_props WHERE user_id = $2 AND key = 'music_quality'), 'high')) DESC,
                 is_default DESC, created_at, id
        LIMIT 1`, songID, user.ID).Scan(&resource.Path, &resource.MIME, &resource.Suffix, &resource.Codec, &resource.Quality, &resource.Bitrate, &resource.SampleRate, &resource.BitDepth, &resource.Channels, &resource.Size, &resource.Duration)
	if err == sql.ErrNoRows {
		return nil, model.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return resource, nil
}

func CommunityMedia(ctx context.Context, mf *model.MediaFile) (*model.MediaFile, error) {
	res, err := SelectCommunityResource(ctx, mf.ID)
	if err != nil {
		return nil, err
	}
	copy := *mf
	copy.Path = res.Path
	copy.LibraryPath = ""
	copy.Suffix = res.Suffix
	copy.Codec = res.Codec
	copy.BitRate = res.Bitrate
	copy.SampleRate = res.SampleRate
	copy.BitDepth = new(res.BitDepth)
	copy.Channels = res.Channels
	copy.Size = res.Size
	copy.Duration = res.Duration
	copy.ProbeData = ""
	return &copy, nil
}

// AccelPath forwards the database resource path using the same encoding as music_api.
func AccelPath(path string) (string, error) {
	return filedelivery.AccelPath(path)
}

func ServeCommunityCover(ctx context.Context, w http.ResponseWriter, r *http.Request, id string) (bool, error) {
	// Artwork IDs may include a cache key after the logical ID.
	if i := strings.Index(id, "?"); i >= 0 {
		id = id[:i]
	}

	if artworkID, err := model.ParseArtworkID(id); err == nil {
		id = artworkID.ID
	}
	user, _ := request.UserFrom(ctx)
	var allowed bool
	err := db.Db().QueryRowContext(ctx, `SELECT NOT EXISTS(SELECT 1 FROM playlist WHERE id=$1) OR EXISTS(SELECT 1 FROM playlist WHERE id=$1 AND deleted_at IS NULL AND (public OR owner_id=$2 OR $3 OR id IN (SELECT playlist_id FROM playlist_collaborator WHERE user_id=$2)))`, id, user.ID, user.IsAdmin && user.ID == "").Scan(&allowed)
	if err != nil {
		return false, err
	}
	if !allowed {
		return false, model.ErrNotFound
	}
	var path, mime, remote string
	err = db.Db().QueryRowContext(ctx, `SELECT path,mime_type,url FROM music_cover WHERE id=$1`, id).Scan(&path, &mime, &remote)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if path == "" && remote != "" {
		data, mime, err := filedelivery.RemoteImage(ctx, remote)
		if err != nil {
			return false, err
		}
		filedelivery.ServeImage(w, r, data, mime)
		return true, nil
	}
	if path != "" {
		if err := filedelivery.Redirect(w, path, mime); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}
