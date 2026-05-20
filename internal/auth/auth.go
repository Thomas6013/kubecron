package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// Config holds OIDC settings read from environment variables.
type Config struct {
	Enabled      bool   `env:"OIDC_ENABLED"       envDefault:"false"`
	IssuerURL    string `env:"OIDC_ISSUER_URL"`
	ClientID     string `env:"OIDC_CLIENT_ID"`
	ClientSecret string `env:"OIDC_CLIENT_SECRET"`
	RedirectURL  string `env:"OIDC_REDIRECT_URL"`
	// 64 hex chars (32 bytes). If empty, a random key is generated at startup
	// (sessions are lost on restart — acceptable for single-instance deploys).
	SessionKey string `env:"OIDC_SESSION_KEY"`
}

const (
	sessionCookie = "kubecron_session"
	stateCookie   = "kubecron_state"
	sessionTTL    = 24 * time.Hour
)

type ctxKey int

const ctxKeyEmail ctxKey = 0

// EmailFromContext returns the authenticated user's email from the request context.
// Returns "" when OIDC is disabled or the user is not logged in.
func EmailFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyEmail).(string)
	return v
}

// Authenticator handles OIDC login, callback, logout, and session validation.
type Authenticator struct {
	provider   *oidc.Provider
	verifier   *oidc.IDTokenVerifier
	oauth2Cfg  oauth2.Config
	sessionKey []byte
}

// session is stored in the signed cookie.
type session struct {
	Subject string `json:"sub"`
	Email   string `json:"email"`
	Expiry  int64  `json:"exp"`
}

// NewAuthenticator performs OIDC discovery and returns a ready Authenticator.
func NewAuthenticator(ctx context.Context, cfg Config) (*Authenticator, error) {
	provider, err := oidc.NewProvider(ctx, cfg.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("auth: OIDC discovery for %s: %w", cfg.IssuerURL, err)
	}

	var sessKey []byte
	if cfg.SessionKey != "" {
		if len(cfg.SessionKey) < 32 {
			return nil, fmt.Errorf("auth: OIDC_SESSION_KEY must be at least 32 characters")
		}
		derived := sha256.Sum256([]byte(cfg.SessionKey))
		sessKey = derived[:]
	} else {
		sessKey = make([]byte, 32)
		if _, err := rand.Read(sessKey); err != nil {
			return nil, fmt.Errorf("auth: generate session key: %w", err)
		}
	}

	return &Authenticator{
		provider: provider,
		verifier: provider.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
		oauth2Cfg: oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Endpoint:     provider.Endpoint(),
			Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
		},
		sessionKey: sessKey,
	}, nil
}

