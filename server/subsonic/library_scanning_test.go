package subsonic

import (
	"context"
	"net/http/httptest"

	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("LibraryScanning", func() {
	var api *Router
	var ms *tests.MockScanner

	BeforeEach(func() {
		ms = tests.NewMockScanner()
		api = &Router{scanner: ms}
	})

	Describe("GetScanStatus", func() {
		It("returns scan status", func() {
			// Setup mock scanner status
			ms.SetStatusResponse(&model.ScannerStatus{
				Scanning:    false,
				Count:       100,
				FolderCount: 10,
			})

			// Create request
			ctx := context.Background()
			r := httptest.NewRequest("GET", "/rest/getScanStatus", nil)
			r = r.WithContext(ctx)

			// Call endpoint
			response, err := api.GetScanStatus(r)

			// Should succeed
			Expect(err).ToNot(HaveOccurred())
			Expect(response).ToNot(BeNil())
			Expect(response.ScanStatus).ToNot(BeNil())
			Expect(response.ScanStatus.Scanning).To(BeFalse())
			Expect(response.ScanStatus.Count).To(Equal(int64(100)))
			Expect(response.ScanStatus.FolderCount).To(Equal(int64(10)))
		})
	})
})
