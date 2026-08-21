package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kubecron/kubecron/internal/storage"
	"github.com/kubecron/kubecron/internal/streamer"
)

func TestParseMode(t *testing.T) {
	tests := []struct {
		in      string
		want    Mode
		wantErr bool
	}{
		{"", ModeUI, false}, // an existing deployment that sets nothing keeps its behaviour
		{"ui", ModeUI, false},
		{"standalone", ModeUI, false},
		{"server", ModeServer, false},
		{"collector", ModeServer, false},
		{"UI", "", true}, // env values are matched exactly; a typo must not silently pick a mode
		{"headless", "", true},
	}
	for _, tc := range tests {
		got, err := ParseMode(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseMode(%q): want error, got %q", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseMode(%q): unexpected error %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseMode(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// newTestServer builds a Server whose routing can be exercised without binding
// a port.
func newTestServer(t *testing.T, mode Mode, token string) (*Server, storage.Store) {
	t.Helper()
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	srv := NewServer(store, nil, streamer.NewBroadcaster(), func() bool { return true }, nil,
		CollectorInfo{Mode: mode, RetentionDays: 90, LogRetentionDays: 14, SampleIntervalSeconds: 15},
		token)
	return srv, store
}

// TestServerModeRegistersNoMutatingRoute is the load-bearing test of collector
// mode. Read-only is not a claim made in a document — it has to be that the
// route is absent, so that a caller who knows the UI's URLs cannot reach it.
func TestServerModeRegistersNoMutatingRoute(t *testing.T) {
	srv, store := newTestServer(t, ModeServer, "")
	seedCluster(t, store)
	seedCronJob(t, store)
	handler := srv.buildHandler()

	mutating := []string{
		"/api/clusters/" + testClusterID + "/cronjobs/" + testNS + "/" + testCJName + "/suspend",
		"/api/clusters/" + testClusterID + "/cronjobs/" + testNS + "/" + testCJName + "/resume",
		"/api/clusters/" + testClusterID + "/cronjobs/" + testNS + "/" + testCJName + "/trigger",
	}
	for _, path := range mutating {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, nil))
		// 404 or 405 both mean "not registered"; anything in the 2xx range
		// would mean a collector just mutated a CronJob.
		if w.Code != http.StatusNotFound && w.Code != http.StatusMethodNotAllowed {
			t.Errorf("POST %s in server mode: got %d, want the route to be absent", path, w.Code)
		}
	}
}

func TestServerModeServesNoUI(t *testing.T) {
	srv, store := newTestServer(t, ModeServer, "")
	seedCluster(t, store)
	handler := srv.buildHandler()

	for _, path := range []string{"/", "/clusters/" + testClusterID, "/static/app.css"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusNotFound {
			t.Errorf("GET %s in server mode: got %d, want 404", path, w.Code)
		}
		if ct := w.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("GET %s in server mode: content-type %q, want JSON — a collector serves no HTML", path, ct)
		}
	}
}

func TestUIModeStillServesCollectorAPI(t *testing.T) {
	// The standalone product answering the same contract is what lets an
	// operator point a console at a KubeCron they already run.
	srv, store := newTestServer(t, ModeUI, "")
	seedCluster(t, store)
	handler := srv.buildHandler()

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/collector", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/collector in ui mode: got %d, want 200", w.Code)
	}

	var body collectorResponse
	decodeJSON(t, w, &body)
	if body.ReadOnly {
		t.Error("ui mode reported read_only=true, but it registers suspend/resume/trigger")
	}
	if !body.Capabilities.Mutations {
		t.Error("ui mode reported capabilities.mutations=false, but it registers the mutating routes")
	}
}

func TestServerModeReportsItselfReadOnly(t *testing.T) {
	srv, store := newTestServer(t, ModeServer, "")
	seedCluster(t, store)

	w := httptest.NewRecorder()
	srv.buildHandler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/collector", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", w.Code)
	}

	var body collectorResponse
	decodeJSON(t, w, &body)
	if !body.ReadOnly || body.Capabilities.Mutations {
		t.Error("server mode must report read_only=true and capabilities.mutations=false")
	}
	if body.Mode != string(ModeServer) {
		t.Errorf("mode = %q, want %q", body.Mode, ModeServer)
	}
	if body.APIVersion != APIVersion || len(body.APIVersions) == 0 {
		t.Errorf("discovery must name the contract version: %+v", body.APIVersions)
	}
	// Retention is what tells a consumer whether an empty window is "nothing
	// happened" or "we no longer hold it".
	if body.Retention.RunDays != 90 || body.Retention.LogDays != 14 {
		t.Errorf("retention = %+v, want the configured 90/14", body.Retention)
	}
	if body.SampleIntervalSeconds != 15 {
		t.Errorf("sample_interval_seconds = %d, want 15", body.SampleIntervalSeconds)
	}
}

// ── Bearer token ──────────────────────────────────────────────────────────────

func TestBearerAuthGuardsCollectorAPI(t *testing.T) {
	const token = "s3cr3t-token"
	srv, store := newTestServer(t, ModeServer, token)
	seedCluster(t, store)
	handler := srv.buildHandler()

	tests := []struct {
		name     string
		header   string
		wantCode int
	}{
		{"no header", "", http.StatusUnauthorized},
		{"wrong token", "Bearer nope", http.StatusUnauthorized},
		{"not bearer", "Basic " + token, http.StatusUnauthorized},
		{"empty bearer", "Bearer ", http.StatusUnauthorized},
		{"correct", "Bearer " + token, http.StatusOK},
		{"scheme is case-insensitive", "bearer " + token, http.StatusOK},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/api/v1/collector", nil)
			if tc.header != "" {
				r.Header.Set("Authorization", tc.header)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != tc.wantCode {
				t.Errorf("got %d, want %d", w.Code, tc.wantCode)
			}
			if tc.wantCode == http.StatusUnauthorized && w.Header().Get("WWW-Authenticate") == "" {
				t.Error("a 401 must carry a WWW-Authenticate challenge")
			}
		})
	}
}

// TestBearerAuthLeavesProbesOpen: the kubelet issues the probes and carries no
// credential, so a token that closed them would fail every pod it protects.
func TestBearerAuthLeavesProbesOpen(t *testing.T) {
	srv, _ := newTestServer(t, ModeServer, "a-token")
	handler := srv.buildHandler()

	for _, path := range []string{"/healthz", "/readyz"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusOK {
			t.Errorf("GET %s without a token: got %d, want 200", path, w.Code)
		}
	}
}

// TestBearerAuthGuardsMetrics: /metrics discloses the whole CronJob inventory
// (AUDIT SEC-29), and in server mode there is no dashboard to make it the least
// of the exposures.
func TestBearerAuthGuardsMetrics(t *testing.T) {
	srv, _ := newTestServer(t, ModeServer, "a-token")
	w := httptest.NewRecorder()
	srv.buildHandler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if w.Code != http.StatusUnauthorized {
		t.Errorf("GET /metrics without a token: got %d, want 401", w.Code)
	}
}

// TestServerModeInstallsNoCSRFCookie: CSRF is a browser defence, and a cookie
// the collector sets on a machine client is noise its consumer has to carry.
func TestServerModeInstallsNoCSRFCookie(t *testing.T) {
	srv, store := newTestServer(t, ModeServer, "")
	seedCluster(t, store)

	w := httptest.NewRecorder()
	srv.buildHandler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/collector", nil))
	if c := w.Result().Cookies(); len(c) != 0 {
		t.Errorf("server mode set %d cookie(s), want none", len(c))
	}
}
