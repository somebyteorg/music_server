package tests

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/onsi/gomega/format"

	"github.com/navidrome/navidrome/conf"
	_ "github.com/navidrome/navidrome/conf/mime" // registers mime_types.yaml, so tests see the same image types as the server
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model/id"
)

var once sync.Once

func Init(t testing.TB, skipOnShort bool) {
	if skipOnShort && testing.Short() {
		t.Skip("skipping test in short mode.")
	}
	once.Do(func() {
		format.TruncatedDiff = false
		_, file, _, _ := runtime.Caller(0)
		appPath, _ := filepath.Abs(filepath.Join(filepath.Dir(file), ".."))
		confPath, _ := filepath.Abs(filepath.Join(appPath, "tests", "navidrome-test.toml"))
		println("Loading test configuration file from " + confPath)
		_ = os.Chdir(appPath)
		conf.LoadFromFile(confPath)
		tmpRoot := filepath.Join(appPath, "..", "tmp", "tests")
		if err := os.MkdirAll(tmpRoot, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Setenv("TMPDIR", tmpRoot); err != nil {
			t.Fatal(err)
		}
		conf.Server.DataFolder = conf.NewDir(filepath.Join(tmpRoot, "data-"+id.NewRandom()))
		conf.Server.CacheFolder = conf.NewDir(filepath.Join(conf.Server.DataFolder.String(), "cache"))
		conf.Server.Backup.Path = conf.NewDir(filepath.Join(conf.Server.DataFolder.String(), "backups"))
		conf.Server.DatabaseSchema = "navidrome_test_" + strings.ToLower(id.NewRandom())
		conf.Server.DatabaseMaxConnections = 4
		testDatabaseURL = conf.Server.DatabaseURL
		conf.Server.EnableExternalServices = false

		noLog := os.Getenv("NOLOG")
		if noLog != "" {
			log.SetLevel(log.LevelError)
		}
	})
	registerDatabaseCleanup(t)
}
