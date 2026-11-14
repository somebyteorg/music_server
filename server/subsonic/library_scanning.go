package subsonic

import (
	"net/http"

	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/server/subsonic/responses"
)

func (api *Router) GetScanStatus(r *http.Request) (*responses.Subsonic, error) {
	ctx := r.Context()
	status, err := api.scanner.Status(ctx)
	if err != nil {
		log.Error(ctx, "Error retrieving Scanner status", err)
		return nil, newError(responses.ErrorGeneric, "Internal Error")
	}
	response := newResponse()
	response.ScanStatus = &responses.ScanStatus{
		Scanning:    status.Scanning,
		Count:       int64(status.Count),
		FolderCount: int64(status.FolderCount),
		LastScan:    &status.LastScan,
		Error:       status.LastError,
		ScanType:    status.ScanType,
		ElapsedTime: int64(status.ElapsedTime),
	}
	return response, nil
}

// StartScan rejects requests because music libraries are maintained by the community web application.
func (api *Router) StartScan(_ *http.Request) (*responses.Subsonic, error) {
	return nil, newError(responses.ErrorAuthorizationFail, "音乐库由跃律共建网页维护，扫描已关闭")
}
