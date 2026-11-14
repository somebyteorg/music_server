// Package imghttp sends original images for the subsonic, public, and jellyfin handlers.
package imghttp

import (
	"net/http"

	"github.com/navidrome/navidrome/core/artwork"
	"github.com/navidrome/navidrome/core/filedelivery"
)

// ServeImage forwards database files or sends original bytes without caching or resizing.
func ServeImage(w http.ResponseWriter, r *http.Request, img *artwork.Image, requestedHash string) error {
	WriteImageHeaders(w, r, img, requestedHash)
	if img.Path != "" {
		return filedelivery.Redirect(w, img.Path, img.MIME)
	}
	data, mime, err := filedelivery.ReadImage(img)
	if err != nil {
		return err
	}
	filedelivery.ServeImage(w, r, data, mime)
	return nil
}

// WriteImageHeaders always disables caching, including for placeholders and versioned URLs.
func WriteImageHeaders(w http.ResponseWriter, _ *http.Request, _ *artwork.Image, _ string) bool {
	h := w.Header()
	h.Del("ETag")
	h.Del("Last-Modified")
	return false
}
