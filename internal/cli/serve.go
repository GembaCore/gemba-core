package cli

import (
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	gemba "github.com/GembaCore/gemba-core"
	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/adapter/bd"
	"github.com/GembaCore/gemba-core/internal/adapter/dolt"
	"github.com/GembaCore/gemba-core/internal/adapter/gt"
	"github.com/GembaCore/gemba-core/internal/adapter/mock"
	"github.com/GembaCore/gemba-core/internal/adapter/native"
	"github.com/GembaCore/gemba-core/internal/adapter/native/agents"
	"github.com/GembaCore/gemba-core/internal/adapter/native/backend"
	"github.com/GembaCore/gemba-core/internal/adapter/noop"
	"github.com/GembaCore/gemba-core/internal/auth"
	"github.com/GembaCore/gemba-core/internal/config"
	corepersona "github.com/GembaCore/gemba-core/internal/core/persona"
	"github.com/GembaCore/gemba-core/internal/persona"
	"github.com/GembaCore/gemba-core/internal/personas/onboarder"
	"github.com/GembaCore/gemba-core/internal/server"
	"github.com/GembaCore/gemba-core/internal/server/metrics"
	"github.com/GembaCore/gemba-core/internal/shader"
	"github.com/GembaCore/gemba-core/internal/shader/gastown"
	"github.com/GembaCore/gemba-core/internal/skills/epic_order"
	"github.com/GembaCore/gemba-core/internal/skills/escalation_handoff"
	"github.com/GembaCore/gemba-core/internal/transport/api"
	"github.com/GembaCore/gemba-core/internal/walk"
	walksources "github.com/GembaCore/gemba-core/internal/walk/sources"
)

