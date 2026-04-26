package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	gemba "github.com/MikeBengtson/gemba"
	"github.com/MikeBengtson/gemba/internal/adapter/bd"
	"github.com/MikeBengtson/gemba/internal/adapter/dolt"
	"github.com/MikeBengtson/gemba/internal/adapter/native"
	"github.com/MikeBengtson/gemba/internal/adapter/native/agents"
	"github.com/MikeBengtson/gemba/internal/adapter/native/backend"
	"github.com/MikeBengtson/gemba/internal/auth"
	"github.com/MikeBengtson/gemba/internal/config"
	"github.com/MikeBengtson/gemba/internal/core"
	corepersona "github.com/MikeBengtson/gemba/internal/core/persona"
	"github.com/MikeBengtson/gemba/internal/persona"
	"github.com/MikeBengtson/gemba/internal/server"
	"github.com/MikeBengtson/gemba/internal/server/metrics"
	"github.com/MikeBengtson/gemba/internal/shader"
	"github.com/MikeBengtson/gemba/internal/shader/gastown"
	"github.com/MikeBengtson/gemba/internal/skills/epic_order"
	"github.com/MikeBengtson/gemba/internal/transport/api"
	"github.com/spf13/cobra"
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

	cmd.Flags().StringVar(&cfg.Orchestration, "orchestration", "",
		"orchestration plane adaptor to bind (empty = none, 'native' = direct-to-shell)")

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
		"mysql://user[:pass]@host:port/dbname of a Dolt server to read "+
			"beads directly (read-only; required unless --beads-dir is "+
			"set; mutually exclusive with it)")

	// Flag name copied verbatim from Claude Code. Do not rename or soften.
	cmd.Flags().BoolVar(&cfg.DangerouslySkipPermissions,
		"dangerously-skip-permissions", false,
		"disable mutation confirmation prompts for this server session "+
			"(name copied from Claude Code; intentional)")

	cmd.Flags().BoolVar(&quiet, "quiet", false,
		"suppress the startup banner")

	return cmd
}

func runServe(ctx context.Context, cfg config.ServeConfig, b BuildInfo, quiet bool, bannerOut io.Writer) error {
	if err := cfg.ValidateBindPolicy(); err != nil {
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
	cfg.BeadsDir = resolvedBeadsDir
	if cfg.DangerouslySkipPermissions {
		slog.Warn("DANGEROUSLY-SKIP-PERMISSIONS IS ACTIVE",
			"note", "mutations will not require confirmation for this session")
	}

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

	reg, err := registerWorkPlane(ctx, cfg)
	if err != nil {
		return err
	}
	host := reg.Host

	if err := registerOrchestrationPlane(ctx, host, cfg); err != nil {
		return err
	}

	if !quiet {
		printStartupBanner(bannerOut, b, cfg, reg)
	}

	addr := fmt.Sprintf("%s:%d", cfg.Listen, cfg.Port)
	spa := gemba.SPA()
	if spa == nil {
		slog.Warn("SPA not embedded in this binary",
			"hint", "run `make build` to embed web/dist; "+
				"non-API routes will return a 503 build hint")
	}
	handler := server.NewRouter(cfg, spa, host)
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
	personaDispatcher := persona.NewDispatcher(persona.NewAuditLog(""))
	handler.AttachPersonaDispatcher(personaDispatcher, skillRegistry)

	// gm-twp2: bridge tailer → dispatcher.Receive plumbing. The
	// orchestration plane's GembaSkillOutput frames already land in
	// the events hub as Kind=skill.output_emitted; FanFromHub
	// subscribes and routes each event's lines into the dispatcher
	// via the consult ID carried as SessionID. Runs on a long-lived
	// goroutine and shuts down when ctx is cancelled.
	if hub := handler.EventsHub(); hub != nil {
		go persona.FanFromHub(ctx, personaDispatcher, hub)
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	go func() {
		scheme := "http"
		if cfg.TLSCert != "" || cfg.TLSSelfSigned {
			scheme = "https"
		}
		slog.Info("gemba listening",
			"url", fmt.Sprintf("%s://%s", scheme, addr),
			"auth", cfg.EffectiveAuthMode())

		var err error
		if scheme == "https" {
			err = srv.ListenAndServeTLS(cfg.TLSCert, cfg.TLSKey)
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

// registerWorkPlane builds the api transport host, instantiates the
// configured WorkPlane adaptor, and binds them. Failures here MUST
// abort startup — a serve process with no WorkPlane has no useful
// surface to expose, so the operator needs to see the error before
// the listener opens.
//
// Two adaptor paths, selected by flag:
//   - --beads-dir (or default cwd): shell to the bd CLI (writes OK)
//   - --dolt-url: direct read-only Dolt SQL (writes return KindReadOnly)
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
}

func registerWorkPlane(ctx context.Context, cfg config.ServeConfig) (*workPlaneReg, error) {
	host := api.New()
	sh, err := buildShader(cfg)
	if err != nil {
		return nil, err
	}
	if cfg.DoltURL != "" {
		return registerDoltWorkPlane(ctx, host, cfg, sh)
	}
	return registerBeadsWorkPlane(ctx, host, cfg, sh)
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
	adaptor, err := bd.NewWorkPlane(bd.Config{BeadsDir: cfg.BeadsDir})
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
	adaptor, err := dolt.NewWorkPlane(dolt.Config{URL: cfg.DoltURL})
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
		"read_only", true,
		"dolt_url", redacted)
	return &workPlaneReg{
		Host:       host,
		Manifest:   manifest,
		Mode:       "dolt-sql",
		SourceKind: "url",
		Source:     redacted,
	}, nil
}

// registerOrchestrationPlane binds the operator-selected
// OrchestrationPlaneAdaptor onto the api Host. A single-slot
// invariant (gm-native.1) lives in the Host itself: Register will
// refuse a second call with KindValidation. Empty Orchestration is
// a no-op (today's default) — the SPA will reflect an absent
// orchestration manifest.
func registerOrchestrationPlane(ctx context.Context, host *api.Host, cfg config.ServeConfig) error {
	switch cfg.Orchestration {
	case "":
		return nil
	case "native":
		return registerNativeOrchestration(ctx, host, cfg)
	default:
		return fmt.Errorf("orchestration: unknown adaptor %q (want 'native' or empty)", cfg.Orchestration)
	}
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
func printStartupBanner(w io.Writer, b BuildInfo, cfg config.ServeConfig, reg *workPlaneReg) {
	version := b.Version
	if version == "" {
		version = "dev"
	}
	fmt.Fprintf(w, "▶ gemba %s  listen=%s:%d  auth=%s\n",
		version, cfg.Listen, cfg.Port, cfg.EffectiveAuthMode())
	fmt.Fprintf(w, "▶ workplane: %s %s (mode=%s %s=%s)\n",
		reg.Manifest.AdaptorName, reg.Manifest.AdaptorVersion,
		reg.Mode, reg.SourceKind, reg.Source)
	fmt.Fprintf(w, "▶ manifest: %d states, 3 core + %d extension edges, feature flags: sprint=%s budget=%s\n",
		len(reg.Manifest.StateMap),
		len(reg.Manifest.EdgeExtensions),
		yesNo(reg.Manifest.SprintNative),
		yesNo(reg.Manifest.TokenBudgetEnforced))
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