// Middleware returns an http.Handler that checks session cookies.
// Exempt paths: /healthz, /readyz, /metrics, /auth/*.
// On success, injects the authenticated user's email into the request context.
func (a *Authenticator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if p == "/healthz" || p == "/readyz" || p == "/metrics" || strings.HasPrefix(p, "/auth/") {
			next.ServeHTTP(w, r)
			return
		}
		c, err := r.Cookie(sessionCookie)
		if err != nil {
			http.Redirect(w, r, "/auth/login?redirect="+r.URL.RequestURI(), http.StatusFound)
			return
		}
		sess, ok := a.parse(c.Value)
		if !ok {
			http.Redirect(w, r, "/auth/login?redirect="+r.URL.RequestURI(), http.StatusFound)
			return
		}
		ctx := context.WithValue(r.Context(), ctxKeyEmail, sess.Email)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// HandleLogin starts the OIDC authorization code flow.
func (a *Authenticator) HandleLogin(w http.ResponseWriter, r *http.Request) {
	state := randStr(16)
	http.SetCookie(w, &http.Cookie{
		Name: stateCookie, Value: state, Path: "/auth/callback",
		MaxAge: 600, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
	redirect := r.URL.Query().Get("redirect")
	if redirect == "" {
		redirect = "/"
	}
	// Embed redirect target inside state so it survives the round-trip.
	stateVal := state + ":" + base64.URLEncoding.EncodeToString([]byte(redirect))
	http.Redirect(w, r, a.oauth2Cfg.AuthCodeURL(stateVal), http.StatusFound)
}

// HandleCallback exchanges the authorization code and sets a session cookie.
func (a *Authenticator) HandleCallback(w http.ResponseWriter, r *http.Request) {
	sc, err := r.Cookie(stateCookie)
	if err != nil {
		http.Error(w, "missing state cookie", http.StatusBadRequest)
		return
	}
	parts := strings.SplitN(r.URL.Query().Get("state"), ":", 2)
	if len(parts) != 2 || parts[0] != sc.Value {
		http.Error(w, "state mismatch", http.StatusBadRequest)
		return
	}
	redirect := "/"
	if dec, err := base64.URLEncoding.DecodeString(parts[1]); err == nil {
		redirect = string(dec)
	}
	http.SetCookie(w, &http.Cookie{Name: stateCookie, Value: "", MaxAge: -1, Path: "/auth/callback"})

	token, err := a.oauth2Cfg.Exchange(r.Context(), r.URL.Query().Get("code"))
	if err != nil {
		http.Error(w, "token exchange failed", http.StatusInternalServerError)
		return
	}
	rawID, ok := token.Extra("id_token").(string)
	if !ok {
		http.Error(w, "no id_token in response", http.StatusInternalServerError)
		return
	}
	idToken, err := a.verifier.Verify(r.Context(), rawID)
	if err != nil {
		http.Error(w, "token verification failed", http.StatusUnauthorized)
		return
	}
	var claims struct {
		Sub   string `json:"sub"`
		Email string `json:"email"`
	}
	if err := idToken.Claims(&claims); err != nil {
		http.Error(w, "claims extraction failed", http.StatusInternalServerError)
		return
	}

	signed, err := a.sign(session{Subject: claims.Sub, Email: claims.Email, Expiry: time.Now().Add(sessionTTL).Unix()})
	if err != nil {
		http.Error(w, "session signing failed", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: signed, Path: "/",
		MaxAge: int(sessionTTL.Seconds()), HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, redirect, http.StatusFound)
}

// HandleLogout clears the session cookie.
func (a *Authenticator) HandleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", MaxAge: -1, Path: "/"})
	http.Redirect(w, r, "/", http.StatusFound)
}

// sign serialises a session and appends an HMAC-SHA256 signature.
func (a *Authenticator) sign(s session) (string, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	payload := base64.URLEncoding.EncodeToString(b)
	mac := hmac.New(sha256.New, a.sessionKey)
	mac.Write([]byte(payload))
	sig := base64.URLEncoding.EncodeToString(mac.Sum(nil))
	return payload + "." + sig, nil
}

// parse verifies the HMAC signature, checks the expiry, and returns the decoded session.
func (a *Authenticator) parse(cookie string) (session, bool) {
	parts := strings.SplitN(cookie, ".", 2)
	if len(parts) != 2 {
		return session{}, false
	}
	mac := hmac.New(sha256.New, a.sessionKey)
	mac.Write([]byte(parts[0]))
	if !hmac.Equal([]byte(base64.URLEncoding.EncodeToString(mac.Sum(nil))), []byte(parts[1])) {
		return session{}, false
	}
	b, err := base64.URLEncoding.DecodeString(parts[0])
	if err != nil {
		return session{}, false
	}
	var s session
	if err := json.Unmarshal(b, &s); err != nil {
		return session{}, false
	}
	if time.Now().Unix() >= s.Expiry {
		return session{}, false
	}
	return s, true
}

func randStr(n int) string {
	b := make([]byte, n)
	rand.Read(b) //nolint:errcheck
	return base64.URLEncoding.EncodeToString(b)[:n]
}