func newServeCmd(b BuildInfo) *cobra.Command {
	var cfg config.ServeConfig
	var quiet bool

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the Gemba HTTP server",
		Long: `Runs the Gemba HTTP server.

By default binds to 127.0.0.1:7666 with no authentication. To expose on the
network, pass --listen with a non-loopback address AND --auth to enable
authentication. Binding a non-loopback interface without --auth is an error.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := cfg.NormalizeListen(cmd.Flags().Changed("port")); err != nil {
				return err
			}
			if err := cfg.ValidateTLSFlags(); err != nil {
				return err
			}
			return runServe(cmd.Context(), cfg, b, quiet, cmd.OutOrStdout())
		},
	}

	cmd.Flags().StringVar(&cfg.Listen, "listen", "127.0.0.1",
		"interface to bind to, or host:port (e.g. 127.0.0.1:7666, [::1]:7666); "+
			"use 0.0.0.0 for all interfaces (requires --auth)")
	cmd.Flags().IntVar(&cfg.Port, "port", 7666, "TCP port to listen on")
	cmd.Flags().BoolVar(&cfg.Open, "open", false,
		"open the UI in a browser after starting")

	cmd.Flags().StringVar(&cfg.AuthMode, "auth", "",
		"authentication mode: none (default), token, oidc")
	cmd.Flags().StringVar(&cfg.TLSCert, "tls-cert", "",
		"path to TLS certificate (use --tls-self-signed to auto-generate)")
	cmd.Flags().StringVar(&cfg.TLSKey, "tls-key", "",
		"path to TLS key file")
	cmd.Flags().BoolVar(&cfg.TLSSelfSigned, "tls-self-signed", false,
		"generate a self-signed TLS certificate on first run")

	cmd.Flags().StringVar(&cfg.City, "city", "",
		"path to Gas City workspace (default: auto-detect city.toml or ~/my-city)")
	cmd.Flags().StringVar(&cfg.Town, "town", "",
		"path to Gas Town HQ (legacy; prefer --city for Gas City workspaces)")

	cmd.Flags().StringVar(&cfg.ConfigPath, "config", "",
		"path to gemba.toml (default: probe cwd, then ~/.config/gemba/)")

	cmd.Flags().StringVar(&cfg.OrchestratorConfigPath, "orchestrator-config", "",
		"path to .gemba/orchestrator.json (default: probe cwd; missing → no shader)")

	cmd.Flags().StringVar(&cfg.Orchestration, "orchestration", "native",
		"orchestration plane adaptor to bind ('native' = direct-to-shell, 'none' = no plane)")

	cmd.Flags().StringVar(&cfg.TerminalBackend, "terminal", "auto",
		"terminal backend when --orchestration=native: auto|tmux|iterm|terminal")

	cmd.Flags().StringVar(&cfg.AgentsRegistryPath, "agents-registry", "",
		"path to .gemba/agents.toml for --orchestration=native "+
			"(default: .gemba/agents.toml in cwd; missing is non-fatal)")

	cmd.Flags().StringVar(&cfg.WorktreesDir, "worktrees-dir", "",
		"parent directory for per-session worktrees "+
			"(default: sibling 'worktrees' next to repo root)")

	cmd.Flags().StringVar(&cfg.BeadsDir, "beads-dir", "",
		"path to the beads workspace the WorkPlane adaptor targets "+
			"(required unless --dolt-url is set; mutually exclusive with it)")

	cmd.Flags().StringVar(&cfg.DoltURL, "dolt-url", "",
		"mysql://user[:pass]@host:port/dbname of a Dolt server to read/write "+
			"beads directly (required unless --beads-dir is "+
			"set; mutually exclusive with it)")

	cmd.Flags().BoolVar(&cfg.Noop, "noop", false,
		"bind the in-memory noop reference WorkPlane + OrchestrationPlane "+
			"(dev/demo; mutually exclusive with --beads-dir / --dolt-url; "+
			"forces --orchestration=noop)")

	cmd.Flags().BoolVar(&cfg.BeadsOnly, "beads-only", false,
		"run as a Beads-only viewer/manager: no project or orchestration required; "+
			"mutations append a JSONL Beads history manifest")
	cmd.Flags().BoolVar(&cfg.BeadsReadOnly, "beads-read-only", false,
		"run Beads-only with every Beads mutation blocked")
	cmd.Flags().StringVar(&cfg.BeadsOnlyManifestPath, "beads-history", "",
		"path to the Beads-only JSONL manifest "+
			"(default: <beads-dir>/.gemba/session-manifest.jsonl)")
	cmd.Flags().StringVar(&cfg.DoltURL, "beads-url", "",
		"alias for --dolt-url; Beads/Dolt URL to use as the work source")
	cmd.Flags().BoolVar(&cfg.Restart, "restart", false,
		"allow gemba serve to restart local helper services when required by the selected mode")

	// gm-e9m0: upstream Prometheus URL for the
	// /api/v1/metrics/series proxy. Empty (the default) means
	// "fall back to PROM_URL env, then [metrics].prom_url in
	// ~/.gemba/config.toml, then return 503 from the endpoint."
	cmd.Flags().StringVar(&cfg.PromURL, "prom-url", "",
		"base URL of an upstream Prometheus instance for the "+
			"/api/v1/metrics/series Insights proxy "+
			"(default: PROM_URL env, then [metrics].prom_url in ~/.gemba/config.toml)")

	// Flag name copied verbatim from Claude Code. Do not rename or soften.
	cmd.Flags().BoolVar(&cfg.DangerouslySkipPermissions,
		"dangerously-skip-permissions", false,
		"disable mutation confirmation prompts for this server session "+
			"(name copied from Claude Code; intentional)")

	cmd.Flags().BoolVar(&quiet, "quiet", false,
		"suppress the startup banner")

	// gm-s47n.12: pool config path. Empty defaults to
	// .gemba/pool.toml in cwd; missing file = no pools (Phase 0
	// zero-delta). When non-empty AND the file exists, the
	// auto-dispatch daemon is constructed per (rig, persona) entry
	// with size > 0 in the file.
	cmd.Flags().StringVar(&cfg.PoolConfigPath, "pool-config", "",
		"path to pool.toml declaring [pool.<rig>.<persona>] blocks "+
			"(default: probe .gemba/pool.toml; missing → no pools)")

	return cmd
}

func runServe(ctx context.Context, cfg config.ServeConfig, b BuildInfo, quiet bool, bannerOut io.Writer) error {
	applyServeEnvDefaults(&cfg)
	normalizeServeMode(&cfg)

	if err := cfg.ValidateBindPolicy(); err != nil {
		return err
	}

	// gm-root.19: resolve the Beads server URL from config / built-in
	// default when the operator has not supplied explicit WorkPlane
	// flags. We do this before ValidateWorkPlaneFlags so the resolution
	// order (CLI > config.toml > built-in default) is applied once, in
	// one place, before any validation that requires a flag to be set.
	if err := applyBeadsURLDefault(&cfg); err != nil {
		return err
	}

	// gm-e9m0: resolve the upstream Prometheus URL the
	// /api/v1/metrics/series proxy queries. CLI > PROM_URL env >
	// [metrics].prom_url > "" (unconfigured; handler returns 503).
	if err := applyPromURLDefault(&cfg); err != nil {
		return err
	}

	// --beads-dir and --dolt-url are mutually exclusive; catch the
	// conflict before any further startup work so the operator gets a
	// clean message instead of silently seeing one ignored.
	if err := cfg.ValidateWorkPlaneFlags(); err != nil {
		return err
	}
	// --beads-dir, when set, must resolve to a real rig (contains .beads/
	// or IS .beads/). Validating here — before any other startup work —
	// gives the operator an actionable error instead of a later cryptic
	// "bd: no .beads/ found" from a spawned subprocess. The resolved path
	// is what bd's subprocess will use as cwd (see registerWorkPlane).
	resolvedBeadsDir, err := cfg.ResolveBeadsDir()
	if err != nil {
		return err
	}

	// gm-root.17.4: bd presence gate. Runs AFTER flag/config
	// validation so the operator gets actionable feedback on wrong
	// invocations before being asked to install bd, but BEFORE any
	// subprocess-spawning work that would fail cryptically without
	// bd on PATH.
	if shouldProbeBd(cfg) {
		if err := probeBd(os.Stderr); err != nil {
			return err
		}
	} else {
		slog.Info("beads-only Dolt URL mode: skipping bd CLI startup probe")
	}
	cfg.BeadsDir = resolvedBeadsDir
	if cfg.BeadsReadOnly && cfg.Restart && cfg.BeadsDir != "" {
		if err := restartBdReadonly(ctx, cfg.BeadsDir); err != nil {
			return err
		}
	}
	if cfg.DangerouslySkipPermissions {
		slog.Warn("DANGEROUSLY-SKIP-PERMISSIONS IS ACTIVE",
			"note", "mutations will not require confirmation for this session")
	}

	// gm-e3.6.1: register the W3C trace-context propagator and (when
	// OTEL_EXPORTER_OTLP_ENDPOINT is set) an OTLP HTTP exporter. The
	// propagator registration is required even with no exporter so the
	// trace middleware can extract incoming traceparent headers and
	// stamp event.TraceID at the adaptor boundary.
	otelShutdown, err := initOTEL(ctx, b)
	if err != nil {
		return fmt.Errorf("init otel: %w", err)
	}
	defer func() {
		if err := otelShutdown(context.Background()); err != nil {
			slog.Warn("otel: exporter shutdown failed", "err", err)
		}
	}()

	// Token auth: verify against the argon2id hash file on disk. If the
	// file is missing, bootstrap it by generating a fresh token, printing
	// it ONCE to stderr, and persisting only the hash. The plaintext
	// token is never written anywhere else and never passes through slog
	// (gm-e5.2 DoD).
	if cfg.EffectiveAuthMode() == "token" && cfg.AuthToken == "" {
		if cfg.AuthTokenHashPath == "" {
			p, err := auth.DefaultTokenPath()
			if err != nil {
				return fmt.Errorf("resolve token path: %w", err)
			}
			cfg.AuthTokenHashPath = p
		}
		if err := ensurePrimaryToken(cfg.AuthTokenHashPath); err != nil {
			return err
		}
	}
	if cfg.EffectiveAuthMode() == "token" {
		tok, err := auth.NewToken()
		if err != nil {
			return fmt.Errorf("generate auth bootstrap token: %w", err)
		}
		cfg.AuthBootstrapToken = tok
		cfg.AuthBootstrapExpiresAt = time.Now().Add(10 * time.Minute)
	}

	reg, err := registerWorkPlane(ctx, cfg)
	if err != nil {
		return err
	}
	host := reg.Host

	if cfg.BeadsOnly {
		cfg.Orchestration = "none"
		slog.Info("beads-only mode active; orchestration plane will not be registered",
			"manifest", cfg.BeadsOnlyManifest())
	} else {
		if err := registerOrchestrationPlane(ctx, host, cfg); err != nil {
			return err
		}
	}

	// gm-s47n.12: load pool config + construct one auto-dispatch
	// daemon per (rig, persona) with size > 0. Phase 0 zero-delta:
	// when no pool config exists OR every entry has size = 0, NO
	// daemons are constructed and behavior is identical to today's
	// main. The clamp + WARN emission lives in this function.
	resolvedPools, poolCfg, poolCfgPath, poolMaxParallel, err := loadAndResolvePoolsWithMeta(cfg)
	if err != nil {
		return err
	}

	if !quiet {
		printStartupBanner(bannerOut, b, cfg, reg, resolvedPools)
	}
	if cfg.AuthBootstrapToken != "" {
		printAuthBootstrapURL(os.Stderr, cfg)
	}

	// gm-root.17.4: cold-start redirect — if no project exists under
	// the configured default_dir, tell the router to redirect the SPA
	// root to /new. Non-fatal: a config I/O error is logged and serve
	// continues without redirecting so a broken config.toml doesn't
	// block the operator.
	if cfg.BeadsOnly {
		cfg.ColdStartRedirect = false
	} else if redirect, err := coldStartRedirect(cfg); err != nil {
		slog.Warn("cold-start redirect probe failed; serving normally",
			"err", err)
	} else {
		cfg.ColdStartRedirect = redirect
		if redirect {
			slog.Info("cold-start: no projects found; SPA root will redirect to /new")
		}
	}

	addr := fmt.Sprintf("%s:%d", cfg.Listen, cfg.Port)
	spa := gemba.SPA()
	if spa == nil {
		slog.Warn("SPA not embedded in this binary",
			"hint", "run `make build` to embed web/dist; "+
				"non-API routes will return a 503 build hint")
	}
	handler := server.NewRouter(cfg, spa, host)
	// gm-s47n.12: surface the resolved pool config to the router so
	// /api/pools can return declared vs effective sizes.
	handler.AttachPools(resolvedPools)
	// gm-s47n.16: bind pool.toml path + MaxParallel for the editor's
	// /api/pool-config endpoint. Empty path is fine — GET returns an
	// empty envelope and PUT returns 400 path_not_configured.
	handler.AttachPoolConfig(poolCfgPath, poolMaxParallel, poolCfg.ReservedForManual)
	// Construct + run the daemons. Phase 0 zero-delta path: when
	// resolvedPools is empty, this loop is a no-op and no goroutines
	// are spawned.
	if len(resolvedPools) > 0 {
		startPoolDaemons(ctx, handler, host, resolvedPools, poolCfg)
	}
	// gm-s47n.12 §10.3: persist session.recycled events to the
	// session_recycles dolt table. Only fires when Mode=dolt-sql
	// (the bd CLI path doesn't expose a *sql.DB); other modes
	// silently skip — the events still flow through the SSE hub
	// for the SPA, just without persistence.
	if op := host.OrchestrationPlane(); op != nil && reg.DoltDB != nil {
		_ = server.StartRecycleWriter(ctx, op, reg.DoltDB)
	}
	// Start the adaptor HealthBus ticker so /api/adaptors and
	// /api/adaptors/stream read from a shared cache instead of
	// probing once per client request (gm-root.7).
	handler.StartHealthBus()
	defer handler.Close()

	// Wire the bound OrchestrationPlane's Subscribe stream into the
	// /events SSE hub (gm-e4.3) so adaptor events fan out to SPA
	// clients without each client polling. Best-effort: a Subscribe
	// failure logs but doesn't stop the server — the SPA degrades to
	// the polling paths /sessions and EscalationPanel still ship.
	if op := host.OrchestrationPlane(); op != nil && handler.EventsHub() != nil {
		ch, err := op.Subscribe(ctx, core.SubscribeFilter{})
		if err != nil {
			slog.Warn("events: orchestration Subscribe failed; SSE stream will be empty",
				"err", err)
		} else {
			handler.EventsHub().AttachOrchestrationStream(ctx,
				op.Describe().AdaptorID, ch)
		}
		// gm-3jv: cross-worker escalation aggregator. The bridge
		// republishes escalation.* OrchestrationEvents as
		// walk.escalation_changed frames on the same hub so an
		// active walk's UI can refresh its agenda live without a
		// dedicated subscription. Nil-safe — see
		// walksources.StartOrchestrationBridge.
		walksources.StartOrchestrationBridge(ctx, op, handler.EventsHub())
	}

	// Wire the bound WorkPlane's Subscribe stream into the hub
	// (gm-e4.3.1) so workitem.* mutations fan out to /board and
	// /backlog subscribers without polling. An adaptor that opts out
	// (noop, dolt read-only) returns KindUnsupported — we treat that
	// as "no events from this plane" and skip silently.
	if wp := host.WorkPlane(); wp != nil && handler.EventsHub() != nil {
		wch, werr := wp.Subscribe(ctx, core.WorkPlaneSubscribeFilter{})
		switch {
		case werr == nil:
			m, mErr := wp.Describe(ctx)
			adaptorID := ""
			if mErr == nil {
				adaptorID = m.AdaptorName
			}
			handler.EventsHub().AttachWorkPlaneStream(ctx, adaptorID, wch)
		case errors.Is(werr, core.ErrUnsupported):
			// Expected for read-only / noop adaptors; no log.
		default:
			slog.Warn("events: workplane Subscribe failed; workitem SSE stream will be empty",
				"err", werr)
		}
	}

	// Bind the Prometheus collector to the events hub and expose
	// /metrics. The collector subscribes once with no filter so every
	// hub-published event drives a metric (gm-e3.6.2).
	if hub := handler.EventsHub(); hub != nil {
		mc := metrics.NewCollector()
		go mc.Run(ctx, hub)
		handler.AttachMetricsHandler(mc.Handler())
	}

	// gm-twp2: persona consult dispatcher + skill registry. The
	// HTTP read endpoints (/api/skills*, /api/consults*) return 503
	// until this attaches; with it bound the SPA can render the
	// /plan recommend-order surface and /insights/personas list.
	//
	// Skills register at startup — adding a new skill means a new
	// import + Register call below. The audit log defaults to
	// $HOME/.gemba/persona; operators with a custom location can
	// override via persona.NewAuditLog(<path>) once that wiring lands.
	skillRegistry := corepersona.NewSkillRegistry()
	if err := epic_order.Register(skillRegistry); err != nil {
		// A duplicate-id Register failure here would mean the binary
		// is misbuilt (two skills claiming the same id), which is a
		// programmer error rather than an operator one. Log and
		// continue — the read endpoints will simply not surface the
		// affected skill.
		slog.Warn("personas: epic_order.Register failed; skill not exposed",
			"err", err)
	}
	// gm-e11.8.7: register the EscalationsPage Hand-off dispatcher
	// skill. Personas that should be hand-off targets must list
	// `escalation_handoff` in their TOML `skills` block — the
	// PersonaCanInvoke gate in the dispatcher checks for it before
	// resolving the skill.
	if err := escalation_handoff.Register(skillRegistry); err != nil {
		slog.Warn("personas: escalation_handoff.Register failed; skill not exposed",
			"err", err)
	}
	personaDispatcher := persona.NewDispatcher(persona.NewAuditLog(""))
	// Persona registry: load persona TOML files from the workspace's
	// .gemba/personas/ directory. Missing dir is acceptable — POST
	// /api/consults will 503 until personas exist; the read
	// endpoints don't depend on the registry.
	personaRegistry := loadPersonaRegistry(cfg)
	handler.AttachPersonaDispatcher(personaDispatcher, skillRegistry, personaRegistry)

	// gm-twp2.1: per-skill applier executor. When a WorkPlane is
	// bound, register epic_order's PATCH /api/work-items/{id}
	// executor on the dispatcher. Without this the apply endpoint
	// stays in record-only mode (operator dispatches the
	// SuggestedAction manually).
	if wp := host.WorkPlane(); wp != nil {
		personaDispatcher.SetApplier(epic_order.ID, epic_order.NewApplier(wp))
	}

	// gm-twp2: bridge tailer → dispatcher.Receive plumbing. The
	// orchestration plane's GembaSkillOutput frames already land in
	// the events hub as Kind=skill.output_emitted; FanFromHub
	// subscribes and routes each event's lines into the dispatcher
	// via the consult ID carried as SessionID. Runs on a long-lived
	// goroutine and shuts down when ctx is cancelled.
	if hub := handler.EventsHub(); hub != nil {
		go persona.FanFromHub(ctx, personaDispatcher, hub)
	}

	// gm-twp2 spawn integration: when an OrchestrationPlane is
	// bound, install NativeSpawn as the dispatcher's post-Begin
	// hook. POST /api/consults then launches a Claude Code session
	// per consult. "claude" is the default agents.toml entry the
	// PM-class personas dispatch as (the emit_skill_output MCP
	// tool ships with the Claude Code bridge); operators can
	// override via per-persona agent-type config in a follow-up
	// slice. When no orchestration plane is bound the spawn func
	// stays nil and POST /api/consults runs in dry-run mode.
	if op := host.OrchestrationPlane(); op != nil {
		personaDispatcher.SetSpawnFunc(persona.NativeSpawn(op, "claude"))
	}

	// gm-vch2: wire the gemba walk surface. The Sources factory bundles
	// the live OrchestrationPlane / Witness / Refinery / Beads-degraded
	// listers so cross-worker escalations land on every walk's agenda.
	// Witness and Refinery default to no-op sources today — a deployment
	// with a live witness or refinery rig swaps in a custom
	// WitnessFindingSource / RefineryRejectionSource via a follow-up
	// configuration knob (no live in-tree adaptor yet).
	walkSources := walksources.LiveSources(walksources.LiveSourcesConfig{
		Plane:     host.OrchestrationPlane(),
		HealthBus: handler.HealthBus(),
		// Wire the bound WorkPlane as the WorkItems lister so the
		// walk's bead_filed / bead_closed sources surface recently
		// touched beads in the agenda. Without this, a WorkPlane-
		// only deployment (no OrchestrationPlane) gets an empty
		// walk even when there are 24h-recent beads worth reviewing.
		WorkItems: host.WorkPlane(),
		// Witness + Refinery left nil; the no-op default contributes
		// zero items until an upstream source is wired.
	})
	handler.AttachWalk(walk.NewMemoryStore(), walkSources)

	// gm-ad1u: wire the /bootstrap wizard backend. In-memory store
	// matches walk's pattern; SQL persistence is a follow-up bead.
	handler.AttachBootstrap(server.NewMemoryBootstrapStore())

	// gm-root.17: wire the /onboard conversational project-creation
	// flow. Memory store + the real Onboarder skill turner
	// (gm-root.17.10) + the production ratifier (gm-root.17.6 —
	// atomic transaction). Defaults: HOME-based config resolution,
	// exec-based shell-out for bd / git.
	//
	// The Onboarder turner lazily resolves an LLM client on first
	// /start probe by reading ~/.gemba/config.toml's [llm] table.
	// When no client is configured, /start returns 503 with the
	// canonical diagnostic so the SPA's /onboard route and board CTA
	// can render it verbatim — see docs/design/newproject.md §"Credential
	// resolution".
	handler.AttachNewProject(
		server.NewMemoryNewProjectStore(),
		onboarder.NewSkillTurner(onboarder.DefaultResolver(cfg.ConfigPath)),
		server.NewRatifier(server.RatifierConfig{}),
	)

	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Resolve the TLS posture before opening the listener. Two modes:
	//   1. operator-supplied --tls-cert/--tls-key: load from disk and
	//      warn (don't abort) if the key file is world/group readable
	//   2. --tls-self-signed: generate an ephemeral ECDSA P-256 cert
	//      with SANs covering localhost + the configured bind host;
	//      print the SHA-256 fingerprint at startup so the operator
	//      can pin it from a curl --cacert / --pin run
	// ValidateTLSFlags has already rejected partial/conflicting input.
	if err := configureTLS(srv, &cfg); err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	go func() {
		scheme := "http"
		if cfg.TLSEnabled() {
			scheme = "https"
		}
		slog.Info("gemba listening",
			"url", fmt.Sprintf("%s://%s", scheme, addr),
			"auth", cfg.EffectiveAuthMode())

		var err error
		if cfg.TLSEnabled() {
			// Cert + key already loaded into srv.TLSConfig.Certificates
			// by configureTLS, so the empty-string args here select the
			// embedded chain rather than re-reading from disk.
			err = srv.ListenAndServeTLS("", "")
		} else {
			err = srv.ListenAndServe()
		}
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down")

	shutdownCtx, cancelShutdown := context.WithTimeout(
		context.Background(), 10*time.Second)
	defer cancelShutdown()
	return srv.Shutdown(shutdownCtx)
}

func applyServeEnvDefaults(cfg *config.ServeConfig) {
	if cfg == nil {
		return
	}
	if !cfg.BeadsOnly && strings.EqualFold(strings.TrimSpace(os.Getenv("GEMBA_MODE")), "beads_only") {
		cfg.BeadsOnly = true
	}
	if truthyEnv(os.Getenv("GEMBA_BEADS_READ_ONLY")) {
		cfg.BeadsReadOnly = true
	}
	if cfg.DoltURL == "" {
		if v := strings.TrimSpace(os.Getenv("GEMBA_BEADS_URL")); v != "" {
			cfg.DoltURL = v
		}
	}
	if cfg.BeadsDir == "" {
		if v := strings.TrimSpace(os.Getenv("GEMBA_BEADS_DIR")); v != "" {
			cfg.BeadsDir = v
		}
	}
	if cfg.BeadsOnlyManifestPath == "" {
		if v := strings.TrimSpace(os.Getenv("GEMBA_BEADS_ONLY_MANIFEST")); v != "" {
			cfg.BeadsOnlyManifestPath = v
		}
	}
}

func normalizeServeMode(cfg *config.ServeConfig) {
	if cfg == nil {
		return
	}
	if cfg.BeadsReadOnly {
		cfg.BeadsOnly = true
	}
}

func truthyEnv(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func restartBdReadonly(ctx context.Context, dir string) error {
	slog.Info("beads-read-only: restarting local bd Dolt server in readonly mode",
		"beads_dir", dir)
	if out, err := runBdCommand(ctx, dir, "dolt", "stop"); err != nil {
		slog.Warn("beads-read-only: bd dolt stop failed; attempting readonly start",
			"err", err,
			"output", strings.TrimSpace(string(out)))
	}
	if out, err := runBdCommand(ctx, dir, "--readonly", "dolt", "start"); err != nil {
		return fmt.Errorf("beads-read-only: start bd Dolt server readonly: %w\n%s",
			err, strings.TrimSpace(string(out)))
	}
	return nil
}

func runBdCommand(ctx context.Context, dir string, args ...string) ([]byte, error) {
	path, err := exec.LookPath("bd")
	if err != nil {
		return nil, core.WrapAdaptorError(core.KindAdaptorDegraded, err,
			"beads: bd CLI not on PATH")
	}
	cmd := exec.CommandContext(ctx, path, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	return cmd.CombinedOutput()
}

func shouldProbeBd(cfg config.ServeConfig) bool {
	if cfg.BeadsOnly && cfg.DoltURL != "" && cfg.BeadsDir == "" {
		return false
	}
	return true
}

// registerWorkPlane builds the api transport host, instantiates the
// configured WorkPlane adaptor, and binds them. Failures here MUST
// abort startup — a serve process with no WorkPlane has no useful
// surface to expose, so the operator needs to see the error before
// the listener opens.
//
// Two adaptor paths, selected by flag:
//   - --beads-dir (or default cwd): shell to the bd CLI (writes OK)
//   - --dolt-url: direct Dolt SQL (writes OK unless --beads-read-only)
//
// ValidateWorkPlaneFlags already guaranteed they're not both set, so
// we only have to decide between "dolt-url present" and "everything
// else".
// workPlaneReg bundles the api.Host with the manifest and source metadata
// needed by the startup banner. Register* functions populate all fields.
type workPlaneReg struct {
	Host     *api.Host
	Manifest core.CapabilityManifest
	// Mode is the adaptor path the operator selected: "bd-dir" (bd CLI)
	// or "dolt-sql" (direct Dolt SQL).
	Mode string
	// SourceKind labels Source for the banner ("dir" or "url").
	SourceKind string
	// Source is the dir path or redacted Dolt URL.
	Source string
	// DoltDB is the live *sql.DB when Mode="dolt-sql"; nil
	// otherwise. The session_recycles writer reads this for the
	// audit-table INSERT (gm-s47n.12, spec §10.3). Optional — when
	// nil, the writer becomes a no-op.
	DoltDB *sql.DB
}

func registerWorkPlane(ctx context.Context, cfg config.ServeConfig) (*workPlaneReg, error) {
	host := api.New()
	// gm-ygwe: cold-start gate — when the resolved Beads URL came from
	// the built-in default fallback AND no project marker exists under
	// the configured [projects].default_dir, refuse to instantiate the
	// WorkPlane. Otherwise a stray local Dolt "gemba" database would
	// silently surface stale work to a fresh operator. Project-data
	// routes (/api/work-items, /api/sprints, /api/escalations, ...)
	// already 503 with adaptor_not_configured when the WorkPlane is
	// nil — see internal/server/work_items.go and friends. The SPA's
	// cold-start /new redirect (gm-root.17.4) plus the project picker
	// empty-state (gm-root.18) carry the operator from here.
	skip, skipErr := coldStartShouldSkipBind(cfg)
	if skipErr != nil {
		// Non-fatal: a permissions or config error must NOT block
		// startup — the worse failure mode is "server can't boot at
		// all," which is harder to diagnose than "no projects, here
		// is the picker." Log loudly and keep going.
		slog.Warn("cold-start gate probe failed; proceeding to bind WorkPlane",
			"err", skipErr)
	}
	if skip && !cfg.BeadsOnly {
		slog.Info("cold-start: no projects found and Beads URL is built-in default; "+
			"WorkPlane will NOT be bound — every project-data route returns 503 "+
			"adaptor_not_configured until the operator creates a project at /new "+
			"or restarts with an explicit --dolt-url / --beads-dir",
			"beads_url_source", cfg.BeadsURLSource,
			"hint", "open / in the SPA; you will be redirected to /new")
		return &workPlaneReg{
			Host:       host,
			Manifest:   core.CapabilityManifest{},
			Mode:       "cold-start",
			SourceKind: "none",
			Source:     "no projects found; WorkPlane unbound",
		}, nil
	}
	sh, err := buildShader(cfg)
	if err != nil {
		return nil, err
	}
	if cfg.Noop {
		return registerNoopWorkPlane(ctx, host, sh)
	}
	if cfg.DoltURL != "" {
		return registerDoltWorkPlane(ctx, host, cfg, sh)
	}
	return registerBeadsWorkPlane(ctx, host, cfg, sh)
}

// registerNoopWorkPlane binds the in-memory reference WorkPlane (gm-e3.7).
// Used by `gemba serve --noop` for SPA development, demos, and conformance
// smoke-testing — the noop adaptor itself is exercised by the conformance
// harness in internal/adapter/noop and testing/.
func registerNoopWorkPlane(ctx context.Context, host *api.Host, sh core.Shader) (*workPlaneReg, error) {
	adaptor := noop.NewWorkPlaneWithTransport(core.TransportAPI)
	wrapped := shader.Wrap(adaptor, sh)
	reg, err := host.RegisterWorkPlane(ctx, wrapped)
	if err != nil {
		return nil, fmt.Errorf("register noop workplane: %w", err)
	}
	manifest, err := wrapped.Describe(ctx)
	if err != nil {
		return nil, fmt.Errorf("describe noop workplane: %w", err)
	}
	slog.Info("workplane adaptor registered",
		"adaptor", reg.AdaptorName,
		"version", reg.AdaptorVersion,
		"protocol", reg.ProtocolVersion,
		"transport", reg.Transport,
		"mode", "noop")
	return &workPlaneReg{
		Host:       host,
		Manifest:   manifest,
		Mode:       "noop",
		SourceKind: "memory",
		Source:     "in-memory reference adaptor",
	}, nil
}

// buildShader resolves the orchestrator config (gm-root.4) and
// constructs the matching core.Shader. Returns NopShader when no
// config file is present — callers don't need to handle nil.
func buildShader(cfg config.ServeConfig) (core.Shader, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return core.NopShader{}, nil // best-effort; degrade to nop
	}
	path := config.ResolveOrchestratorConfigPath(cfg.OrchestratorConfigPath, cwd)
	if path == "" {
		return core.NopShader{}, nil
	}
	oc, err := config.LoadOrchestratorConfig(path)
	if err != nil {
		return nil, fmt.Errorf("orchestrator config: %w", err)
	}
	switch oc.Orchestrator {
	case "gastown":
		var gcfg config.GastownShaderConfig
		if len(oc.Config) > 0 {
			if err := json.Unmarshal(oc.Config, &gcfg); err != nil {
				return nil, fmt.Errorf("orchestrator config[gastown]: %w", err)
			}
		}
		sh, err := gastown.New(gastown.Config{
			Rig:          gcfg.Rig,
			RigAbbr:      gcfg.RigAbbr,
			IDFormat:     gcfg.IDFormat,
			TitleFormat:  gcfg.TitleFormat,
			KindPrefixes: gcfg.KindPrefixes,
		})
		if err != nil {
			return nil, err
		}
		return sh, nil
	case "nop", "":
		return core.NopShader{}, nil
	default:
		return nil, fmt.Errorf("orchestrator config: unknown orchestrator %q", oc.Orchestrator)
	}
}

func registerBeadsWorkPlane(ctx context.Context, host *api.Host, cfg config.ServeConfig, sh core.Shader) (*workPlaneReg, error) {
	adaptor, err := bd.NewWorkPlane(bd.Config{BeadsDir: cfg.BeadsDir, ReadOnly: cfg.BeadsReadOnly})
	if err != nil {
		return nil, fmt.Errorf("beads workplane: %w", err)
	}
	wrapped := shader.Wrap(adaptor, sh)
	reg, err := host.RegisterWorkPlane(ctx, wrapped)
	if err != nil {
		return nil, fmt.Errorf("register beads workplane: %w", err)
	}
	manifest, err := wrapped.Describe(ctx)
	if err != nil {
		return nil, fmt.Errorf("describe beads workplane: %w", err)
	}
	slog.Info("workplane adaptor registered",
		"adaptor", reg.AdaptorName,
		"version", reg.AdaptorVersion,
		"protocol", reg.ProtocolVersion,
		"transport", reg.Transport,
		"read_only", cfg.BeadsReadOnly,
		"beads_dir", cfg.BeadsDir)
	return &workPlaneReg{
		Host:       host,
		Manifest:   manifest,
		Mode:       "bd-dir",
		SourceKind: "dir",
		Source:     cfg.BeadsDir,
	}, nil
}

func registerDoltWorkPlane(ctx context.Context, host *api.Host, cfg config.ServeConfig, sh core.Shader) (*workPlaneReg, error) {
	adaptor, err := dolt.NewWorkPlane(dolt.Config{URL: cfg.DoltURL, ReadOnly: cfg.BeadsReadOnly})
	if err != nil {
		return nil, fmt.Errorf("dolt workplane: %w", err)
	}
	// Hand the live pool to the registry-side probe so /api/adaptors
	// reflects real Dolt health instead of falsely reporting
	// "not configured (pass --dolt-url to enable)" — the dolt probe
	// has no view of ServeConfig and only knows the pool is wired
	// when this hook is set.
	dolt.SetProbeDB(adaptor.DB())
	wrapped := shader.Wrap(adaptor, sh)
	reg, err := host.RegisterWorkPlane(ctx, wrapped)
	if err != nil {
		dolt.SetProbeDB(nil)
		_ = adaptor.Close()
		return nil, fmt.Errorf("register dolt workplane: %w", err)
	}
	manifest, err := wrapped.Describe(ctx)
	if err != nil {
		dolt.SetProbeDB(nil)
		_ = adaptor.Close()
		return nil, fmt.Errorf("describe dolt workplane: %w", err)
	}
	redacted := redactDoltURL(cfg.DoltURL)
	slog.Info("workplane adaptor registered",
		"adaptor", reg.AdaptorName,
		"version", reg.AdaptorVersion,
		"protocol", reg.ProtocolVersion,
		"transport", reg.Transport,
		"read_only", cfg.BeadsReadOnly,
		"dolt_url", redacted)
	return &workPlaneReg{
		Host:       host,
		Manifest:   manifest,
		Mode:       "dolt-sql",
		SourceKind: "url",
		Source:     redacted,
		DoltDB:     adaptor.DB(),
	}, nil
}

// registerOrchestrationPlane binds the operator-selected
// OrchestrationPlaneAdaptor onto the api Host. A single-slot
// invariant (gm-native.1) lives in the Host itself: Register will
// refuse a second call with KindValidation. The flag defaults to
// "native" so /coach + /api/operational-context return data on
// fresh installs; pass --orchestration=none (or empty) to disable
// the plane explicitly.
func registerOrchestrationPlane(ctx context.Context, host *api.Host, cfg config.ServeConfig) error {
	mode := cfg.Orchestration
	// --noop short-circuits the orchestration selector. The noop reference
	// plane has no real spawn surface — it just maintains in-memory state
	// for the SPA / harness.
	if cfg.Noop {
		mode = "noop"
	}
	switch mode {
	case "", "none":
		return nil
	case "noop":
		return registerNoopOrchestration(ctx, host)
	case "native":
		return registerNativeOrchestration(ctx, host, cfg)
	case "mock":
		return registerMockOrchestration(ctx, host, cfg)
	case "gastown":
		return registerGastownOrchestration(ctx, host)
	default:
		return fmt.Errorf("orchestration: unknown adaptor %q (want 'native', 'noop', 'mock', 'gastown', 'none', or empty)", cfg.Orchestration)
	}
}

// registerMockOrchestration binds the in-process mock plane
// (gm-root.28). Operator-usable as a 'dry-run' mode: real bd
// closes + gemba-state bead-done emits, but no claude session
// spawn. The autodispatch daemon dispatches to mock sessions like
// any other plane; SessionReady recycling exercises the same
// pool warmth path the native adaptor does.
func registerMockOrchestration(ctx context.Context, host *api.Host, cfg config.ServeConfig) error {
	// Pre-seed one mock session per persona discovered in
	// <project>/.gemba/personas/. The autodispatch daemon expects
	// idle sessions to exist before it dispatches; without
	// pre-seed, mock-mode would require the operator to manually
	// kick off a session via the SPA before any work flows.
	personas := discoverPersonaNames(cfg.BeadsDir)
	if len(personas) == 0 {
		personas = personasFromPoolConfig(cfg.PoolConfigPath)
	}
	// Pass the bound workplane so the runner can fetch+close
	// beads in-process (no shell-out to bd, no embedded-Dolt
	// lock race with the workplane adaptor itself).
	wp := host.WorkPlane()
	plane := mock.NewOrchestrationPlane(mock.Config{
		ProjectDir:      cfg.BeadsDir,
		PreseedPersonas: personas,
		WorkPlane:       wp,
	})
	reg, err := host.RegisterOrchestrationPlane(ctx, plane)
	if err != nil {
		return fmt.Errorf("register mock orchestration: %w", err)
	}
	slog.Info("orchestration plane registered",
		"adaptor", reg.AdaptorName,
		"version", reg.AdaptorVersion,
		"protocol", reg.ProtocolVersion,
		"transport", reg.Transport,
		"mode", "mock",
		"project_dir", cfg.BeadsDir)
	return nil
}

// discoverPersonaNames lists the persona toml files under
// <project>/.gemba/personas/ and returns their persona ids
// (filename without extension). Used to pre-seed the mock plane
// with one session per persona at registration time. Best-effort:
// returns empty slice on any error.
func discoverPersonaNames(projectDir string) []string {
	dir := filepath.Join(projectDir, ".gemba", "personas")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".toml") {
			continue
		}
		out = append(out, strings.TrimSuffix(name, ".toml"))
	}
	return out
}

func personasFromPoolConfig(path string) []string {
	if path == "" {
		return nil
	}
	poolCfg, err := config.LoadPoolConfig(path)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	add := func(persona string) {
		persona = strings.TrimSpace(persona)
		if persona == "" || seen[persona] {
			return
		}
		seen[persona] = true
		out = append(out, persona)
	}
	add(poolCfg.DefaultPersona)
	for _, persona := range poolCfg.Routing {
		add(persona)
	}
	for _, byPersona := range poolCfg.Pools {
		for persona := range byPersona {
			add(persona)
		}
	}
	return out
}

// registerGastownOrchestration binds the gt-CLI orchestration
// adaptor (gm-root.27.35). The gt-adaptor capability probe
// (gm-e7.12) runs at construction; if the gt binary is missing or
// too old, NewOrchestrationPlane returns an error and the server
// fails to start with a clear message.
func registerGastownOrchestration(ctx context.Context, host *api.Host) error {
	plane, err := gt.NewOrchestrationPlane()
	if err != nil {
		return fmt.Errorf("gastown orchestration: %w", err)
	}
	reg, err := host.RegisterOrchestrationPlane(ctx, plane)
	if err != nil {
		return fmt.Errorf("register gastown orchestration: %w", err)
	}
	slog.Info("orchestration plane registered",
		"adaptor", reg.AdaptorName,
		"version", reg.AdaptorVersion,
		"protocol", reg.ProtocolVersion,
		"transport", reg.Transport,
		"mode", "gastown")
	return nil
}

// registerNoopOrchestration binds the in-memory reference OrchestrationPlane
// (gm-e3.7). Companion to registerNoopWorkPlane — together these unblock
// SPA development without a real backend.
func registerNoopOrchestration(ctx context.Context, host *api.Host) error {
	plane := noop.NewOrchestrationPlaneWithTransport(core.TransportAPI)
	reg, err := host.RegisterOrchestrationPlane(ctx, plane)
	if err != nil {
		return fmt.Errorf("register noop orchestration: %w", err)
	}
	slog.Info("orchestration plane registered",
		"adaptor", reg.AdaptorName,
		"version", reg.AdaptorVersion,
		"protocol", reg.ProtocolVersion,
		"transport", reg.Transport,
		"mode", "noop")
	return nil
}

// registerNativeOrchestration wires the native adaptor end-to-end
// (gm-native.20). Resolves the concrete terminal backend, loads the
// agents.toml registry, discovers the repo root, then constructs the
// OrchestrationPlane via NewWithConfig so StartSession can actually
// spawn panes. A missing agents.toml is a non-fatal warning — serve
// comes up with an empty roster and the SPA surfaces a clear empty
// state instead of crashing.
func registerNativeOrchestration(ctx context.Context, host *api.Host, cfg config.ServeConfig) error {
	kind, err := backend.ResolveKind(backend.Kind(cfg.TerminalBackend))
	if err != nil {
		return fmt.Errorf("orchestration=native: %w", err)
	}
	b, err := backend.Select(kind)
	if err != nil {
		return fmt.Errorf("orchestration=native: %w", err)
	}

	registryPath := cfg.AgentsRegistryPath
	if registryPath == "" {
		registryPath = ".gemba/agents.toml"
	}
	registry, regErr := agents.Load(registryPath)
	if regErr != nil {
		// Missing file is the common first-run case — warn, keep going
		// with an empty registry so the SPA's agent-type picker shows
		// 'no agents registered; drop a .gemba/agents.toml to wire
		// one up' instead of 503ing the entire orchestration plane.
		slog.Warn("native orchestration: agents.toml not loaded; registry will be empty",
			"path", registryPath,
			"err", regErr)
	}

	slog.Info("native orchestration plane selected",
		"backend", string(kind),
		"agents_registry", registryPath,
		"agents_registered", len(registry.Names()))

	plane := native.NewWithConfig(native.Config{
		Backend:      b,
		Registry:     registry,
		WorkPlane:    host.WorkPlane(),
		RepoRoot:     os.Getenv("PWD"),
		WorktreesDir: cfg.WorktreesDir,
	})
	reg, err := host.RegisterOrchestrationPlane(ctx, plane)
	if err != nil {
		return fmt.Errorf("register native orchestration: %w", err)
	}
	slog.Info("orchestration plane registered",
		"adaptor", reg.AdaptorName,
		"version", reg.AdaptorVersion,
		"protocol", reg.ProtocolVersion,
		"transport", reg.Transport)
	return nil
}

// printStartupBanner emits the three-line operator-facing summary the gm-root.1.3
// bead specifies: build version, effective adaptor path, and a one-line manifest
// digest (state count, core + extension edges, feature-flag posture). Output is
// plain text on stdout so `gemba serve | tee` captures it verbatim; the slog
// pipeline is reserved for structured machine-readable lines.
//
// The banner never carries credentials: the Dolt URL is redacted upstream by
// redactDoltURL before it lands in workPlaneReg.Source.
func printStartupBanner(w io.Writer, b BuildInfo, cfg config.ServeConfig, reg *workPlaneReg, pools []config.ResolvedPool) {
	version := b.Version
	if version == "" {
		version = "dev"
	}
	fmt.Fprintf(w, "▶ gemba %s  listen=%s:%d  auth=%s\n",
		version, cfg.Listen, cfg.Port, cfg.EffectiveAuthMode())
	if cfg.BeadsOnly {
		fmt.Fprintf(w, "▶ mode: beads-only (history=%s)\n", cfg.BeadsOnlyManifest())
	}
	// gm-ygwe: cold-start mode binds no WorkPlane; the manifest is a
	// zero value. Render a clear single-line banner for that case so the
	// operator sees "no project bound" instead of a confused "▶
	// workplane:  (mode=cold-start ...)" with empty adaptor identity.
	if reg.Mode == "cold-start" {
		fmt.Fprintf(w, "▶ workplane: <unbound> (mode=cold-start; %s)\n", reg.Source)
		fmt.Fprintln(w, "▶ manifest: <none> — open / in the SPA to create or attach a project")
		return
	}
	fmt.Fprintf(w, "▶ workplane: %s %s (mode=%s %s=%s)\n",
		reg.Manifest.AdaptorName, reg.Manifest.AdaptorVersion,
		reg.Mode, reg.SourceKind, reg.Source)
	fmt.Fprintf(w, "▶ manifest: %d states, 3 core + %d extension edges, feature flags: sprint=%s budget=%s\n",
		len(reg.Manifest.StateMap),
		len(reg.Manifest.EdgeExtensions),
		yesNo(reg.Manifest.SprintNative),
		yesNo(reg.Manifest.TokenBudgetEnforced))
	// gm-s47n.12 §3.3: surface effective pool size next to the
	// (implicit) MaxParallel cap so the clamp is visible without
	// grepping the slog stream. Phase 0 zero-delta: when no pools
	// are resolved the line is omitted entirely so existing banner
	// regression tests stay green.
	if len(pools) > 0 {
		for _, p := range pools {
			clamp := ""
			if p.ClampActivated {
				clamp = fmt.Sprintf(" (clamped from %d by MaxParallel)", p.SizeDeclared)
			}
			fmt.Fprintf(w, "▶ pool[%s/%s]: size=%d%s floor=%.2f recycle_after_beads=%d idle_ceiling_min=%d\n",
				p.Scope, p.Persona,
				p.SizeEffective, clamp,
				p.Floor, p.RecycleAfterBeads, p.IdleCeilingMinutes)
		}
	}
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// redactDoltURL strips any password component out of the URL before
// it hits the slog pipeline. The Dolt URL may carry a password in
// the user-info section; logging it verbatim would leak the
// credential to any log sink.
func redactDoltURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw
	}
	if _, ok := u.User.Password(); !ok {
		return raw
	}
	u.User = url.User(u.User.Username())
	return u.String()
}

// ensurePrimaryToken makes sure path contains a valid argon2id hash. When
// the file is absent it generates a fresh token, prints the plaintext once
// to stderr, and persists the hash with 0600 permissions. Stderr (not the
// slog pipeline) is used deliberately so the token does not enter any
// structured log stream.
func ensurePrimaryToken(path string) error {
	existing, err := auth.ReadHash(path)
	if err != nil {
		return fmt.Errorf("read token hash: %w", err)
	}
	if existing != "" {
		return nil
	}
	tok, err := auth.NewToken()
	if err != nil {
		return fmt.Errorf("generate auth token: %w", err)
	}
	hash, err := auth.HashToken(tok)
	if err != nil {
		return fmt.Errorf("hash auth token: %w", err)
	}
	if err := auth.WriteHash(path, hash); err != nil {
		return fmt.Errorf("write token hash: %w", err)
	}
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "==============================================================")
	fmt.Fprintln(os.Stderr, "  Gemba generated a new auth token. This is the ONLY time it")
	fmt.Fprintln(os.Stderr, "  will be shown. Copy it now.")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "  Token:  "+tok)
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "  Header: Authorization: Bearer <token>")
	fmt.Fprintln(os.Stderr, "  Hash:   "+path)
	fmt.Fprintln(os.Stderr, "==============================================================")
	fmt.Fprintln(os.Stderr)
	return nil
}

func printAuthBootstrapURL(w io.Writer, cfg config.ServeConfig) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, "==============================================================")
	fmt.Fprintln(w, "  Gemba generated a one-time browser login URL.")
	fmt.Fprintln(w, "  Open it to unlock this server without pasting the token.")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "  Open:   "+authBootstrapURL(cfg))
	fmt.Fprintln(w, "  Until:  "+cfg.AuthBootstrapExpiresAt.Format(time.RFC3339))
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "  If it expires, paste the primary token at the browser prompt.")
	fmt.Fprintln(w, "==============================================================")
	fmt.Fprintln(w)
}

func authBootstrapURL(cfg config.ServeConfig) string {
	scheme := "http"
	if cfg.TLSEnabled() {
		scheme = "https"
	}
	host := browserHost(cfg.Listen)
	u := url.URL{
		Scheme:   scheme,
		Host:     net.JoinHostPort(host, strconv.Itoa(cfg.Port)),
		Path:     "/",
		Fragment: "gemba-bootstrap=" + cfg.AuthBootstrapToken,
	}
	return u.String()
}

func browserHost(listen string) string {
	switch strings.Trim(listen, "[]") {
	case "", "0.0.0.0", "::":
		return "127.0.0.1"
	default:
		return listen
	}
}

// configureTLS resolves the TLS posture for the http.Server based on
// the validated ServeConfig flags. Three branches:
//   - TLSSelfSigned: generate an ephemeral ECDSA P-256 cert and print
//     its SHA-256 fingerprint to stderr so the operator can pin it
//   - TLSCert/TLSKey: load operator-supplied chain from disk and warn
//     (do not abort) if the key file is world/group readable
//   - neither: leave srv.TLSConfig nil; plain HTTP path
//
// The cert is attached via srv.TLSConfig.Certificates so the
// ListenAndServeTLS("","") call in runServe uses the in-memory chain
// instead of re-reading from disk. ValidateTLSFlags has already
// rejected conflicting input before we reach this point.
func configureTLS(srv *http.Server, cfg *config.ServeConfig) error {
	if !cfg.TLSEnabled() {
		return nil
	}

	var cert tls.Certificate
	if cfg.TLSSelfSigned {
		c, fp, err := auth.GenerateSelfSignedCert(auth.SelfSignedCertOptions{
			BindHost: cfg.Listen,
		})
		if err != nil {
			return fmt.Errorf("tls self-signed: %w", err)
		}
		cert = c
		// Stderr (not slog) keeps the fingerprint reachable on a `gemba
		// serve | tee logs.json` pipeline without polluting structured
		// logs an operator may forward to a SIEM. Same precedent as the
		// auth-token bootstrap message above.
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "==============================================================")
		fmt.Fprintln(os.Stderr, "  Gemba generated a self-signed TLS certificate.")
		fmt.Fprintln(os.Stderr, "  Pin this fingerprint from clients to detect MITM:")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "  SHA-256: "+fp)
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "  curl --insecure works for quick checks, but pin in production.")
		fmt.Fprintln(os.Stderr, "==============================================================")
		fmt.Fprintln(os.Stderr)
		slog.Info("tls: self-signed certificate generated",
			"fingerprint_sha256", fp,
			"valid_for", "365d")
	} else {
		// Operator-supplied chain. Permission warning is a soft signal:
		// some deploys sit behind a chmod-relaxed shared volume and that
		// is the operator's call. We log loudly but boot anyway.
		if _, warn, err := auth.CheckCertKeyPermissions(cfg.TLSKey); err != nil {
			return fmt.Errorf("tls key %s: %w", cfg.TLSKey, err)
		} else if warn != "" {
			slog.Warn("tls key permissions advisory", "msg", warn)
		}
		c, err := tls.LoadX509KeyPair(cfg.TLSCert, cfg.TLSKey)
		if err != nil {
			return fmt.Errorf("load tls keypair: %w", err)
		}
		cert = c
		slog.Info("tls: operator-supplied certificate loaded",
			"cert", cfg.TLSCert,
			"key", cfg.TLSKey)
	}

	srv.TLSConfig = &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}
	return nil
}

