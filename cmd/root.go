package cmd

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/consts"
	"github.com/navidrome/navidrome/db"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/resources"
	"github.com/navidrome/navidrome/scanner"
	"github.com/navidrome/navidrome/scheduler"
	"github.com/navidrome/navidrome/server/backgrounds"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/sync/errgroup"

	// Import adapters to register them
	_ "github.com/navidrome/navidrome/adapters/deezer"
	_ "github.com/navidrome/navidrome/adapters/gotaglib"
	_ "github.com/navidrome/navidrome/adapters/lastfm"
	_ "github.com/navidrome/navidrome/adapters/listenbrainz"
)

var (
	cfgFile  string
	noBanner bool

	rootCmd = &cobra.Command{
		Use:   "navidrome",
		Short: "Navidrome is a self-hosted music server and streamer",
		Long: `Navidrome is a self-hosted music server and streamer.
Complete documentation is available at https://www.navidrome.org/docs`,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			preRun()
		},
		Run: func(cmd *cobra.Command, args []string) {
			if err := runNavidrome(cmd.Context()); err != nil {
				log.Fatal("Fatal error in Navidrome. Aborting", err)
			}
		},
		PostRun: func(cmd *cobra.Command, args []string) {
			postRun()
		},
		Version: consts.Version,
	}
)

// Execute runs the root cobra command, which will start the Navidrome server by calling the runNavidrome function.
func Execute() {
	ctx, cancel := mainContext(context.Background())
	defer cancel()

	rootCmd.SetVersionTemplate(`{{println .Version}}`)
	if err := rootCmd.ExecuteContext(ctx); err != nil {
		log.Fatal(err)
	}
}

func preRun() {
	if !noBanner {
		println(resources.Banner())
	}
	conf.Load(noBanner)
}

func postRun() {
	log.Info("Navidrome stopped, bye.")
}

// runNavidrome is the main entry point for the Navidrome server. It starts all the services and blocks.
// If any of the services returns an error, it stops the others and returns that error, so the caller can
// exit with a non-zero code. If the context is cancelled (a signal or a service stop), it returns nil.
func runNavidrome(parentCtx context.Context) error {
	defer db.Init(parentCtx)()

	// Music library updates are handled by the community web application.
	g, ctx := errgroup.WithContext(parentCtx)
	g.Go(startServer(ctx))
	g.Go(startScheduler(ctx))
	g.Go(startPlaybackServer(ctx))
	g.Go(startJellyfinDiscovery(ctx))
	g.Go(schedulePeriodicBackup(ctx))
	g.Go(startInsightsCollector(ctx))
	g.Go(scheduleDBAnalyzer(ctx))
	g.Go(startPluginManager(ctx))

	// Errors caused by a normal shutdown are not failures
	if err := g.Wait(); err != nil && parentCtx.Err() == nil {
		return err
	}
	return nil
}

// mainContext returns a context that is cancelled when the process receives a signal to exit.
func mainContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(ctx,
		os.Interrupt,
		syscall.SIGHUP,
		syscall.SIGTERM,
		syscall.SIGABRT,
	)
}

// startServer starts the Navidrome web server, adding all the necessary routers.
func startServer(ctx context.Context) func() error {
	return func() error {
		a := CreateServer()
		a.MountRouter("Native API", consts.URLPathNativeAPI, CreateNativeAPIRouter(ctx))
		a.MountRouter("Subsonic API", consts.URLPathSubsonicAPI, CreateSubsonicAPIRouter(ctx))
		a.MountRouter("Public Endpoints", consts.URLPathPublic, CreatePublicRouter())
		if conf.Server.LastFM.Enabled {
			a.MountRouter("LastFM Auth", consts.URLPathNativeAPI+"/lastfm", CreateLastFMRouter())
		}
		if conf.Server.ListenBrainz.Enabled {
			a.MountRouter("ListenBrainz Auth", consts.URLPathNativeAPI+"/listenbrainz", CreateListenBrainzRouter())
		}
		if conf.Server.Jellyfin.Enabled {
			a.MountRouter("Jellyfin API", consts.URLPathJellyfinAPI, CreateJellyfinAPIRouter(ctx))
		}
		if conf.Server.DevAPIv1 {
			a.MountRouter("API v1", consts.URLPathAPIv1, CreateAPIv1Router(ctx))
		}
		if conf.Server.Prometheus.Enabled {
			p := CreatePrometheus()
			// blocking call because takes <100ms but useful if fails
			p.WriteInitialMetrics(ctx)
			a.MountRouter("Prometheus metrics", conf.Server.Prometheus.MetricsPath, p.GetHandler())
		}
		if conf.Server.DevEnableProfiler {
			a.MountRouter("Profiling", "/debug", profilerHandler())
		}
		if strings.HasPrefix(conf.Server.UILoginBackgroundURL, "/") {
			a.MountRouter("Background images", conf.Server.UILoginBackgroundURL, backgrounds.NewHandler())
		}
		return a.Run(ctx, conf.Server.Address, conf.Server.Port, conf.Server.TLSCert, conf.Server.TLSKey)
	}
}

