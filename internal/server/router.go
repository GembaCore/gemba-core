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
	"github.com/go-chi/cors"

	"github.com/GembaCore/gemba-core/internal/adapter/registry"
	"github.com/GembaCore/gemba-core/internal/auth"
	"github.com/GembaCore/gemba-core/internal/config"
	corepersona "github.com/GembaCore/gemba-core/internal/core/persona"
	"github.com/GembaCore/gemba-core/internal/core/phase"
	"github.com/GembaCore/gemba-core/internal/events"
	"github.com/GembaCore/gemba-core/internal/persona"
	tracemw "github.com/GembaCore/gemba-core/internal/server/middleware"
	"github.com/GembaCore/gemba-core/internal/skills/walk_summary"
	"github.com/GembaCore/gemba-core/internal/tenant"
	"github.com/GembaCore/gemba-core/internal/transport/api"
	"github.com/GembaCore/gemba-core/internal/walk"
	"github.com/GembaCore/gemba-core/internal/workflow"
)

// healthBusInterval is zero by design: adaptor health is probed on
// demand, not on a background ticker. The shared HealthBus still caches
// snapshots and publishes explicitly refreshed transitions, but Start()
// is a no-op when interval <= 0.
const healthBusInterval = 0 * time.Second

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

	// healthBus caches adaptor status and fans out explicit refreshes
	// over /api/adaptors/stream. NewRouter creates it, but there is no
	// background probe ticker; callers refresh it only after an
	// operation failure or an operator health action. gm-root.7.
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

	// metricsHTTPClient is the http.Client the /api/v1/metrics/series
	// proxy uses to call upstream Prometheus (gm-e9m0). Nil falls
	// back to a default 15s-timeout client at request time. Tests
	// inject a stub via AttachMetricsHTTPClient so they don't need
	// a live Prometheus instance.
	metricsHTTPClient *http.Client

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

	// walkStore + walkSources back the /api/v1/walks/* surface
	// (gm-wgv). walkStore is required for every walk handler; a nil
	// store returns 503 from each route. walkSources is zero-value-
	// safe — nil listers contribute zero items to BuildAgenda, which
	// is the intended state until gm-3jv ships the cross-worker
	// escalation aggregator. Bind via Router.AttachWalk.
	walkStore   walk.Store
	walkSources walk.Sources

	// walkSummary is the Documentarian post-end artifact skill
	// (gm-77u). Optional — when nil, /api/v1/walks/{id}/end records
	// the transition and emits the SSE event but does not write the
	// markdown summary. Bind via Router.AttachWalkSummary.
	walkSummary *walk_summary.Skill

	// phaseStore backs the /api/v1/phase + /api/v1/phase/advance
	// surface (gm-jt9). Optional — when nil both routes return 503.
	// Bind via Router.AttachPhase. The same store is consumed by the
	// Purview-as-gate path (gm-3on) to decide whether a Manager
	// mutation is allowed in the project's current phase.
	phaseStore phase.Store

	// bootstrapStore backs the /api/bootstrap/* surface (gm-ad1u).
	// Required for every bootstrap handler; a nil store returns
	// 503 from each route. Bind via Router.AttachBootstrap.
	bootstrapStore BootstrapStore

	// newProjectStore, newProjectTurner, and newProjectRatifier back
	// the /api/v1/newproject/* surface (gm-root.17.3). All three are
	// lazy-attached via AttachNewProject. A nil store returns 503 from
	// every newproject handler; a nil turner falls back to the stub;
	// a nil ratifier returns 503 from /ratify only.
	newProjectStore    NewProjectStore
	newProjectTurner   SkillTurner
	newProjectRatifier NewProjectRatifier

	// interactionStore backs /api/v1/interactions:* (gm-ddpy). It is
	// intentionally process-local for the first slice: interactions
	// are operator cockpit state until ratified into WorkPlane records.
	interactionStore *interactionStore

	// attachPinger / attachGitRunner / attachNow back the
	// /api/v1/projects/attach handler (gm-xwa8). All three are zero-
	// value safe — nil falls back to the production defaults
	// (defaultBeadsPinger, execRunner, time.Now). Tests inject stubs
	// via AttachProjects so they don't need a real Dolt server or git
	// binary.
	attachPinger    BeadsPinger
	attachGitRunner CommandRunner
	attachNow       func() time.Time

	// adoptableLister backs GET /api/v1/projects/adoptable (gm-gmyl).
	// Nil falls back to defaultAdoptableLister which opens a mysql
	// connection against the configured Dolt host. Tests inject a
	// stub via AttachProjects(AttachConfig{AdoptableLister: ...}).
	adoptableLister AdoptableLister

	// cloneRunner backs POST /api/v1/projects/clone (gm-e12.21.2).
	// Nil falls back to execRunner. Tests inject a stub so they
	// don't shell out to git.
	cloneRunner CommandRunner

	// workflowClient backs the /api/workflows/* surface (gm-e12.22.2).
	// Nil → every workflow handler returns 503 adaptor_not_configured;
	// cmd/gemba serve attaches a real client via AttachWorkflowClient.
	workflowClient *workflow.Client

	// pools backs GET /api/pools (gm-s47n.12, spec §10.1). Holds the
	// resolved pool config so the endpoint can surface declared vs
	// effective sizes (the MaxParallel clamp is observable
	// post-startup via this field). Nil / empty → endpoint returns
	// {"pools": []}.
	pools *poolsRegistry

	// poolConfig backs GET/PUT /api/pool-config (gm-s47n.16). Holds
	// the configured pool.toml path + the host-level MaxParallel +
	// ReservedForManual default the editor surfaces alongside the
	// parsed body. Nil → GET returns an empty envelope, PUT returns
	// 400 path_not_configured.
	poolConfig *poolConfigRegistry

	// personasDir backs GET /api/personas (gm-s47n.16). Filesystem
	// path to the workspace's `.gemba/personas/` directory; when
	// empty the endpoint falls back to "<cwd>/.gemba/personas".
	personasDir string

	// doltReadyState carries the embedded-dolt supervisor handle that
	// drives /api/readyz (gm-o9t8.1.2.3). Bind via AttachDoltSupervisor;
	// nil = external-dolt or noop mode (readyz omits the dolt check).
	doltReadyState

	// workspacesRoot is the on-disk parent directory holding cloned
	// per-workspace repos at <root>/<wsid>/repo/ (gm-o9t8.1.4.3 +
	// gm-o9t8.1.6.2 diff streaming). Bind via AttachWorkspacesRoot;
	// empty → /api/v1/workspaces/{wsid}/diff returns 503.
	workspacesRoot string

	// tenantStore backs GET /api/v1/tenants/{tid} (gm-o9t8.3.9). When
	// nil the handler returns 503 adaptor_not_configured; cmd/gemba
	// serve attaches a real store (SQLStore or MemStore) via
	// AttachTenantStore after the Dolt pool is up.
	tenantStore tenant.Store

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
	r.interactionStore = newInteractionStore()
	r.healthBus = registry.NewHealthBus(healthBusInterval, r.boundAdaptorStatuses)
	r.eventsHub = events.NewHub(events.Config{})
	r.notifyDeduper = newNotifyDeduper(0)

	mux := chi.NewRouter()
	mux.Use(middleware.RequestID)
	mux.Use(middleware.RealIP)
	// gm-e3.6.1: extract W3C trace-context off the request and attach a
	// server-kind span to the request's context. Sits before Logger so
	// log lines emitted by handlers can pick the trace id off the
	// context if/when the slog handler is wired to do so. With no
	// TracerProvider registered (default) this is effectively a no-op
	// — spans are created but the no-op exporter drops them.
	mux.Use(tracemw.Trace())
	mux.Use(middleware.Logger)
	mux.Use(middleware.Recoverer)
	mux.Use(middleware.Timeout(30_000_000_000)) // 30s

	// gm-e4.1: CORS off by default. When ServeConfig.CORSAllowedOrigins
	// is non-empty the middleware mounts with those origins + the
	// canonical Gemba HTTP surface (GET/POST/PATCH/DELETE,
	// X-GEMBA-Confirm). Loopback / same-origin SPA paths don't need
	// CORS; the operator opts in for cross-origin SPAs or local dev
	// proxies.
	if origins := cfg.CORSAllowedOrigins; len(origins) > 0 {
		mux.Use(cors.Handler(cors.Options{
			AllowedOrigins:   origins,
			AllowedMethods:   []string{"GET", "POST", "PATCH", "DELETE", "OPTIONS"},
			AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-GEMBA-Confirm", "traceparent"},
			ExposedHeaders:   []string{"X-Gemba-Instance"},
			AllowCredentials: true,
			MaxAge:           300,
		}))
	}

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
	bootstrapStore := auth.NewBootstrapStore(cfg.AuthBootstrapToken, cfg.AuthBootstrapExpiresAt)

	// API routes. Everything under /api/* and /events/* is explicit so the
	// SPA fallback never shadows them (see gm-b2).
	if cookieSigner != nil && bootstrapStore != nil {
		mux.Method(http.MethodPost, "/api/auth/bootstrap",
			auth.BootstrapLoginHandler(cookieSigner, bootstrapStore))
	}
	mux.Route("/api", func(api chi.Router) {
		if apiAuth != nil {
			api.Use(apiAuth)
		}
		// gm-o9t8.3.9: attach the bearer-bound tenant to every
		// authenticated request's context. The OAuth resolver lands
		// in gm-o9t8.3.5; until then SingleUserResolver answers
		// DefaultTenant. WithTenant runs AFTER apiAuth so it can
		// assume credentials are valid.
		api.Use(tracemw.WithTenant(tracemw.SingleUserResolver()))
		api.NotFound(apiNotFound)
		api.MethodNotAllowed(apiNotFound)

		// Login: exchange bearer (or refresh cookie) for a signed session
		// cookie. Mounted inside the auth-protected block — the middleware
		// does the credential check, the handler just stamps the cookie.
		if cookieSigner != nil {
			api.Method(http.MethodPost, "/auth/login", auth.LoginHandler(cookieSigner))
		}

		api.Get("/health", r.health)
		// gm-o9t8.1.2.3: readiness probe that reflects embedded
		// dependency state. /health stays a pure liveness probe;
		// /readyz returns 503 when the embedded dolt sql-server
		// is unreachable so orchestrators can drain traffic.
		api.Get("/readyz", r.readyz)
		api.Get("/version", r.version)
		// gm-o9t8.1.1.2: identity probe for the CLI `gemba login` /
		// `gemba whoami` flow. Auth-gated; returns the subject the
		// bearer/cookie maps to (v1: a static "operator" — see
		// whoami.go for the multi-identity follow-up).
		api.Get("/whoami", r.whoami)
		api.Get("/config", r.config)
		api.Get("/beads-history", r.beadsHistory)
		api.Get("/beads/health", r.beadsHealth)
		api.With(requireConfirmNonce(r.nonceCache)).
			Post("/beads/health/actions", r.beadsHealthAction)

		// gm-e4.2: self-describing OpenAPI 3.1 schema. The SPA's
		// generated client (web/src/api/gen/) is built against this
		// document at codegen time; the runtime route lets `gemba
		// doctor`, integration tests, and external tooling pull the
		// same spec without depending on the working directory.
		api.Get("/openapi.json", r.openapiSpecHandler)

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
		// gm-e12.8: orchestrator's known AgentGroups. Drives the SPA's
		// AgentGroupBoard, which dispatches on GroupMode (static | pool
		// | graph). Empty list when no orchestration plane is bound.
		api.Get("/agent-groups", r.listAgentGroups)
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
		api.Post("/interactions:ensure", r.ensureInteraction)
		api.Post("/interactions:turn", r.turnInteraction)
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
			Post("/work-items/{id}/cascade-dispatch", r.cascadeDispatchWorkItem)
		api.With(requireConfirmNonce(r.nonceCache)).
			Patch("/work-items/{id}", r.patchWorkItem)
		api.With(requireConfirmNonce(r.nonceCache)).
			Delete("/work-items/{id}", r.deleteWorkItem)
		// gm-o9t8.1.3.2: 501 stub for `gemba bead note`. Wire surface
		// pinned here; persistence semantics land in a follow-up bead.
		api.With(requireConfirmNonce(r.nonceCache)).
			Post("/work-items/{id}/notes", r.noteWorkItemStub)
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

		// gm-e12.22.2: /api/workflows/* — Library + Active runs surface
		// the SPA's Workflow tab consumes. Thin proxy over bd formula
		// + bd mol; the workflow client is lazy-attached via
		// AttachWorkflowClient so a Router built without it returns
		// 503 from every workflow handler instead of panicking.
		api.Get("/workflows/formulas", r.listWorkflowFormulas)
		api.Get("/workflows/formulas/{name}", r.getWorkflowFormula)
		api.Get("/workflows/runs", r.listWorkflowRuns)
		api.Get("/workflows/runs/{id}", r.getWorkflowRun)
		api.With(requireConfirmNonce(r.nonceCache)).
			Post("/workflows/runs", r.startWorkflowRun)
		api.With(requireConfirmNonce(r.nonceCache)).
			Post("/workflows/runs/{id}/burn", r.burnWorkflowRun)

		// Spec Kit artifacts are a planning source: read/edit the
		// .specify/specs (or specs) tree and, on explicit operator
		// action, sync feature → milestone/epic/story/task beads.
		api.Get("/spec-kit/workspace", r.getSpecKitWorkspace)
		api.With(requireConfirmNonce(r.nonceCache)).
			Post("/spec-kit/scaffold", r.initializeSpecKitScaffold)
		api.Get("/spec-kit/files", r.getSpecKitFile)
		api.With(requireConfirmNonce(r.nonceCache)).
			Put("/spec-kit/files", r.putSpecKitFile)
		api.Get("/spec-kit/features", r.listSpecKitFeatures)
		api.With(requireConfirmNonce(r.nonceCache)).
			Post("/spec-kit/features", r.createSpecKitFeature)
		api.Get("/spec-kit/features/{id}", r.getSpecKitFeature)
		api.Get("/spec-kit/features/{id}/draft", r.draftSpecKitFeatureSync)
		api.Get("/spec-kit/features/{id}/sync-plan", r.planSpecKitFeatureSync)
		api.With(requireConfirmNonce(r.nonceCache)).
			Post("/spec-kit/features/{id}/sync-to-beads", r.syncSpecKitFeature)
		api.With(requireConfirmNonce(r.nonceCache)).
			Post("/bootstrap/drafts/apply", r.applyBootstrapDraft)
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

		// gm-s47n.12: pool state read endpoint (spec §10.1). Returns
		// one entry per (scope, persona) pool with declared vs
		// effective sizes (so the MaxParallel clamp is observable
		// post-startup) and a member roster projected from the
		// OrchestrationPlane's live session inventory. Read-only;
		// no nonce.
		api.Get("/pools", r.listPools)

		// gm-s47n.16: pool config editor surface (spec §7).
		// /api/pool-config GET reads pool.toml; PUT writes it
		// (nonce-gated). /api/personas enumerates
		// .gemba/personas/*.toml for the editor's persona dropdown.
		// /api/orchestration/* is the adaptor-aware bridge for the
		// gt-flow editor (rigs, polecats, scheduler config).
		api.Get("/pool-config", r.getPoolConfig)
		api.With(requireConfirmNonce(r.nonceCache)).
			Put("/pool-config", r.putPoolConfig)
		api.Get("/personas", r.listPersonaFiles)
		api.Get("/orchestration/state", r.orchestrationState)
		api.With(requireConfirmNonce(r.nonceCache)).
			Post("/orchestration/scopes", r.createScope)
		api.With(requireConfirmNonce(r.nonceCache)).
			Post("/orchestration/polecats", r.createPolecat)
		api.Get("/orchestration/scheduler-config", r.getSchedulerConfig)
		api.With(requireConfirmNonce(r.nonceCache)).
			Put("/orchestration/scheduler-config", r.putSchedulerConfig)

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

		// gm-8qr: persona roster read surface. Surfaces every
		// persona's identity + the three gm-9rv axes (Personality /
		// Perspective / Purview). 503 until AttachPersonaDispatcher
		// binds a registry. Read-only and additive — gm-3on / gm-jt9
		// will extend the response with phase + active-purview state
		// without breaking the existing shape.
		api.Get("/v1/personas", r.listPersonasV1)

		// gm-jt9: project Phase primitive. GET returns the workspace's
		// current Phase + history; POST /advance is nonce-gated. Both
		// return 503 until AttachPhase binds a phase.Store. The
		// Purview-as-gate path (gm-3on) reads the same store to decide
		// whether a Manager mutation is allowed.
		api.Get("/v1/phase", r.getPhase)
		api.With(requireConfirmNonce(r.nonceCache)).
			Post("/v1/phase/advance", r.advancePhase)

		// gm-wgv: Gemba walk HTTP surface. Mounted under /api/v1/
		// per the design's §API table; rest of /api stays unversioned
		// for now. The "verb" routes (walks:start, walks:recent) sit
		// alongside the "resource" routes (walks/{id}/...) — colon
		// is a literal path-segment character in chi, not a param
		// marker. Walk routes do NOT carry a nonce gate — the store
		// operations are in-memory + low-blast-radius today, and the
		// /events stream already dedupes by GembaEvent.ID. Add
		// requireConfirmNonce when /turn or /decide grows persona-
		// applier side effects (gm-4p6).
		api.Post("/v1/walks:start", r.startWalk)
		api.Get("/v1/walks:recent", r.listRecentWalks)
		api.Get("/v1/walks/{id}", r.getWalk)
		api.Post("/v1/walks/{id}/agenda", r.addWalkAgenda)
		api.Patch("/v1/walks/{id}/agenda/{item}", r.patchWalkAgenda)
		// gm-bms: per-agenda-item perspective contributions. Read-only
		// — surfaces inline notes from every persona whose
		// Perspective.VolunteerMode == "always". v1 returns canned
		// templated text; gm-4p6 wires real persona-dispatcher output
		// without changing the wire shape.
		api.Get("/v1/walks/{id}/agenda/{item}/perspectives", r.listWalkPerspectives)
		api.Post("/v1/walks/{id}/turn", r.addWalkTurn)
		api.Post("/v1/walks/{id}/decide/{item}", r.decideWalkItem)
		api.Post("/v1/walks/{id}/consult", r.consultWalk)
		api.Post("/v1/walks/{id}/pause", r.pauseWalk)
		api.Post("/v1/walks/{id}/resume", r.resumeWalk)
		api.Post("/v1/walks/{id}/end", r.endWalk)

		// gm-ad1u: /bootstrap wizard (ui-spec §5.15) backend. Mirrors
		// web/src/api/bootstrap.ts wire shapes verbatim. start /
		// progress / plan are read-style; commit is nonce-gated so
		// SPA double-clicks can't double-write project config.
		api.Post("/bootstrap/start", r.bootstrapStart)
		api.Get("/bootstrap/progress", r.bootstrapProgress)
		api.Get("/bootstrap/plan", r.bootstrapPlan)
		api.With(requireConfirmNonce(r.nonceCache)).
			Post("/bootstrap/commit", r.bootstrapCommit)

		// gm-e4.1: canonical /api/v1/* alias surface. The bead's spec
		// names eight resource roots — workitems, agents, groups,
		// workspaces, sessions, escalations, sprints, capabilities —
		// mounted under a stable /api/v1/ prefix. The legacy unversioned
		// routes above stay registered for SPA backward-compat; new
		// callers MUST hit /api/v1/*. Mutating routes carry the same
		// requireConfirmNonce gate as their unversioned twins.
		api.Get("/v1/capabilities", r.capabilities)
		api.Get("/v1/beads-history", r.beadsHistory)
		api.Get("/v1/agents", r.listAgents)
		api.Get("/v1/groups", r.listAgentGroups)
		api.Get("/v1/sessions", r.listSessions)
		api.With(requireConfirmNonce(r.nonceCache)).
			Post("/v1/sessions", r.startSession)
		api.With(requireConfirmNonce(r.nonceCache)).
			Delete("/v1/sessions/{id}", r.endSession)
		api.Get("/v1/sessions/{id}/peek", r.peekSession)
		api.Post("/v1/interactions:ensure", r.ensureInteraction)
		api.Post("/v1/interactions:turn", r.turnInteraction)
		api.Get("/v1/escalations", r.listEscalations)
		api.With(requireConfirmNonce(r.nonceCache)).
			Post("/v1/escalations/{id}/respond", r.respondEscalation)
		// gm-root.27.22 — test-mode synthetic-escalation injector,
		// gated on GEMBA_ENABLE_TEST_ESCALATIONS=1. Default-off so
		// production servers never expose it. Used by the headless
		// acceptance harness's triage step (gm-root.27.11).
		if testEscalationsEnabled() {
			api.With(requireConfirmNonce(r.nonceCache)).
				Post("/v1/test/escalations", r.postTestEscalation)
		}
		api.Get("/v1/sprints", r.listSprints)
		api.Get("/v1/workitems", r.listWorkItems)
		api.Get("/v1/workitems/{id}", r.getWorkItem)
		api.With(requireConfirmNonce(r.nonceCache)).
			Post("/v1/workitems", r.createWorkItem)
		api.With(requireConfirmNonce(r.nonceCache)).
			Patch("/v1/workitems/{id}", r.patchWorkItem)
		api.Get("/v1/workspaces", r.listWorkspaces)
		api.Get("/v1/pools", r.listPools)

		// gm-root.18: project picker — enumerate projects on disk +
		// switch the active workspace. Both endpoints are read-light and
		// mutation-light (no content is written to disk); the switch
		// endpoint records the selection in process memory only. The
		// nonce gate is intentionally omitted on /switch: the operation
		// is idempotent and low-blast-radius (in-process flag only;
		// no disk writes). Add the gate if future iterations persist
		// the selection durably.
		api.Get("/v1/projects", r.listProjects)
		api.Post("/v1/projects/switch", r.switchProject)

		// gm-e12.21.1: directory-listing endpoint backing the SPA's
		// <FilePicker>. Read-only metadata; allow-list bounded to
		// $HOME and the configured projects dir.
		api.Get("/v1/fs/list", r.listFs)
		api.Get("/v1/fs/info", r.infoFs)
		// gm-gmyl: enumerate beads-shaped DBs visible on the configured
		// Dolt server that aren't already attached to a filesystem
		// project. The picker renders these alongside configured
		// projects so the operator can adopt one in a single click.
		api.Get("/v1/projects/adoptable", r.listAdoptableProjects)

		// gm-xwa8: configure-and-attach modal target. Adopts an existing
		// beads DB into a project on this machine — writes
		// .gemba/workspace.toml, optionally `git init`s a fresh repo,
		// and pings the DB. NO milestones/epics/beads are seeded
		// (adoption is non-destructive). Nonce-gated so the SPA's
		// double-submit guard covers this mutation like every other
		// write surface.
		api.With(requireConfirmNonce(r.nonceCache)).
			Post("/v1/projects/attach", r.attachProject)

		// gm-e12.21.2: clone-from-URL handler for the Create-project
		// modal. Same nonce gate as attach — disk-write mutation that
		// must not fire twice on a double-submit. URL validation is
		// inside the handler so a bogus body still returns 400 rather
		// than panicking the cmd dispatcher.
		api.With(requireConfirmNonce(r.nonceCache)).
			Post("/v1/projects/clone", r.cloneProject)

		// gm-root.17.14: bind a partially-bootstrapped project to a
		// git repo. Two modes — "create" (git init in place) and
		// "navigate" (copy the beads DB into an existing repo).
		// Nonce-gated for the same reason as /attach: the handler
		// writes to disk and the operator must not accidentally
		// trigger it twice.
		api.With(requireConfirmNonce(r.nonceCache)).
			Post("/v1/projects/bind", r.bindProject)

		// gm-ddpy: deterministic onboarding setup runs before the
		// Onboarder LLM is launched. It prepares or adopts the worktree,
		// initializes Gemba/Beads metadata, installs agent guidance, and
		// verifies source-analysis/MCP availability where possible.
		api.With(requireConfirmNonce(r.nonceCache)).
			Post("/v1/onboarding/setup", r.onboardingSetup)

		// gm-root.17.3: conversational new-project flow. All three
		// endpoints are post-only; /start and /turn are fire-and-forget
		// (low blast radius); /ratify is nonce-gated because it writes to
		// disk and the operator must not accidentally trigger it twice.
		api.Post("/v1/newproject/start", r.newProjectStart)
		api.Post("/v1/newproject/{id}/turn", r.newProjectTurn)
		api.With(requireConfirmNonce(r.nonceCache)).
			Post("/v1/newproject/{id}/ratify", r.newProjectRatify)

		// gm-root.17.13: lightweight project-creation path. Bypasses
		// the Onboarder + skill: the SPA's /new form posts a name +
		// description and the server runs the same atomic ratify
		// against an empty Milestones[] tree. Nonce-gated for the
		// same reason as /ratify (disk write + initial commit).
		api.With(requireConfirmNonce(r.nonceCache)).
			Post("/v1/newproject/create", r.newProjectCreate)

		// gm-root.17.13: onboarder availability probe. The board's
		// empty-state CTA reads this to decide whether to render
		// "Plan with the Onboarder →". Always 200; the body's
		// `available` flag is the operative signal.
		api.Get("/v1/onboarder/probe", r.onboarderProbe)

		// gm-e9m0: Insights time-series query proxy. Translates
		// /api/v1/metrics/series requests into upstream Prometheus
		// /api/v1/query_range calls (D4 — gm-sf51, "Path A"). When
		// PromURL is empty the handler returns 503
		// KindAdaptorDegraded so the SPA's Insights panel can render
		// an "awaiting Prometheus" stub. Read-only — no nonce gate.
		api.Get("/v1/metrics/series", r.metricsSeries)

		// gm-o9t8.14 — Govern + Convenience verb surface stubs. Wire
		// shape pinned; all handlers return 501 with the
		// not_implemented envelope until the ASDD epic (gm-v0sp)
		// lands. Auth applies via the shared apiAuth middleware on
		// the /api block; write endpoints additionally require the
		// X-GEMBA-Confirm nonce.
		//
		// gm-o9t8.3.9: every /api/v1/workspaces/{wsid}/... route
		// runs through RequireWSIDTenantMatch so a bearer scoped to
		// tenant A cannot poke at tenant B's wsids. Bare-wsid M1
		// routes still work because both sides default to
		// DefaultTenant. WithTenant on the /api block has already
		// stamped the bearer's tenant on the context.
		api.Group(func(ws chi.Router) {
			ws.Use(tracemw.RequireWSIDTenantMatch)

			// Spec family (govern):
			ws.Get("/v1/workspaces/{wsid}/specs", r.listSpecsStub)
			ws.With(requireConfirmNonce(r.nonceCache)).
				Patch("/v1/workspaces/{wsid}/specs/{slug}", r.patchSpecStub)
			ws.With(requireConfirmNonce(r.nonceCache)).
				Post("/v1/workspaces/{wsid}/specs/{slug}/reconcile", r.reconcileSpecStub)
			ws.Get("/v1/workspaces/{wsid}/specs/{slug}/watch", r.watchSpecStub)
			ws.With(requireConfirmNonce(r.nonceCache)).
				Post("/v1/workspaces/{wsid}/specs/{slug}/snapshot", r.snapshotSpecStub)
			ws.With(requireConfirmNonce(r.nonceCache)).
				Post("/v1/workspaces/{wsid}/specs/{slug}/adopt", r.adoptSpecStub)

			// Decision family (govern):
			ws.Get("/v1/workspaces/{wsid}/decisions", r.listDecisionsStub)
			ws.With(requireConfirmNonce(r.nonceCache)).
				Post("/v1/workspaces/{wsid}/decisions", r.createDecisionStub)
			ws.Get("/v1/workspaces/{wsid}/decisions/refs", r.decisionRefsStub)
			ws.Get("/v1/workspaces/{wsid}/decisions/{id}", r.getDecisionStub)
			ws.With(requireConfirmNonce(r.nonceCache)).
				Patch("/v1/workspaces/{wsid}/decisions/{id}/lock", r.lockDecisionStub)

			// Constitution / spec-lint reads (govern):
			ws.Get("/v1/workspaces/{wsid}/labels", r.listLabelsStub)

			// Convenience verb — gemba no-args (gm-o9t8.1.4.4:
			// real handler, replaced the 501 stub):
			ws.Get("/v1/workspaces/{wsid}/status", r.workspaceStatus)

			// gm-o9t8.1.6.2: stream `git diff` from the server-side
			// workspace repo. GET, so no nonce gate; auth applies
			// via the shared apiAuth middleware on the /api block.
			ws.Get("/v1/workspaces/{wsid}/diff", r.workspaceDiffHandler)
		})

		// gm-o9t8.3.9: tenant metadata. Auth-gated by the shared
		// /api middleware; the handler itself enforces "requested
		// tid == bearer tid" so the route is owner-only without a
		// dedicated admin role.
		api.Get("/v1/tenants/{tid}", r.getTenant)
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

// StartHealthBus preserves the old serve wiring hook. With the current
// zero interval it is intentionally a no-op: adaptor health is checked
// by explicit refresh paths, not by a periodic background probe.
// gm-root.7.
func (r *Router) StartHealthBus() {
	if r.healthBus != nil {
		r.healthBus.Start()
	}
}

// HealthBus returns the registry.HealthBus this router caches
// adaptor status against. cmd/gemba serve uses this to wire the
// gemba walk's BeadsDegradedLister against the same probe ticker
// the SPA banner reads from (gm-vch2). Returns nil only when the
// router was built without one — test paths that don't exercise
// adaptor health.
func (r *Router) HealthBus() *registry.HealthBus { return r.healthBus }

// AttachWorkflowClient binds the workflow.Client the /api/workflows/*
// surface dispatches to (gm-e12.22.2). Until called, every workflow
// handler returns 503 adaptor_not_configured. cmd/gemba serve calls
// this once at boot with a client backed by the bd CLI on PATH; tests
// inject a stub Runner via NewClientWithRunner.
func (r *Router) AttachWorkflowClient(c *workflow.Client) { r.workflowClient = c }

// AttachMetricsHandler binds an http.Handler to GET /metrics. Calling
// with nil leaves the route returning 503 — useful for tests that
// want to skip the metrics surface entirely. cmd/gemba serve calls
// this with a metrics.Collector.Handler() once at boot.
func (r *Router) AttachMetricsHandler(h http.Handler) { r.metricsHandler = h }

// AttachMetricsHTTPClient overrides the http.Client the
// /api/v1/metrics/series proxy uses to call upstream Prometheus
// (gm-e9m0). Tests pass a client wired to httptest.Server so they
// don't depend on a real Prometheus instance. cmd/gemba serve does
// not call this — production runs use the default 15s-timeout client.
func (r *Router) AttachMetricsHTTPClient(c *http.Client) { r.metricsHTTPClient = c }

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
	mode := "full"
	if r.cfg.BeadsOnly {
		mode = "beads_only"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"auth_mode":      r.cfg.EffectiveAuthMode(),
		"yolo_available": r.cfg.DangerouslySkipPermissions,
		"listen":         r.cfg.Listen,
		"port":           r.cfg.Port,
		"mode":           mode,
		"beads_only":     r.cfg.BeadsOnly,
		"city":           r.cfg.City,
		"town":           r.cfg.Town,
		// beads_source is the workspace identity — 1:1 with a beads
		// database. The SPA renders it in the Topbar so operators
		// running multiple gemba instances against different databases
		// can tell them apart at a glance.
		"beads_source":       r.cfg.BeadsSource(),
		"beads_history_path": r.cfg.BeadsOnlyManifest(),
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
