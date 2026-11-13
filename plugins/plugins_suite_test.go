package plugins

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/conf/configtest"
	"github.com/navidrome/navidrome/db"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	testDataDir    = "plugins/testdata"
	wazeroCacheDir = ".wazero-cache"
)

// Shared test state initialized in BeforeSuite
var (
	testdataDir   string // Path to testdata folder with test plugin .ndp packages
	tmpPluginsDir string // Temp directory for plugin tests that modify files
	testManager   *Manager
)

func TestPlugins(t *testing.T) {
	tests.Init(t, false)
	tests.InitDB(t.Context())

	// Set globally so tests using configtest.SetupConfig inherit it. The cache
	// persists between runs; entries are content-addressed, so a stale one only misses.
	conf.Server.CacheFolder = conf.NewDir(filepath.Join(os.TempDir(), "plugins-wazero-cache"))
	conf.Server.Plugins.CacheSize = "1GB" // the default evicts the cache mid-run

	log.SetLevel(log.LevelFatal)
	RegisterFailHandler(Fail)
	RunSpecs(t, "Plugins Suite")
}

// stubLocalhostDNS resolves "localhost." without real DNS: the trailing dot never matches /etc/hosts.
func stubLocalhostDNS() {
	dialResolver = tests.StubResolver(map[string]string{"localhost.": "127.0.0.1"})
	DeferCleanup(func() { dialResolver = nil })
}

// createTestManager creates a new plugin Manager with the given plugin config.
// It creates a temp directory, copies the test-metadata-agent plugin, and starts the manager.
// Returns the manager, temp directory path, and a cleanup function.
func createTestManager(pluginConfig map[string]map[string]string) (*Manager, string) {
	return createTestManagerWithPlugins(pluginConfig, "test-metadata-agent"+PackageExtension)
}

// createTestManagerWithPlugins creates a new plugin Manager with the given plugin config
// and specified plugins. It creates a temp directory, copies the specified plugins, and starts the manager.
// Returns the manager and temp directory path.
func createTestManagerWithPlugins(pluginConfig map[string]map[string]string, plugins ...string) (*Manager, string) {
	return createTestManagerWithPluginsAndMetrics(pluginConfig, noopMetricsRecorder{}, plugins...)
}

// installTestPlugins copies the given .ndp packages into dir and returns their
// enabled DB rows, so callers can grant whatever access the test needs.
func installTestPlugins(dir string, plugins ...string) model.Plugins {
	var rows model.Plugins
	for _, plugin := range plugins {
		data, err := os.ReadFile(filepath.Join(testdataDir, plugin))
		Expect(err).ToNot(HaveOccurred())
		destPath := filepath.Join(dir, plugin)
		Expect(os.WriteFile(destPath, data, 0600)).To(Succeed())

		hash := sha256.Sum256(data)
		rows = append(rows, model.Plugin{
			ID:      strings.TrimSuffix(plugin, PackageExtension),
			Path:    destPath,
			SHA256:  hex.EncodeToString(hash[:]),
			Enabled: true,
		})
	}
	return rows
}

// createTestManagerWithPluginsAndMetrics creates a new plugin Manager with the given plugin config,
// metrics recorder, and specified plugins. It creates a temp directory, copies the specified plugins,
// and starts the manager. Returns the manager and temp directory path.
func createTestManagerWithPluginsAndMetrics(pluginConfig map[string]map[string]string, metrics PluginMetricsRecorder, plugins ...string) (*Manager, string) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "plugins-test-*")
	Expect(err).ToNot(HaveOccurred())

	enabledPlugins := installTestPlugins(tmpDir, plugins...)
	for i, p := range enabledPlugins {
		enabledPlugins[i].AllUsers = true // Allow all users by default in tests
		if pluginConfig[p.ID] != nil {
			configBytes, err := json.Marshal(pluginConfig[p.ID])
			Expect(err).ToNot(HaveOccurred())
			enabledPlugins[i].Config = string(configBytes)
		}
	}

	// Setup config
	DeferCleanup(configtest.SetupConfig())
	conf.Server.Plugins.Enabled = true
	conf.Server.Plugins.Folder = conf.NewDir(tmpDir)
	conf.Server.Plugins.AutoReload = false

	// Setup mock DataStore with pre-enabled plugins
	mockPluginRepo := tests.CreateMockPluginRepo()
	mockPluginRepo.Permitted = true
	mockPluginRepo.SetData(enabledPlugins)
	for _, enabled := range enabledPlugins {
		_, err := db.Db().ExecContext(GinkgoT().Context(), `INSERT INTO plugin(id,path,manifest,config,users,libraries,sha256,created_at,updated_at) VALUES($1,'','{}','{}','[]','[]','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP) ON CONFLICT(id) DO NOTHING`, enabled.ID)
		Expect(err).ToNot(HaveOccurred())
	}
	dataStore := &tests.MockDataStore{MockedPlugin: mockPluginRepo}

	// Create and start manager
	manager := &Manager{
		plugins:        make(map[string]*plugin),
		ds:             dataStore,
		metrics:        metrics,
		subsonicRouter: http.NotFoundHandler(), // Stub router for tests
	}
	err = manager.Start(GinkgoT().Context())
	Expect(err).ToNot(HaveOccurred())

	DeferCleanup(func() {
		_ = manager.Stop()
		_ = os.RemoveAll(tmpDir)
	})

	return manager, tmpDir
}

var _ = SynchronizedBeforeSuite(func() []byte {
	root, err := os.MkdirTemp(os.TempDir(), "plugin-fixtures-*")
	Expect(err).ToNot(HaveOccurred())
	Expect(os.CopyFS(filepath.Join(root, "testdata"), os.DirFS(testDataDir))).To(Succeed())
	Expect(os.CopyFS(filepath.Join(root, "pdk", "go"), os.DirFS("plugins/pdk/go"))).To(Succeed())
	buildTestPlugins(filepath.Join(root, "testdata"))
	return []byte(root)
}, func(root []byte) {
	testdataDir = filepath.Join(string(root), "testdata")
	testManager, tmpPluginsDir = createTestManager(nil)
})

var _ = AfterSuite(func() {
	if testManager != nil {
		_ = testManager.Stop()
	}
	if tmpPluginsDir != "" {
		_ = os.RemoveAll(tmpPluginsDir)
	}
})

// noopMetricsRecorder is a no-op implementation of PluginMetricsRecorder for tests
type noopMetricsRecorder struct{}

func (noopMetricsRecorder) RecordPluginRequest(context.Context, string, string, bool, int64) {}

func setupPluginStorage(ctx context.Context) {
	GinkgoHelper()
	tests.InitDB(ctx)
	for _, pluginID := range []string{"test_plugin", "other_plugin", "test_close_race"} {
		_, err := db.Db().ExecContext(ctx, "DELETE FROM plugin WHERE id=$1", pluginID)
		Expect(err).ToNot(HaveOccurred())
		_, err = db.Db().ExecContext(ctx, `INSERT INTO plugin(id,path,manifest,config,users,libraries,sha256,created_at,updated_at) VALUES($1,'','{}','{}','[]','[]','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`, pluginID)
		Expect(err).ToNot(HaveOccurred())
	}
}
