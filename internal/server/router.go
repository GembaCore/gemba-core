// Package server wires the HTTP surface. See doc.go for the package-level
// overview.
package server

import (
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/MikeBengtson/gemba/internal/adapter/registry"
	"github.com/MikeBengtson/gemba/internal/auth"
	"github.com/MikeBengtson/gemba/internal/config"
	"github.com/MikeBengtson/gemba/internal/transport/api"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// healthBusInterval is the ticker cadence for the registry HealthBus
// started by NewRouter. 5s matches the old poll rate — the difference
// is that we probe once per *process* instead of once per *tab*, and
// client-side rendering becomes event-driven (see adaptorsStream).
const healthBusInterval = 5 * time.Second

// Router is the package-level entry point. cmd/gemba passes in the embedded
// SPA filesystem so this package doesn't import the embed declaration
// directly. host carries the bound transport-plane adaptors so handlers
// can reach the WorkPlane / OrchestrationPlane without re-resolving them
// per request.
type Router struct {
	cfg  config.ServeConfig
	spa  fs.FS
	host *api.Host

	// nonceCache backs the X-GEMBA-Confirm idempotency gate every
	// mutating route is wrapped in. Per-process — multi-instance
	// gemba serve will need a shared store.
	nonceCache *NonceCache

	// healthBus caches adaptor status and fans out transitions over
	// /api/adaptors/stream. NewRouter creates it but does NOT start
	// the ticker — cmd/gemba serve does via StartHealthBus so tests
	// that never exercise the stream don't leak a goroutine per
	// construction. gm-root.7.
	healthBus *registry.HealthBus

	mux http.Handler
}

// NewRouter builds the chi router. spa must be a filesystem rooted at the
// built Vite output (with an index.html at the top level); pass nil or an
// empty FS during development and the handler will return a helpful hint.
// host may be nil during early bring-up — handlers that need a WorkPlane
// must check Host() before dereferencing.
func NewRouter(cfg config.ServeConfig, spa fs.FS, host *api.Host) *Router {
	r := &Router{
		cfg:        cfg,
		spa:        spa,
		host:       host,
		nonceCache: NewNonceCache(0, 0), // defaults: 1024 entries / 5min TTL
	}
	r.healthBus = registry.NewHealthBus(healthBusInterval, r.boundAdaptorStatuses)

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
		// banner (gm-b1 / gm-root.7). The SPA subscribes to the SSE
		// stream; the JSON endpoint is the snapshot fallback used by
		// `gemba doctor` and the initial-load bootstrap when the
		// EventSource errors before the first frame lands.
		api.Get("/adaptors", r.adaptorsHealth)
		api.Get("/adaptors/stream", r.adaptorsStream)

		// Capability manifests for both registered planes. The SPA reads
		// these to gate adaptor-specific controls (gm-e11.4). When no
		// WorkPlane is registered the handler emits 503 adaptor_not_configured
		// — callers treat that identically to a null manifest (hide).
		api.Get("/capabilities", r.capabilities)

		// Stubs for the real surface. Filled in by Phase 2 work
		// (gm-e2.6 and children). Returning 501 now makes it obvious
		// which endpoints aren't wired yet.
		//
		// Surface follows Gas City's primitives:
		//   /city          — workspace identity, loaded packs, topology
		//   /rigs          — registered rigs and their pack assignments
		//   /agents        — running agent sessions (provider-agnostic)
		//   /sessions      — raw session metadata (tmux/k8s/subproc/exec)
		//   /work-items         — every WorkItem the bound adaptor exposes
		//   /work-items/ready   — unblocked work (bd ready, equivalents elsewhere)
		//   /packs         — available packs, loaded packs
		//   /desired-state — parsed city.toml (desired)
		//   /drift         — desired vs actual reconciliation status
		//
		// gm-root.9: HTTP surface speaks Gemba's vocabulary, not the
		// bd adaptor's. Routes used to live under /api/beads — that
		// name leaked the bd identity into the contract.
		api.Get("/city", notImplemented)
		api.Get("/rigs", notImplemented)
		// gm-root.10: orchestrator's known agents — drives the SPA's
		// AssigneePicker / OwnerPicker. Empty list when no
		// orchestration plane is bound (the freeform editor takes over).
		api.Get("/agents", r.listAgents)
		// gm-native.15: session inventory + dispatch + end. POST and
		// DELETE are gated by X-GEMBA-Confirm so SPA double-clicks /
		// React re-mounts can't double-spawn or double-end.
		api.Get("/sessions", r.listSessions)
		api.With(requireConfirmNonce(r.nonceCache)).
			Post("/sessions", r.startSession)
		api.With(requireConfirmNonce(r.nonceCache)).
			Delete("/sessions/{id}", r.endSession)
		// gm-peg: list work items across the registered WorkPlane.
		// Empty filter today — filtering / pagination land in later
		// milestones.
		api.Get("/work-items", r.listWorkItems)
		api.Get("/work-items/ready", notImplemented)
		// gm-kn2: single-work-item fetch with full relationship graph.
		// Drives the SPA drill-in drawer. Static sibling routes above
		// (/work-items, /work-items/ready) take precedence in chi's
		// matcher.
		api.Get("/work-items/{id}", r.getWorkItem)
		// Mutations gated by the X-GEMBA-Confirm nonce so SPA
		// double-clicks / React re-mounts can't double-apply.
		api.With(requireConfirmNonce(r.nonceCache)).
			Patch("/work-items/{id}", r.patchWorkItem)
		// gm-root.11: WorkPlane sprints — drives the SPA's SprintPicker.
		// Adaptors with sprint_native=false return an empty list and the
		// freeform editor takes over.
		api.Get("/sprints", r.listSprints)
		api.Get("/packs", notImplemented)
		api.Get("/desired-state", notImplemented)
		api.Get("/drift", notImplemented)

		// gm-native.13: escalation surfacing + answer-back.
		// GET /api/escalations returns the open set from the
		// bound OrchestrationPlane. POST /api/escalations/{id}/respond
		// routes the operator's answer back into the terminal via
		// the adaptor's ResolveEscalation.
		api.Get("/escalations", r.listEscalations)
		api.With(requireConfirmNonce(r.nonceCache)).
			Post("/escalations/{id}/respond", r.respondEscalation)
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

	r.mux = mux
	return r
}

// ServeHTTP dispatches through the chi mux built in NewRouter. Router
// satisfies http.Handler so callers can hand it directly to http.Server.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mux.ServeHTTP(w, req)
}

// Host returns the api.Host this router was built with, or nil if the
// router was constructed without one. Tests and handlers use this to
// reach the registered WorkPlane / OrchestrationPlane.
func (r *Router) Host() *api.Host { return r.host }

// StartHealthBus starts the background probe ticker that feeds the
// /api/adaptors snapshot and the /api/adaptors/stream SSE endpoint.
// cmd/gemba serve calls this once after NewRouter; tests that don't
// need the stream can skip it and still get correct /api/adaptors
// responses (the handler falls through to a synchronous probe).
// gm-root.7.
func (r *Router) StartHealthBus() {
	if r.healthBus != nil {
		r.healthBus.Start()
	}
}

// Close stops the HealthBus ticker. Safe to call when StartHealthBus
// was never invoked.
func (r *Router) Close() {
	if r.healthBus != nil {
		r.healthBus.Stop()
	}
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
