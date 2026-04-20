// Package server wires the HTTP surface. See doc.go for the package-level
// overview.
package server

import (
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/MikeBengtson/gemba/internal/auth"
	"github.com/MikeBengtson/gemba/internal/config"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Router is the package-level entry point. cmd/gemba passes in the embedded
// SPA filesystem so this package doesn't import the embed declaration
// directly.
type Router struct {
	cfg config.ServeConfig
	spa fs.FS
}

// NewRouter builds the chi router. spa must be a filesystem rooted at the
// built Vite output (with an index.html at the top level). Pass nil or an
// empty FS during development; the handler will return a helpful hint.
func NewRouter(cfg config.ServeConfig, spa fs.FS) http.Handler {
	r := &Router{cfg: cfg, spa: spa}

	mux := chi.NewRouter()
	mux.Use(middleware.RequestID)
	mux.Use(middleware.RealIP)
	mux.Use(middleware.Logger)
	mux.Use(middleware.Recoverer)
	mux.Use(middleware.Timeout(30_000_000_000)) // 30s

	// Auth middleware is mounted on the API/events surface only, regardless
	// of bind interface. Token auth must enforce on loopback too (gm-b3 /
	// gm-99g): any /api/* or /events request must reject before route
	// lookup when a missing/invalid bearer is presented.
	var apiAuth func(http.Handler) http.Handler
	var cookieSigner *auth.CookieSigner
	if cfg.EffectiveAuthMode() == "token" {
		if verifier := buildVerifier(cfg); verifier != nil {
			cs, err := auth.NewCookieSigner()
			if err != nil {
				slog.Error("cookie signer init failed; cookie auth disabled",
					"err", err)
			} else {
				cookieSigner = cs
			}
			apiAuth = auth.BearerOrCookieAuth(verifier, cookieSigner)
		}
	}

	// API routes. Everything under /api/* and /events/* is explicit so the
	// SPA fallback never shadows them (see gm-b2).
	mux.Route("/api", func(api chi.Router) {
		if apiAuth != nil {
			api.Use(apiAuth)
		}
		api.NotFound(apiNotFound)
		api.MethodNotAllowed(apiNotFound)

		// Login: exchange bearer (or refresh cookie) for a signed session
		// cookie. Mounted inside the auth-protected block — the middleware
		// does the credential check, the handler just stamps the cookie.
		if cookieSigner != nil {
			api.Method(http.MethodPost, "/auth/login", auth.LoginHandler(cookieSigner))
		}

		api.Get("/health", r.health)
		api.Get("/version", r.version)
		api.Get("/config", r.config)

		// Per-adaptor runtime health. Drives the SPA's degraded-state
		// banner (gm-b1). The SPA polls this every few seconds and
		// surfaces a banner when any adaptor reports healthy=false.
		api.Get("/adaptors", adaptorsHealth)

		// Stubs for the real surface. Filled in by Phase 2 work
		// (gm-e2.6 and children). Returning 501 now makes it obvious
		// which endpoints aren't wired yet.
		//
		// Surface follows Gas City's primitives:
		//   /city          — workspace identity, loaded packs, topology
		//   /rigs          — registered rigs and their pack assignments
		//   /agents        — running agent sessions (provider-agnostic)
		//   /sessions      — raw session metadata (tmux/k8s/subproc/exec)
		//   /beads         — work items across all rigs
		//   /beads/ready   — unblocked work (bd ready)
		//   /packs         — available packs, loaded packs
		//   /desired-state — parsed city.toml (desired)
		//   /drift         — desired vs actual reconciliation status
		api.Get("/city", notImplemented)
		api.Get("/rigs", notImplemented)
		api.Get("/agents", notImplemented)
		api.Get("/sessions", notImplemented)
		api.Get("/beads", notImplemented)
		api.Get("/beads/ready", notImplemented)
		api.Get("/packs", notImplemented)
		api.Get("/desired-state", notImplemented)
		api.Get("/drift", notImplemented)
	})

	mux.Route("/events", func(ev chi.Router) {
		if apiAuth != nil {
			ev.Use(apiAuth)
		}
		ev.NotFound(apiNotFound)
		ev.MethodNotAllowed(apiNotFound)
		ev.Get("/", notImplemented)
	})

	// SPA fallback is last and only matches non-API paths.
	mux.NotFound(r.serveSPA)

	return mux
}

// --- stock handlers -------------------------------------------------------

func (r *Router) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (r *Router) version(w http.ResponseWriter, _ *http.Request) {
	// Build info lives in cmd/gemba; handlers don't have access. For now a
	// placeholder; gm-e2.6 will wire a proper build-info struct.
	writeJSON(w, http.StatusOK, map[string]string{
		"component": "gemba",
		"status":    "pre-alpha scaffold",
	})
}

func (r *Router) config(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"auth_mode":      r.cfg.EffectiveAuthMode(),
		"yolo_available": r.cfg.DangerouslySkipPermissions,
		"listen":         r.cfg.Listen,
		"port":           r.cfg.Port,
		"city":           r.cfg.City,
		"town":           r.cfg.Town,
	})
}

func notImplemented(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusNotImplemented, map[string]string{
		"error": "not implemented yet; see gm-e2.6",
	})
}

// apiNotFound is the 404 handler for /api/* and /events/*. It always
// returns a JSON error envelope so frontend clients using fetch(...).then(r => r.json())
// get structured errors instead of chi's default text/plain body.
// See gm-b2 / gm-xke.
func apiNotFound(w http.ResponseWriter, req *http.Request) {
	writeJSON(w, http.StatusNotFound, map[string]string{
		"error":  "not_found",
		"path":   req.URL.Path,
		"method": req.Method,
	})
}

// buildVerifier picks the right token verifier for the running config.
// A plaintext AuthToken wins (tests and legacy flows); otherwise fall back
// to the hash file at AuthTokenHashPath. Returns nil when neither is set,
// meaning auth=token was requested but no credential is available.
func buildVerifier(cfg config.ServeConfig) auth.Verifier {
	if cfg.AuthToken != "" {
		return auth.NewPlainVerifier(cfg.AuthToken)
	}
	if cfg.AuthTokenHashPath != "" {
		return auth.NewHashVerifier(cfg.AuthTokenHashPath)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
