package public

import "net/http"

func (pub *Router) handleDownloads(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "Bulk archive downloads are disabled; download individual tracks", http.StatusNotImplemented)
}