// profilerHandler returns the pprof handler. net/http/pprof resolves the profile
// name from the raw request path, so the BasePath has to come off first.
func profilerHandler() http.Handler {
	// A trailing or root slash would make StripPrefix drop the leading slash chi needs.
	basePath := strings.TrimRight(conf.Server.BasePath, "/")
	return http.StripPrefix(basePath, middleware.Profiler())
}

func schedulePeriodicBackup(ctx context.Context) func() error {
	return func() error {
		schedule := conf.Server.Backup.Schedule
		if schedule == "" {
			log.Info(ctx, "Periodic backup is DISABLED")
			return nil
		}

		schedulerInstance := scheduler.GetInstance()

		log.Info("Scheduling periodic backup", "schedule", schedule)
		_, err := schedulerInstance.Add(schedule, func() {
			start := time.Now()
			path, err := db.Backup(ctx)
			elapsed := time.Since(start)
			if err != nil {
				log.Error(ctx, "Error backing up database", "elapsed", elapsed, err)
				return
			}
			log.Info(ctx, "Backup complete", "elapsed", elapsed, "path", path)

			count, err := db.Prune(ctx)
			if err != nil {
				log.Error(ctx, "Error pruning database", "error", err)
			} else if count > 0 {
				log.Info(ctx, "Successfully pruned old files", "count", count)
			} else {
				log.Info(ctx, "No backups pruned")
			}
		})

		return err
	}
}

func scheduleDBAnalyzer(ctx context.Context) func() error {
	return func() error {
		if !conf.Server.EnableScheduledDBAnalyze {
			log.Info(ctx, "Scheduled DB analysis is DISABLED")
			return nil
		}
		log.Info(ctx, "Scheduling DB analysis check", "schedule", consts.DBAnalyzeCheckSchedule)
		schedulerInstance := scheduler.GetInstance()
		_, err := schedulerInstance.Add(consts.DBAnalyzeCheckSchedule, func() {
			release, ok := scanner.LockForMaintenance()
			if !ok {
				log.Debug(ctx, "Skipping DB analysis check because a scan is in progress")
				return
			}
			defer release()
			if _, err := db.OptimizeIfNeeded(ctx); err != nil {
				log.Error(ctx, "Error analyzing DB", err)
			}
		})
		return err
	}
}

// startScheduler starts the Navidrome scheduler, which is used to run periodic tasks.
func startScheduler(ctx context.Context) func() error {
	return func() error {
		log.Info(ctx, "Starting scheduler")
		schedulerInstance := scheduler.GetInstance()
		schedulerInstance.Run(ctx)
		return nil
	}
}

// startInsightsCollector starts the Navidrome Insight Collector, if configured.
func startInsightsCollector(ctx context.Context) func() error {
	return func() error {
		if !conf.Server.EnableInsightsCollector {
			log.Info(ctx, "Insight Collector is DISABLED")
			return nil
		}
		log.Info(ctx, "Starting Insight Collector")
		select {
		case <-time.After(conf.Server.DevInsightsInitialDelay):
		case <-ctx.Done():
			return nil
		}
		ic := CreateInsights()
		ic.Run(ctx)
		return nil
	}
}

// startJellyfinDiscovery never returns an error: a discovery failure must not stop the server.
func startJellyfinDiscovery(ctx context.Context) func() error {
	return func() error {
		if !conf.Server.Jellyfin.Enabled || !conf.Server.Jellyfin.AutoDiscovery {
			log.Debug("Jellyfin auto-discovery is DISABLED")
			return nil
		}
		CreateJellyfinDiscovery().Serve(ctx)
		return nil
	}
}

// startPlaybackServer starts the Navidrome playback server, if configured.
// It is responsible for the Jukebox functionality
func startPlaybackServer(ctx context.Context) func() error {
	return func() error {
		if !conf.Server.Jukebox.Enabled {
			log.Debug("Jukebox is DISABLED")
			return nil
		}
		log.Info(ctx, "Starting Jukebox service")
		playbackInstance := GetPlaybackServer()
		return playbackInstance.Run(ctx)
	}
}

// startPluginManager starts the plugin manager, if configured.
func startPluginManager(ctx context.Context) func() error {
	return func() error {
		manager := GetPluginManager(ctx)
		if !conf.Server.Plugins.Enabled {
			log.Debug("Plugin system is DISABLED")
			return nil
		}
		log.Info(ctx, "Starting plugin manager")
		return manager.Start(ctx)
	}
}

