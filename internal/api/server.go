package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
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
	info          CollectorInfo
	apiToken      string // bearer token for server mode; empty = unauthenticated
	httpServer    *http.Server
}

// NewServer constructs a Server with the given dependencies.
//
// authenticator may be nil to disable OIDC protection. info.Mode decides which
// half of the HTTP surface Start registers; apiToken, when non-empty, is the
// bearer token every request must carry in server mode.
func NewServer(
	store storage.Store,
	registry *cluster.Registry,
	broadcaster *streamer.Broadcaster,
	cacheSynced func() bool,
	authenticator *auth.Authenticator,
	info CollectorInfo,
	apiToken string,
) *Server {
	return &Server{
		store:         store,
		registry:      registry,
		broadcaster:   broadcaster,
		cacheSynced:   cacheSynced,
		authenticator: authenticator,
		info:          info,
		apiToken:      apiToken,
	}
}

// Start registers all routes, applies middleware, and begins listening on port.
func (s *Server) Start(port int) error {
	handler := s.buildHandler()

	s.httpServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: handler,
		// No WriteTimeout: SSE streams (/api/v1/runs/{id}/stream) are long-lived.
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	return s.httpServer.ListenAndServe()
}

// buildHandler assembles the mux and the middleware chain for the configured
// mode. It is separate from Start so tests can exercise the routing decisions
// without binding a port.
func (s *Server) buildHandler() http.Handler {
	h := &Handler{
		store:       s.store,
		registry:    s.registry,
		broadcaster: s.broadcaster,
		cacheSynced: s.cacheSynced,
		info:        s.info,
	}

	mux := http.NewServeMux()

	// The versioned collector API. Registered in both modes: the standalone
	// product answering the same contract is what lets an operator point
	// KubeDeck at a KubeCron they already run, rather than deploying a second
	// one beside it.
	s.registerCollectorAPI(mux, h)

	// Observability and probes, in both modes.
	mux.Handle("GET /metrics", promhttp.Handler())
	mux.HandleFunc("GET /healthz", h.Healthz)
	mux.HandleFunc("GET /readyz", h.Readyz)

	if s.info.Mode.ServesUI() {
		s.registerUI(mux, h)
	} else {
		// Server mode answers no HTML at all, so an unmatched path must not
		// fall through to a page. A JSON 404 naming the mode is what a
		// misconfigured consumer needs to see.
		mux.HandleFunc("GET /", h.modeNotFound)
	}

	return Chain(mux, s.middlewares()...)
}

// registerCollectorAPI registers the /api/v1 contract read by KubeDeck.
//
// Every route here is a GET. That is the whole of collector mode's read-only
// property: there is no mutating route to reach, in either mode, under this
// prefix.
func (s *Server) registerCollectorAPI(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("GET /api/v1/collector", h.Collector)
	mux.HandleFunc("GET /api/v1/clusters", h.ListClustersV1)
	mux.HandleFunc("GET /api/v1/clusters/{clusterID}/cronjobs", h.ListCronJobsV1)
	mux.HandleFunc("GET /api/v1/clusters/{clusterID}/cronjobs/{ns}/{name}/runs", h.ListRunsV1)
	mux.HandleFunc("GET /api/v1/clusters/{clusterID}/cronjobs/{ns}/{name}/daily", h.DailyRunStatsV1)
	mux.HandleFunc("GET /api/v1/runs/{id}", h.GetRunV1)
	mux.HandleFunc("GET /api/v1/runs/{id}/samples", h.GetResourceSamplesV1)
	mux.HandleFunc("GET /api/v1/runs/{id}/logs", h.GetLogsV1)
	mux.HandleFunc("GET /api/v1/runs/{id}/logs.txt", h.DownloadLogsV1)
	mux.HandleFunc("GET /api/v1/runs/{id}/stream", h.StreamLogs)
	// Anything else under the prefix answers in the contract's own vocabulary
	// rather than falling through to the UI's 404 or, in server mode, to
	// modeNotFound.
	mux.HandleFunc("GET /api/v1/", h.v1NotFound)
}

