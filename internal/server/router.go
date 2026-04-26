// Package server wires the HTTP surface. See doc.go for the package-level
// overview.
package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/MikeBengtson/gemba/internal/adapter/registry"
	"github.com/MikeBengtson/gemba/internal/auth"
	"github.com/MikeBengtson/gemba/internal/config"
	corepersona "github.com/MikeBengtson/gemba/internal/core/persona"
	"github.com/MikeBengtson/gemba/internal/events"
	"github.com/MikeBengtson/gemba/internal/persona"
	"github.com/MikeBengtson/gemba/internal/transport/api"
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

	// instanceID is a per-process boot UUID stamped on every response
	// the SPA treats as startup-immutable (capabilities, adaptors).
	// Clients store the first id they observe and full-reload on
	// mismatch — that's how we detect a server restart with different
	// config without a live capabilities-changed channel. gm-6m60.
	instanceID string

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

	// eventsHub is the GembaEvent fan-out broker that drives the
	// /events SSE endpoint (gm-e4.3). NewRouter constructs the hub;
	// cmd/gemba serve attaches the bound OrchestrationPlane's Subscribe
	// stream via AttachOrchestrationStream so events flow adaptor →
	// hub → SSE clients. Optional — when nil the /events endpoint
	// returns 503.
	eventsHub *events.Hub

	// metricsHandler is the Prometheus /metrics handler. Lazy-attached
	// by cmd/gemba serve via AttachMetricsHandler so a Router without
	// a metrics binding (most tests) returns 503 from /metrics instead
	// of panicking on a nil deref. gm-e3.6.2.
	metricsHandler http.Handler

	// notifyDeduper coalesces same-id same-UpdatedAt POSTs to
	// /api/workitems/notify so a misbehaving git hook or a
	// double-fire of `bd update` doesn't double-publish events
	// (gm-e4.3.2). Bounded FIFO; oldest entry evicted on insert.
	notifyDeduper *notifyDeduper

	// personaDispatcher + skillRegistry + personaRegistry back the
	// /api/consults and /api/skills surfaces (gm-twp2). Lazy-attached
	// via AttachPersonaDispatcher so a Router built before persona
	// configuration parses (or in tests that don't exercise consults)
	// returns 503 from those endpoints instead of panicking on a nil
	// deref. The personaRegistry is required for POST /api/consults
	// (lookup persona by id) but optional for the read endpoints.
	personaDispatcher *persona.Dispatcher
	skillRegistry     *corepersona.SkillRegistry
	personaRegistry   *corepersona.Registry

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
		instanceID: newInstanceID(),
		nonceCache: NewNonceCache(0, 0), // defaults: 1024 entries / 5min TTL
	}
	r.healthBus = registry.NewHealthBus(healthBusInterval, r.boundAdaptorStatuses)
	r.eventsHub = events.NewHub(events.Config{})
	r.notifyDeduper = newNotifyDeduper(0)

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
		// gm-e7.3: live snapshot of a running session — currently a
		// transcript tail from the backing pane. Read-only; no nonce.
		api.Get("/sessions/{id}/peek", r.peekSession)
		// gm-s47n.5.4: operational-context aggregator. Single read
		// returning the join — Agent + Session + Workspace +
		// Assignment + Profile + Health — that the planner, the
		// coach UI, and operators all consume. Query: ?session_id=ID.
		api.Get("/operational-context", r.operationalContext)
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
		// gm-e12.10: POST creates a new work item with optional parent
		// (carried as a parent_child Relationship with To="").
		api.With(requireConfirmNonce(r.nonceCache)).
			Post("/work-items", r.createWorkItem)
		api.With(requireConfirmNonce(r.nonceCache)).
			Patch("/work-items/{id}", r.patchWorkItem)
		// gm-e4.3.2: out-of-process notify endpoint. Auth-gated;
		// NOT nonce-gated — the caller is server-internal plumbing
		// (the bd post-commit git hook from gm-e4.3.3, ops scripts).
		// Idempotency is per-(id, UpdatedAt) inside the handler so a
		// retry storm does not double-publish.
		api.Post("/workitems/notify", r.notifyWorkItem)
		// gm-root.11: WorkPlane sprints — drives the SPA's SprintPicker.
		// Adaptors with sprint_native=false return an empty list and the
		// freeform editor takes over.
		api.Get("/sprints", r.listSprints)
		// gm-uipx.8: workspace repository registry, used by the
		// /project/config Workspace-repos section. Empty list is the
		// right answer when the workspace has no .gemba/repositories/
		// dir — never an error.
		api.Get("/repositories", r.listRepositories)
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

		// gm-s47n.6.1: coach-mode SPA grid. One server-side
		// composition of every input the SPA needs — sessions,
		// ready beads, conflict graph (file/workspace/semantic),
		// affinity matrix, parallel-safe batches.
		api.Get("/planner/coach", r.plannerCoach)

		// gm-twp2: persona consults + the skill registry. All routes
		// here return 503 until AttachPersonaDispatcher binds the
		// dispatcher and skill registry. Write paths (POST consult,
		// apply, bridge → Receive) land in follow-up slices; this
		// commit ships the read surface so /plan + /insights/personas
		// have something to render against.
		api.Get("/skills", r.listSkills)
		api.Get("/skills/{id}", r.getSkill)
		api.Get("/skills/{id}/output_schema.json", r.getSkillOutputSchema)
		api.Get("/consults", r.listConsults)
		api.Get("/consults/{id}", r.getConsult)
		// POST /api/consults starts a new persona consult. Nonce-
		// gated like every mutating route so a SPA double-submit or
		// React re-mount can't fork two consults from one click.
		api.With(requireConfirmNonce(r.nonceCache)).
			Post("/consults", r.createConsult)
		// POST /api/consults/{id}/apply/{idx} records that the
		// operator applied the validated line at idx. Same nonce
		// gate so a double-click can't double-record. Returns the
		// recorded line so the SPA can render the SuggestedAction
		// the operator acted on.
		api.With(requireConfirmNonce(r.nonceCache)).
			Post("/consults/{id}/apply/{idx}", r.applyConsult)
	})

	mux.Route("/events", func(ev chi.Router) {
		if apiAuth != nil {
			ev.Use(apiAuth)
		}
		ev.NotFound(apiNotFound)
		ev.MethodNotAllowed(apiNotFound)
		// gm-e4.3: SSE hub stream. ?topics=... + ?planes=... +
		// scope filters. See events_stream.go for parser.
		ev.Get("/", r.eventsStream)
	})

	// Prometheus scrape surface (gm-e3.6.2). Sits behind the same auth
	// middleware as /api/* — a deployment with token auth requires its
	// scrape job to present credentials; loopback-only deployments
	// leave auth off and Prometheus scrapes anonymously, matching the
	// standard "/metrics behind a reverse proxy" convention. Mounted
	// when r.metricsHandler is non-nil; cmd/gemba serve calls
	// AttachMetricsHandler before starting the listener.
	if apiAuth != nil {
		mux.Group(func(g chi.Router) {
			g.Use(apiAuth)
			g.Method(http.MethodGet, "/metrics", metricsAdapter(&r.metricsHandler))
		})
	} else {
		mux.Method(http.MethodGet, "/metrics", metricsAdapter(&r.metricsHandler))
	}

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

