package nativeapi

import (
	"context"
	"io"
	"net/http"
)

func handleImageUpload(_ func(context.Context, io.Reader, string) error) http.HandlerFunc {
	return artworkWriteDisabled
}

func handleImageDelete(_ func(context.Context) error) http.HandlerFunc {
	return artworkWriteDisabled
}

func artworkWriteDisabled(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "Manage artwork in the music library", http.StatusForbidden)
}