// registerUI registers the dashboard, its static assets, the unversioned JSON
// API that backs its HTMX fragments, and the CronJob controls.
//
// None of this exists in server mode. The mutating routes in particular are
// absent rather than merely hidden: a collector that could suspend a CronJob
// would be a second authorization model to keep in agreement with KubeDeck's,
// and KubeCron's is off by default.
func (s *Server) registerUI(mux *http.ServeMux, h *Handler) {
	// Static assets (CSS, fonts fallback)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(static.FS))))

	loginLimiter := RateLimit(10, time.Minute)
	triggerLimiter := RateLimit(20, time.Minute)

	// Auth routes — a browser redirect flow, meaningless without pages to
	// redirect to.
	if s.authenticator != nil {
		mux.Handle("GET /auth/login", loginLimiter(http.HandlerFunc(s.authenticator.HandleLogin)))
		mux.HandleFunc("GET /auth/callback", s.authenticator.HandleCallback)
		// POST (not GET): logout is state-changing and must be CSRF-protected.
		mux.HandleFunc("POST /auth/logout", s.authenticator.HandleLogout)
		mux.HandleFunc("GET /auth/logged-out", s.authenticator.HandleLoggedOut)
	}

	// API — JSON. Unversioned: this is the dashboard's own backing API and
	// changes with it. External consumers use /api/v1.
	mux.HandleFunc("GET /api/clusters", h.ListClusters)
	mux.HandleFunc("GET /api/clusters/{clusterID}/cronjobs", h.ListCronJobs)
	mux.HandleFunc("GET /api/clusters/{clusterID}/cronjobs/{ns}/{name}/runs", h.ListRuns)
	mux.HandleFunc("GET /api/runs/{id}/stream", h.StreamLogs)
	mux.HandleFunc("GET /api/runs/{id}/resources", h.GetResourceSamples)
	mux.HandleFunc("GET /api/runs/{id}/logs.txt", h.DownloadLogs)

	// operator wraps a mutating handler with the operator-authorization check
	// when OIDC is enabled; otherwise it is a pass-through (open access).
	operator := func(next http.Handler) http.Handler { return next }
	if s.authenticator != nil {
		operator = s.authenticator.RequireOperator
	}
	mux.Handle("POST /api/clusters/{clusterID}/cronjobs/{ns}/{name}/suspend", operator(http.HandlerFunc(h.Suspend)))
	mux.Handle("POST /api/clusters/{clusterID}/cronjobs/{ns}/{name}/resume", operator(http.HandlerFunc(h.Resume)))
	mux.Handle("POST /api/clusters/{clusterID}/cronjobs/{ns}/{name}/trigger", operator(triggerLimiter(http.HandlerFunc(h.Trigger))))

	// UI — HTML pages
	mux.HandleFunc("GET /clusters/{clusterID}/cronjobs/{ns}/{name}/runs/more", h.RunsListMore)
	mux.HandleFunc("GET /clusters/{clusterID}/cronjobs/{ns}/{name}/runs/{id}", h.RunDetail)
	mux.HandleFunc("GET /clusters/{clusterID}/cronjobs/{ns}/{name}/runs", h.RunsList)
	mux.HandleFunc("GET /clusters/{clusterID}/cronjobs/{ns}/rows", h.NamespaceRows)
	mux.HandleFunc("GET /clusters/{clusterID}/cronjobs/{ns}", h.NamespaceDetail)
	mux.HandleFunc("GET /clusters/{clusterID}", h.ClusterDetail)
	mux.HandleFunc("GET /", h.Dashboard)
}