// InstanceID returns the per-process boot id stamped on startup-
// immutable responses (capabilities, adaptors). The SPA stores the
// first id it sees and full-reloads if a later response shows a
// different id, which is how we detect a server restart with new
// config without a capabilities-changed channel. gm-6m60.
func (r *Router) InstanceID() string { return r.instanceID }

// newInstanceID generates a 128-bit hex-encoded random id for the
// boot stamp. Same generator the auth package uses for tokens — no
// new dependency. Falls back to a timestamp-derived sentinel on the
// vanishingly-rare crypto/rand failure so NewRouter never panics.
func newInstanceID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "boot-" + time.Now().UTC().Format("20060102T150405.000000000")
	}
	return hex.EncodeToString(b)
}

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

// AttachMetricsHandler binds an http.Handler to GET /metrics. Calling
// with nil leaves the route returning 503 — useful for tests that
// want to skip the metrics surface entirely. cmd/gemba serve calls
// this with a metrics.Collector.Handler() once at boot.
func (r *Router) AttachMetricsHandler(h http.Handler) { r.metricsHandler = h }

// metricsAdapter dereferences the Router's metrics handler at request
// time, so the route can be registered before the handler is set.
// Routes without an attached handler return 503.
func metricsAdapter(p *http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		h := *p
		if h == nil {
			http.Error(w, "metrics not configured", http.StatusServiceUnavailable)
			return
		}
		h.ServeHTTP(w, req)
	}
}

// Close stops the HealthBus ticker and tears down the events hub.
// Safe to call when StartHealthBus was never invoked.
func (r *Router) Close() {
	if r.healthBus != nil {
		r.healthBus.Stop()
	}
	if r.eventsHub != nil {
		r.eventsHub.Close()
	}
}

// EventsHub returns the GembaEvent fan-out broker. cmd/gemba serve
// uses this to AttachOrchestrationStream against the registered
// adaptor's Subscribe() output, so adaptor events fan to /events SSE
// subscribers. Returns nil only when the router was constructed
// without one (zero-value/test paths).
func (r *Router) EventsHub() *events.Hub { return r.eventsHub }

// AttachPersonaDispatcher binds the persona consult dispatcher, the
// skill registry it draws from, and the persona registry the POST
// endpoint resolves persona_id against (gm-twp2). cmd/gemba serve
// calls this once at boot after parsing personas + registering
// skills; tests attach a fixture-built dispatcher per case. Routes
// under /api/skills* and /api/consults* return 503 until at least
// the dispatcher + skill registry are bound; POST /api/consults
// additionally requires the persona registry (the read endpoints
// don't).
//
// Any argument may be nil to detach (tests that re-use a Router
// across cases set then clear).
func (r *Router) AttachPersonaDispatcher(d *persona.Dispatcher, sr *corepersona.SkillRegistry, pr *corepersona.Registry) {
	r.personaDispatcher = d
	r.skillRegistry = sr
	r.personaRegistry = pr
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
