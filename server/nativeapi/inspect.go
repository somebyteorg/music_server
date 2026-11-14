package nativeapi

import (
	"net/http"

	"github.com/deluan/rest"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
)

func inspect(_ model.DataStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		response := map[string]any{
			"file":       "",
			"rawTags":    model.RawTags{},
			"mappedTags": map[string]any{},
		}
		if err := rest.RespondWithJSON(w, http.StatusOK, response); err != nil {
			log.Error(r.Context(), "Error writing inspect response", err)
		}
	}
}
