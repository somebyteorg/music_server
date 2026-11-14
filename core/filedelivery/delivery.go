// Package filedelivery serves original resources from database paths and URLs.
package filedelivery

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/navidrome/navidrome/utils/httpclient"
	_ "golang.org/x/image/webp"
)

const maxImageSize = 20 * 1024 * 1024

var remoteImageClient = httpclient.NewExternal(15 * time.Second)

func AccelPath(path string) (string, error) {
	if path == "" || strings.ContainsAny(path, "\\\x00\r\n") {
		return "", fmt.Errorf("invalid resource path")
	}
	segments := strings.Split(path, "/")
	for i, segment := range segments {
		if segment == "" || segment == "." || segment == ".." {
			return "", fmt.Errorf("invalid resource path")
		}
		segments[i] = strings.ReplaceAll(url.QueryEscape(segment), "+", "%20")
	}
	return "/" + strings.Join(segments, "/"), nil
}

func Redirect(w http.ResponseWriter, path, mime string) error {
	redirect, err := AccelPath(path)
	if err != nil {
		return err
	}
	w.Header().Set("X-Accel-Redirect", redirect)
	w.Header().Set("Content-Type", mime)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	return nil
}

// ReadImage validates an image without changing or storing its bytes.
func ReadImage(reader io.Reader) ([]byte, string, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maxImageSize+1))
	if err != nil {
		return nil, "", err
	}
	if len(data) > maxImageSize {
		return nil, "", fmt.Errorf("cover exceeds size limit")
	}
	info, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || info.Width <= 0 || info.Height <= 0 || info.Width > 16000 || info.Height > 16000 {
		return nil, "", fmt.Errorf("invalid cover image")
	}
	return data, http.DetectContentType(data), nil
}

// ServeImage sends the original image and prevents response caching.
func ServeImage(w http.ResponseWriter, r *http.Request, data []byte, mime string) {
	w.Header().Set("Content-Type", mime)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(data))
}

func RemoteImage(ctx context.Context, remote string) ([]byte, string, error) {
	u, err := url.Parse(remote)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil {
		return nil, "", fmt.Errorf("invalid cover URL")
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, remote, nil)
	if err != nil {
		return nil, "", err
	}
	response, err := remoteImageClient.Do(r)
	if err != nil {
		return nil, "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("cover download returned %d", response.StatusCode)
	}
	return ReadImage(response.Body)
}