// loadPersonaRegistry resolves the persona TOML directory from the
// resolved beads-dir (the workspace root) and loads it. A missing
// directory yields an empty registry rather than an error — the
// operator may not have authored personas yet, and POST /api/consults
// degrades to 503 cleanly via the lazy-attach gate. A malformed file
// logs and yields an empty registry so a single bad TOML can't
// brick the whole serve startup.
//
// The personas dir lives at <beads-dir parent>/.gemba/personas — but
// for the read-only adaptors today the workspace root maps to the
// dir containing .beads, so .gemba lives alongside .beads. When
// beads-dir is empty (no WorkPlane bound) the registry stays empty.
func loadPersonaRegistry(cfg config.ServeConfig) *corepersona.Registry {
	if cfg.BeadsDir == "" {
		return corepersona.NewRegistry()
	}
	// .beads/ is inside the workspace root; .gemba/personas is a
	// sibling. Climb one level off BeadsDir which is itself the
	// .beads-containing dir per ResolveBeadsDir's contract.
	personaDir := filepath.Join(cfg.BeadsDir, ".gemba", "personas")
	reg, err := corepersona.LoadRegistry(personaDir)
	if err != nil {
		// LoadRegistry returns an error only on truly bad input (a
		// file that fails to decode). A missing dir is reported as
		// "no personas" — the registry comes back empty and that's
		// fine. Anything else is operator-actionable: log the path.
		slog.Warn("personas: LoadRegistry failed; POST /api/consults will 503",
			"dir", personaDir, "err", err)
		return corepersona.NewRegistry()
	}
	return reg
}