// TODO: Implement some struct tags to map flags to viper
func init() {
	cobra.OnInitialize(func() {
		conf.InitConfig(cfgFile, true)
	})

	rootCmd.PersistentFlags().StringVarP(&cfgFile, "configfile", "c", "", `config file (default "./navidrome.toml")`)
	rootCmd.PersistentFlags().BoolVarP(&noBanner, "nobanner", "n", false, `don't show banner`)
	rootCmd.PersistentFlags().String("musicfolder", viper.GetString("musicfolder"), "folder where your music is stored")
	rootCmd.PersistentFlags().String("datafolder", viper.GetString("datafolder"), "folder to store application data (DB), needs write access")
	rootCmd.PersistentFlags().String("cachefolder", viper.GetString("cachefolder"), "folder to store cache data (transcoding, images...), needs write access")
	rootCmd.PersistentFlags().StringP("loglevel", "l", viper.GetString("loglevel"), "log level, possible values: error, info, debug, trace")
	rootCmd.PersistentFlags().String("logfile", viper.GetString("logfile"), "log file path, if not set logs will be printed to stderr")

	_ = viper.BindPFlag("musicfolder", rootCmd.PersistentFlags().Lookup("musicfolder"))
	_ = viper.BindPFlag("datafolder", rootCmd.PersistentFlags().Lookup("datafolder"))
	_ = viper.BindPFlag("cachefolder", rootCmd.PersistentFlags().Lookup("cachefolder"))
	_ = viper.BindPFlag("loglevel", rootCmd.PersistentFlags().Lookup("loglevel"))
	_ = viper.BindPFlag("logfile", rootCmd.PersistentFlags().Lookup("logfile"))

	rootCmd.Flags().StringP("address", "a", viper.GetString("address"), "IP address to bind to")
	rootCmd.Flags().IntP("port", "p", viper.GetInt("port"), "HTTP port Navidrome will listen to")
	rootCmd.Flags().String("baseurl", viper.GetString("baseurl"), "base URL to configure Navidrome behind a proxy (ex: /music or http://my.server.com)")
	rootCmd.Flags().String("tlscert", viper.GetString("tlscert"), "optional path to a TLS cert file (enables HTTPS listening)")
	rootCmd.Flags().String("unixsocketperm", viper.GetString("unixsocketperm"), "optional file permission for the unix socket")
	rootCmd.Flags().String("tlskey", viper.GetString("tlskey"), "optional path to a TLS key file (enables HTTPS listening)")

	rootCmd.Flags().Duration("sessiontimeout", viper.GetDuration("sessiontimeout"), "how long Navidrome will wait before closing web ui idle sessions")
	rootCmd.Flags().String("uiloginbackgroundurl", viper.GetString("uiloginbackgroundurl"), "URL to a backaground image used in the Login page")
	rootCmd.Flags().Bool("enabletranscodingconfig", viper.GetBool("enabletranscodingconfig"), "enables transcoding configuration in the UI")
	rootCmd.Flags().Bool("enabletranscodingcancellation", viper.GetBool("enabletranscodingcancellation"), "enables transcoding context cancellation")
	rootCmd.Flags().String("transcodingcachesize", viper.GetString("transcodingcachesize"), "size of transcoding cache")
	rootCmd.Flags().String("imagecachesize", viper.GetString("imagecachesize"), "size of image (art work) cache. set to 0 to disable cache")
	rootCmd.Flags().String("albumplaycountmode", viper.GetString("albumplaycountmode"), "how to compute playcount for albums. absolute (default) or normalized")
	rootCmd.Flags().Bool("autoimportplaylists", viper.GetBool("autoimportplaylists"), "enable/disable .m3u playlist auto-import`")

	rootCmd.Flags().Bool("prometheus.enabled", viper.GetBool("prometheus.enabled"), "enable/disable prometheus metrics endpoint")
	rootCmd.Flags().String("prometheus.metricspath", viper.GetString("prometheus.metricspath"), "http endpoint for prometheus metrics")

	_ = viper.BindPFlag("address", rootCmd.Flags().Lookup("address"))
	_ = viper.BindPFlag("port", rootCmd.Flags().Lookup("port"))
	_ = viper.BindPFlag("tlscert", rootCmd.Flags().Lookup("tlscert"))
	_ = viper.BindPFlag("unixsocketperm", rootCmd.Flags().Lookup("unixsocketperm"))
	_ = viper.BindPFlag("tlskey", rootCmd.Flags().Lookup("tlskey"))
	_ = viper.BindPFlag("baseurl", rootCmd.Flags().Lookup("baseurl"))

	_ = viper.BindPFlag("sessiontimeout", rootCmd.Flags().Lookup("sessiontimeout"))
	_ = viper.BindPFlag("uiloginbackgroundurl", rootCmd.Flags().Lookup("uiloginbackgroundurl"))

	_ = viper.BindPFlag("prometheus.enabled", rootCmd.Flags().Lookup("prometheus.enabled"))
	_ = viper.BindPFlag("prometheus.metricspath", rootCmd.Flags().Lookup("prometheus.metricspath"))

	_ = viper.BindPFlag("enabletranscodingconfig", rootCmd.Flags().Lookup("enabletranscodingconfig"))
	_ = viper.BindPFlag("enabletranscodingcancellation", rootCmd.Flags().Lookup("enabletranscodingcancellation"))
	_ = viper.BindPFlag("transcodingcachesize", rootCmd.Flags().Lookup("transcodingcachesize"))
	_ = viper.BindPFlag("imagecachesize", rootCmd.Flags().Lookup("imagecachesize"))
}
