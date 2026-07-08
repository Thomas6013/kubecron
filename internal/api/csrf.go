package api

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

const (
	csrfCookieName = "csrf_token"
	csrfHeaderName = "X-CSRF-Token"
)

// EnsureCSRFCookie returns a middleware that sets a CSRF cookie on every
// response if one is not already present. The cookie is not HttpOnly so that
// the HTMX event handler in htmlHead can read it and attach it to POST
// requests. secure should be true when the app is served over HTTPS.
func EnsureCSRFCookie(secure bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, err := r.Cookie(csrfCookieName); err != nil {
				http.SetCookie(w, &http.Cookie{
					Name:     csrfCookieName,
					Value:    newCSRFToken(),
					Path:     "/",
					SameSite: http.SameSiteStrictMode,
					HttpOnly: false,
					Secure:   secure,
				})
			}
			next.ServeHTTP(w, r)
		})
	}
}

// CSRFProtect validates the X-CSRF-Token request header against the
// csrf_token cookie on every POST request (double-submit cookie pattern).
func CSRFProtect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			cookie, err := r.Cookie(csrfCookieName)
			if err != nil || cookie.Value == "" {
				writeError(w, http.StatusForbidden, "missing CSRF token")
				return
			}
			if r.Header.Get(csrfHeaderName) != cookie.Value {
				writeError(w, http.StatusForbidden, "invalid CSRF token")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func newCSRFToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
