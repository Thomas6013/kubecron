package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"
)

// BearerAuth returns a middleware that requires `Authorization: Bearer <token>`
// on every request except the liveness and readiness probes.
//
// This is server mode's front door, and it is a static token rather than OIDC
// on purpose. OIDC's guard answers an unauthenticated request with a 302 to an
// identity provider; a collector's client is a program, which cannot complete
// an interactive login. A shared secret is the shape that fits — and the
// deployment it is meant for makes it a modest one: a collector is a ClusterIP
// Service reached by port-forward, so possession of the token is not the only
// thing standing between a stranger and the data.
//
// /healthz and /readyz stay open because the kubelet issues them and carries no
// credential. /metrics does NOT: in server mode there is no dashboard, so the
// exporter is the one route that would otherwise publish the whole cluster's
// CronJob inventory to an unauthenticated caller (AUDIT SEC-29). A Prometheus
// scraping a token-protected collector uses its own `authorization` scrape
// config.
func BearerAuth(token string) func(http.Handler) http.Handler {
	// Compared as digests so the comparison is over fixed-length inputs: a
	// constant-time compare of different-length slices returns early and leaks
	// the length of the expected token.
	want := sha256.Sum256([]byte(token))

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
				next.ServeHTTP(w, r)
				return
			}
			presented, ok := bearerToken(r)
			if !ok {
				unauthorized(w, "missing bearer token")
				return
			}
			got := sha256.Sum256([]byte(presented))
			if subtle.ConstantTimeCompare(got[:], want[:]) != 1 {
				unauthorized(w, "invalid bearer token")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// bearerToken extracts the credential from the Authorization header. The scheme
// is matched case-insensitively, as RFC 7235 requires.
func bearerToken(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	const prefix = "bearer "
	if len(h) <= len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return "", false
	}
	return strings.TrimSpace(h[len(prefix):]), true
}

func unauthorized(w http.ResponseWriter, msg string) {
	// The challenge lets a client tell "I need a credential" from "the one I
	// sent is wrong", which is the difference between prompting an operator and
	// retrying forever.
	w.Header().Set("WWW-Authenticate", `Bearer realm="kubecron"`)
	writeError(w, http.StatusUnauthorized, msg)
}
