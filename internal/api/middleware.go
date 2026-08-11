package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/kubecron/kubecron/internal/metrics"
)

// responseWriter wraps http.ResponseWriter to capture the HTTP status code.
type responseWriter struct {
	http.ResponseWriter
	status int
}

func newResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{ResponseWriter: w, status: http.StatusOK}
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Logger logs each request with method, path, status code and duration using slog.
func Logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := newResponseWriter(w)
		next.ServeHTTP(rw, r)
		slog.Info("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rw.status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

// Instrument records request counts and latency per matched route.
//
// The route label is the ServeMux pattern (for example
// "GET /clusters/{clusterID}"), never r.URL.Path: paths embed cluster,
// namespace, CronJob and run identifiers, so labelling by path would mint a new
// time series per object and grow without bound. Requests that match no route
// share a single "unmatched" label for the same reason — an unrouted path is
// attacker-controlled.
//
// r.Pattern is only filled in once ServeMux has dispatched, so it is read after
// the inner handler returns.
func Instrument(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := newResponseWriter(w)
		next.ServeHTTP(rw, r)

		route := r.Pattern
		if route == "" {
			route = "unmatched"
		}
		metrics.HTTPRequestsTotal.WithLabelValues(route, r.Method, strconv.Itoa(rw.status)).Inc()
		metrics.HTTPRequestDurationSeconds.WithLabelValues(route).Observe(time.Since(start).Seconds())
	})
}

// Recover catches panics, logs them, and returns a 500 JSON error response.
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic recovered", "error", rec, "path", r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{"error": "internal server error"}) //nolint:errcheck
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// SecurityHeaders sets baseline hardening headers on every response.
// CSP and HSTS are intentionally not set here: the UI relies on inline
// scripts (CSP needs a nonce refactor) and TLS is terminated by the ingress.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

// Chain applies middlewares in order (first middleware is the outermost wrapper).
func Chain(h http.Handler, middlewares ...func(http.Handler) http.Handler) http.Handler {
	// Apply in reverse so that the first middleware in the list is executed first.
	for i := len(middlewares) - 1; i >= 0; i-- {
		h = middlewares[i](h)
	}
	return h
}
