// Package api wires the HTTP surface. See doc.go for the package-level
// overview.
package api

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"

	"github.com/YOUR_ORG/gemba/internal/config"
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

	// API routes. Everything under /api/* and /events/* is explicit so the
	// SPA fallback never shadows them (see gm-b2).
	mux.Route("/api", func(api chi.Router) {
		api.Get("/health", r.health)
		api.Get("/version", r.version)
		api.Get("/config", r.config)

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

	mux.Get("/events", notImplemented)

	// SPA fallback is last and only matches non-API paths.
	mux.NotFound(r.serveSPA)

	return mux
}

// --- SPA fallback ---------------------------------------------------------

func (r *Router) serveSPA(w http.ResponseWriter, req *http.Request) {
	path := strings.TrimPrefix(req.URL.Path, "/")

	// Safety: never let the SPA fallback answer for API paths, even if
	// routing changes in the future.
	if strings.HasPrefix(path, "api/") || strings.HasPrefix(path, "events") {
		http.NotFound(w, req)
		return
	}

	if r.spa == nil {
		spaUnbuilt(w)
		return
	}

	// Try the requested file first (for /assets/*.js, /favicon.ico, etc.)
	if path != "" {
		if f, err := r.spa.Open(path); err == nil {
			f.Close()
			http.ServeFileFS(w, req, r.spa, path)
			return
		}
	}

	// Fall through to index.html for client-side routing.
	if f, err := r.spa.Open("index.html"); err != nil {
		spaUnbuilt(w)
		return
	} else {
		f.Close()
	}
	http.ServeFileFS(w, req, r.spa, "index.html")
}

func spaUnbuilt(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte(`<!doctype html>
<html><body style="font-family:system-ui;max-width:40em;margin:3em auto;padding:1em">
<h1>Gemba frontend not built</h1>
<p>The <code>web/dist</code> directory is empty. Run:</p>
<pre>make build</pre>
<p>from the repo root, or run in dev mode:</p>
<pre>make dev</pre>
<p>The dev server (Vite on :5173) proxies API calls back to this binary.</p>
</body></html>`))
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

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