// middlewares builds the chain for the configured mode.
//
// The browser-shaped middleware — the OIDC redirect flow and the double-submit
// CSRF cookie — is installed only in UI mode. Both defend a browser against
// itself: OIDC's guard answers an unauthenticated request with a 302 to an
// identity provider, which a machine client cannot follow, and CSRF defends
// against a cookie the collector never sets. In server mode the front door is
// the bearer token instead, and there are no POST routes for CSRF to protect.
func (s *Server) middlewares() []func(http.Handler) http.Handler {
	if !s.info.Mode.ServesUI() {
		chain := []func(http.Handler) http.Handler{Logger, Instrument, Recover, SecurityHeaders}
		if s.apiToken != "" {
			chain = append([]func(http.Handler) http.Handler{BearerAuth(s.apiToken)}, chain...)
		}
		return chain
	}

	secureCookies := s.authenticator != nil && s.authenticator.Secure()
	chain := []func(http.Handler) http.Handler{Logger, Instrument, Recover, SecurityHeaders, EnsureCSRFCookie(secureCookies), CSRFProtect}
	if s.authenticator != nil {
		chain = append([]func(http.Handler) http.Handler{s.frontDoor()}, chain...)
	}
	return chain
}

// frontDoor picks the guard for a request by what is asking.
//
// OIDC answers an unauthenticated request with a 302 to an identity provider.
// That is right for a person and useless to a program: a console reading
// /api/v1 receives a redirect to a login page it cannot fill in, which is what
// a KubeCron in ui mode did to every API client — the routes were registered
// and reachable and answered "log in first" from behind a port forward, inside
// the cluster, exactly as they did from an Ingress. The network path was never
// the problem; the middleware wraps the router and sees every request.
//
// So when an API token is configured, /api/v1 is guarded by the token and
// everything else by the session. Two doors, each for the client that can open
// it — the arrangement /metrics already has.
//
// **The split only exists when a token is actually set.** Without one the
// session guard keeps the whole surface, because the alternative is publishing
// the cluster inventory, run outcomes and captured log bodies to anyone who can
// reach the Service. That condition is the whole of the security here: it is
// structural, not a documented convention, and it is why this returns the
// session guard unchanged rather than an exemption list somebody could extend
// without noticing what it costs.
func (s *Server) frontDoor() func(http.Handler) http.Handler {
	return splitFrontDoor(s.authenticator.Middleware, s.apiToken)
}

// splitFrontDoor is frontDoor's decision, with the session guard passed in.
//
// Separated so it can be tested: building a real Authenticator performs OIDC
// discovery against a live issuer, and a test that cannot construct one ends up
// asserting the routing with no guard installed at all — which passes whatever
// the routing does.
func splitFrontDoor(session func(http.Handler) http.Handler, apiToken string) func(http.Handler) http.Handler {
	if apiToken == "" {
		return session
	}
	token := BearerAuth(apiToken)

	return func(next http.Handler) http.Handler {
		guardedByToken := token(next)
		guardedBySession := session(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isCollectorAPI(r.URL.Path) {
				guardedByToken.ServeHTTP(w, r)
				return
			}
			guardedBySession.ServeHTTP(w, r)
		})
	}
}

// isCollectorAPI reports whether a path belongs to the versioned contract.
//
// Prefix-matched on "/api/v1/" with the bare "/api/v1" allowed alongside, so
// that no path outside the contract can be mistaken for one: "/api/v1beta"
// shares the string "/api/v1" as a prefix and is not this API.
func isCollectorAPI(path string) bool {
	return path == "/api/v1" || strings.HasPrefix(path, "/api/v1/")
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
	info        CollectorInfo
}

// modeNotFound answers any non-API path in server mode. A collector serves no
// pages, and saying so is more useful than an empty 404 to whoever opened it in
// a browser expecting the dashboard.
func (h *Handler) modeNotFound(w http.ResponseWriter, r *http.Request) {
	slog.Debug("request for a UI path in server mode", "path", r.URL.Path)
	writeJSON(w, http.StatusNotFound, map[string]string{
		"error":   "this KubeCron runs in server (collector) mode and serves no UI",
		"mode":    string(h.info.Mode),
		"api":     "/api/v1/collector",
		"hint":    "set KUBECRON_MODE=ui to serve the dashboard from this instance",
		"product": "kubecron",
	})
}
