package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/kubecron/kubecron/internal/auth"
	"github.com/kubecron/kubecron/internal/cluster"
	"github.com/kubecron/kubecron/internal/storage"
	"github.com/kubecron/kubecron/internal/streamer"
	"github.com/kubecron/kubecron/internal/ui/static"
)

// Server is the HTTP server for KubeCron.
type Server struct {
	store         storage.Store
	registry      *cluster.Registry
	broadcaster   *streamer.Broadcaster
	cacheSynced   func() bool
	authenticator *auth.Authenticator // nil when OIDC is disabled
	httpServer    *http.Server
}

// NewServer constructs a Server with the given dependencies.
// authenticator may be nil to disable OIDC protection.
func NewServer(
	store storage.Store,
	registry *cluster.Registry,
	broadcaster *streamer.Broadcaster,
	cacheSynced func() bool,
	authenticator *auth.Authenticator,
) *Server {
	return &Server{
		store:         store,
		registry:      registry,
		broadcaster:   broadcaster,
		cacheSynced:   cacheSynced,
		authenticator: authenticator,
	}
}

// Start registers all routes, applies middleware, and begins listening on port.
func (s *Server) Start(port int) error {
	h := &Handler{
		store:       s.store,
		registry:    s.registry,
		broadcaster: s.broadcaster,
		cacheSynced: s.cacheSynced,
	}

	mux := http.NewServeMux()

	// Static assets (CSS, fonts fallback)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(static.FS))))

	loginLimiter := RateLimit(10, time.Minute)
	triggerLimiter := RateLimit(20, time.Minute)

	// Auth routes
	if s.authenticator != nil {
		mux.Handle("GET /auth/login", loginLimiter(http.HandlerFunc(s.authenticator.HandleLogin)))
		mux.HandleFunc("GET /auth/callback", s.authenticator.HandleCallback)
		mux.HandleFunc("GET /auth/logout", s.authenticator.HandleLogout)
		mux.HandleFunc("GET /auth/logged-out", s.authenticator.HandleLoggedOut)
	}

	// API — JSON
	mux.HandleFunc("GET /api/clusters", h.ListClusters)
	mux.HandleFunc("GET /api/clusters/{clusterID}/cronjobs", h.ListCronJobs)
	mux.HandleFunc("GET /api/clusters/{clusterID}/cronjobs/{ns}/{name}/runs", h.ListRuns)
	mux.HandleFunc("GET /api/runs/{id}/stream", h.StreamLogs)
	mux.HandleFunc("GET /api/runs/{id}/resources", h.GetResourceSamples)
	mux.HandleFunc("GET /api/runs/{id}/logs.txt", h.DownloadLogs)
	mux.HandleFunc("POST /api/clusters/{clusterID}/cronjobs/{ns}/{name}/suspend", h.Suspend)
	mux.HandleFunc("POST /api/clusters/{clusterID}/cronjobs/{ns}/{name}/resume", h.Resume)
	mux.Handle("POST /api/clusters/{clusterID}/cronjobs/{ns}/{name}/trigger", triggerLimiter(http.HandlerFunc(h.Trigger)))

	// Observability
	mux.Handle("GET /metrics", promhttp.Handler())
	mux.HandleFunc("GET /healthz", h.Healthz)
	mux.HandleFunc("GET /readyz", h.Readyz)

	// UI — HTML pages
	mux.HandleFunc("GET /clusters/{clusterID}/cronjobs/{ns}/{name}/runs/more", h.RunsListMore)
	mux.HandleFunc("GET /clusters/{clusterID}/cronjobs/{ns}/{name}/runs/{id}", h.RunDetail)
	mux.HandleFunc("GET /clusters/{clusterID}/cronjobs/{ns}/{name}/runs", h.RunsList)
	mux.HandleFunc("GET /clusters/{clusterID}/cronjobs/{ns}/rows", h.NamespaceRows)
	mux.HandleFunc("GET /clusters/{clusterID}/cronjobs/{ns}", h.NamespaceDetail)
	mux.HandleFunc("GET /clusters/{clusterID}", h.ClusterDetail)
	mux.HandleFunc("GET /", h.Dashboard)

	// Build middleware chain; prepend OIDC middleware if enabled.
	middlewares := []func(http.Handler) http.Handler{Logger, Recover, EnsureCSRFCookie, CSRFProtect}
	if s.authenticator != nil {
		middlewares = append([]func(http.Handler) http.Handler{s.authenticator.Middleware}, middlewares...)
	}
	handler := Chain(mux, middlewares...)

	s.httpServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: handler,
	}

	return s.httpServer.ListenAndServe()
}

// Shutdown performs a graceful shutdown of the HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.httpServer == nil {
		return nil
	}
	return s.httpServer.Shutdown(ctx)
}

// Handler holds the dependencies needed by all HTTP handlers.
type Handler struct {
	store       storage.Store
	registry    *cluster.Registry
	broadcaster *streamer.Broadcaster
	cacheSynced func() bool
}
