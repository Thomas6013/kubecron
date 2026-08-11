package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
	// Embed the IANA time-zone database. The distroless runtime image ships no
	// /usr/share/zoneinfo, so without this time.LoadLocation fails and every
	// CronJob spec.timeZone would be unresolvable (DOM-1).
	_ "time/tzdata"

	"github.com/caarlos0/env/v11"

	"github.com/kubecron/kubecron/internal/api"
	"github.com/kubecron/kubecron/internal/auth"
	"github.com/kubecron/kubecron/internal/cluster"
	"github.com/kubecron/kubecron/internal/metrics"
	"github.com/kubecron/kubecron/internal/sampler"
	"github.com/kubecron/kubecron/internal/storage"
	"github.com/kubecron/kubecron/internal/streamer"
	"github.com/kubecron/kubecron/internal/version"
	"github.com/kubecron/kubecron/internal/watcher"
)

// Config holds all runtime configuration sourced from environment variables.
type Config struct {
	KubeconfigDir         string `env:"KUBECONFIG_DIR"          envDefault:"/etc/kubecron/kubeconfigs"`
	DBPath                string `env:"DB_PATH"                 envDefault:"/data/kubecron.db"`
	Port                  int    `env:"PORT"                    envDefault:"8080"`
	RetentionDays         int    `env:"RETENTION_DAYS"          envDefault:"90"`
	LogRetentionDays      int    `env:"LOG_RETENTION_DAYS"      envDefault:"14"`
	MetricsSampleInterval int    `env:"METRICS_SAMPLE_INTERVAL" envDefault:"15"` // seconds
	OIDC                  auth.Config
}

func main() {
	// 1. Parse config from environment.
	cfg := Config{}
	if err := env.Parse(&cfg); err != nil {
		slog.Error("config parse failed", "err", err)
		os.Exit(1)
	}

	// 2. Structured JSON logging.
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))
	slog.Info("kubecron starting", "port", cfg.Port, "db", cfg.DBPath)

	// Root context — cancelled on SIGTERM / SIGINT.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	// 3. Open SQLite and run migrations.
	store, err := storage.Open(cfg.DBPath)
	if err != nil {
		slog.Error("database open failed", "err", err)
		os.Exit(1)
	}

	// 4. Background retention goroutine.
	go storage.StartRetention(ctx, store, cfg.RetentionDays, cfg.LogRetentionDays)

	// 5. Load kubeconfigs → one ClusterClient per file.
	mgr := cluster.NewManager(store, cfg.KubeconfigDir)
	if err := mgr.Load(ctx); err != nil {
		// Non-fatal: some clusters may have failed; others are still loaded.
		slog.Warn("cluster loading completed with errors", "err", err)
	}

	// 6. Restart recovery: mark any run still 'running' in the DB as failed
	// before informers start — this avoids a race where OnAdd fires before cleanup.
	if runs, err := store.GetRunningRuns(ctx); err == nil && len(runs) > 0 {
		for _, r := range runs {
			if err := store.MarkRunFailed(ctx, r.ID); err != nil {
				slog.Warn("failed to mark stale run as failed", "run_id", r.ID, "err", err)
			}
		}
		slog.Info("marked stale runs as failed on startup", "count", len(runs))
	}

	// 7. Shared broadcaster and streamer (single instance, cross-cluster).
	broadcaster := streamer.NewBroadcaster()
	logStreamer := streamer.NewStreamer(store, broadcaster)

	sampleInterval := time.Duration(cfg.MetricsSampleInterval) * time.Second

	// 8. Per-cluster: probe Metrics API + start informer controller.
	var controllers []*watcher.Controller

	for _, cc := range mgr.Registry().All() {
		// Probe the Metrics API availability (background re-probe every 5 min).
		// On success, flip the in-memory flag the pod watcher reads to gate
		// resource sampling.
		sampler.StartProbe(ctx, cc.ID, cc.MetricsClient, store, func() { cc.SetMetricsEnabled(true) })

		s := sampler.NewSampler(store, cc.MetricsClient, sampleInterval)

		ctrl := watcher.NewController(cc, store, logStreamer, s)
		if err := ctrl.Start(ctx); err != nil {
			slog.Error("watcher start failed", "cluster", cc.ID, "err", err)
			continue
		}
		controllers = append(controllers, ctrl)
		slog.Info("watcher started", "cluster", cc.ID)
	}

	// cacheSynced is true once all informer caches have completed their initial
	// list — used by /readyz to gate traffic.
	cacheSynced := func() bool {
		for _, ctrl := range controllers {
			if !ctrl.CacheSynced() {
				return false
			}
		}
		return true
	}

	// 9. Optional OIDC authenticator.
	var authenticator *auth.Authenticator
	if cfg.OIDC.Enabled {
		authenticator, err = auth.NewAuthenticator(ctx, cfg.OIDC)
		if err != nil {
			slog.Error("OIDC initialisation failed", "err", err)
			os.Exit(1)
		}
		slog.Info("OIDC authentication enabled", "issuer", cfg.OIDC.IssuerURL)
	} else {
		// The Helm chart refuses to install an externally-reachable release with
		// OIDC off (SEC-28), but nothing stops a raw manifest, a Compose file or
		// a `go run` from doing it. Say so once, loudly, at Warn: without OIDC
		// the operator gate in api.Server is a pass-through and the auth
		// middleware is never installed, so suspend/resume/trigger are anonymous
		// on every cluster whose kubeconfig is mounted.
		slog.Warn("OIDC is disabled — all endpoints are UNAUTHENTICATED, including suspend/resume/trigger on every connected cluster; do not expose this service outside a trusted network",
			"remediation", "set OIDC_ENABLED=true")
	}

	// 10. Prometheus state collector. Republishes the gauge-valued metrics from
	// stored state on a ticker so that they survive a restart: the watcher
	// wiring alone only reacts to live events, leaving run-outcome gauges with
	// no series at all until each CronJob next fires.
	metrics.SetBuildInfo(version.Version)
	go metrics.NewStateCollector(store, metrics.DefaultCollectInterval).Run(ctx)

	// 11. HTTP server.
	srv := api.NewServer(store, mgr.Registry(), broadcaster, cacheSynced, authenticator)

	go func() {
		slog.Info("http server listening", "port", cfg.Port)
		if err := srv.Start(cfg.Port); err != nil {
			slog.Error("http server exited", "err", err)
		}
	}()

	// 12. Block until shutdown signal, then drain gracefully.
	<-ctx.Done()
	slog.Info("shutdown signal received, draining (timeout 30s)…")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("graceful shutdown error", "err", err)
	}

	// Close after the HTTP server has drained: SQLite checkpoints the WAL into
	// the main file on the last connection close.
	if err := store.Close(); err != nil {
		slog.Error("database close error", "err", err)
	}

	slog.Info("kubecron stopped")
}
