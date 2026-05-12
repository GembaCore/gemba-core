// Package config: see doc.go for the overview.
package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ServeConfig captures every flag `gemba serve` accepts. Held on the CLI side
// and passed into the server; keep it a pure data type.
type ServeConfig struct {
	Listen string
	Port   int
	Open   bool

	AuthMode string
	// AuthToken, when non-empty, is the plaintext bearer compared against
	// incoming requests. Set it for tests and for --auth-token overrides;
	// production runs leave it empty and let the server verify against the
	// hashed file at AuthTokenHashPath.
	AuthToken string
	// AuthTokenHashPath points at the argon2id PHC hash file written by
	// `gemba auth token rotate`. Defaults to ~/.gemba/tokens/primary.
	AuthTokenHashPath string
	// AuthBootstrapToken is an ephemeral one-time token used by the SPA to
	// exchange a launch URL fragment for the normal session cookie. It is
	// generated at process start for token-auth deployments and is never
	// persisted.
	AuthBootstrapToken     string
	AuthBootstrapExpiresAt time.Time
	TLSCert                string
	TLSKey                 string
	TLSSelfSigned          bool

	// City is the path to a Gas City workspace (preferred).
	// Town is the path to a Gas Town HQ (legacy; kept for back-compat).
	// Exactly one may be set; an empty workspace defers to auto-detect.
	City string
	Town string

	// ProjectDir is the project worktree root the bd WorkPlane adaptor
	// targets. It is the preferred operator-facing name for the local
	// project source selected by `gemba serve --project-dir`.
	ProjectDir string

	// BeadsDir is the legacy name for ProjectDir. It is retained so older
	// scripts using `--beads-dir` and GEMBA_BEADS_DIR keep working. After
	// resolution, serve normalizes BeadsDir to the resolved project root
	// because the bd adaptor still consumes this field.
	BeadsDir string

	// DoltURL is a mysql://user[:password]@host:port/dbname connection
	// string pointing at a Dolt server that already hosts a beads
	// database. When set, the server skips the bd CLI path and opens
	// a direct SQL connection instead (gm-0fd). Mutually exclusive with
	// BeadsDir. Writes are enabled unless BeadsReadOnly is true or the
	// Dolt user/server denies them.
	DoltURL string

	// BeadsURLSource records where the resolved DoltURL came from after
	// applyBeadsURLDefault runs (gm-ygwe). Values:
	//
	//	"cli"     — the operator passed --dolt-url (or --project-dir) explicitly
	//	"config"  — populated from ~/.gemba/config.toml's [beads].url
	//	"default" — populated from the built-in DefaultBeadsURL fallback
	//	""        — applyBeadsURLDefault has not been called yet
	//
	// The "default" branch is the cold-start danger zone: a stray local
	// Dolt database named "gemba" would otherwise silently bind, surfacing
	// stale work to a fresh operator with no projects on this machine. The
	// cold-start gate (registerWorkPlane / coldStartShouldSkipBind) refuses
	// to bind a WorkPlane when the source is "default" and no project
	// marker exists under the configured default_dir.
	BeadsURLSource string

	// Noop, when true, binds the in-memory noop reference adaptors for
	// both the WorkPlane and OrchestrationPlane (gm-e3.7). Intended for
	// SPA development, demos, and conformance smoke-testing. Mutually
	// exclusive with BeadsDir / DoltURL because the noop WorkPlane is
	// itself a complete adaptor — there is no backend behind it.
	// When set, --orchestration is forced to "noop".
	Noop bool

	// BeadsOnly, when true, runs Gemba as a Beads viewer/manager without
	// binding an OrchestrationPlane or requiring a project. Mutating Beads
	// actions append an informational JSONL manifest instead of dispatching
	// sessions.
	BeadsOnly bool

	// BeadsReadOnly implies BeadsOnly and blocks every Beads mutation at
	// the server and adaptor boundary. URL sources are otherwise writable
	// when the configured Dolt user has write permissions.
	BeadsReadOnly bool

	// Restart permits serve to restart local helper processes when needed
	// to enforce a selected mode, for example bd Dolt readonly posture.
	Restart bool

	// BeadsOnlyManifestPath optionally overrides the JSONL manifest path
	// used by BeadsOnly mode. Empty defaults to .gemba/session-manifest.jsonl
	// under BeadsDir when available, otherwise the current working directory.
	BeadsOnlyManifestPath string

	// ConfigPath is an explicit gemba.toml override. Empty means "probe
	// the standard locations." File loading lands with a later bead;
	// serve threads the path through today so the flag surface is stable.
	ConfigPath string

	// OrchestratorConfigPath points at a JSON file describing the
	// active orchestrator's shader settings (gm-root.4). Empty means
	// "probe ./.gemba/orchestrator.json"; missing → no shader, NopShader
	// takes over. Set explicitly via --orchestrator-config.
	OrchestratorConfigPath string

	// Orchestration selects which OrchestrationPlaneAdaptor to bind at
	// server startup (gm-native.2). The CLI flag defaults to "native"
	// so /coach + /api/operational-context return data on fresh
	// installs. Supported values:
	//   "native"      — direct-to-shell adaptor (gm-native), default
	//   "none" or ""  — no orchestration plane; SPA hides
	//                   agent-management surfaces, /coach 503s
	// Future values may include "gt" (gastown) etc.; the single-slot
	// invariant (gm-native.1) means only one may be active at a time.
	Orchestration string

	// TerminalBackend picks the shell multiplexer backend when
	// Orchestration=="native". Empty or "auto" detects via env
	// (TMUX / TERM_PROGRAM). Accepted: auto | tmux | iterm | terminal.
	TerminalBackend string

	// AgentsRegistryPath points to the TOML file the native
	// OrchestrationPlane loads its agent-type registry from
	// (gm-native.6). Empty defaults to ".gemba/agents.toml" in the
	// workspace. Missing file is non-fatal: the SPA's picker shows an
	// empty roster until the operator drops a registry in.
	AgentsRegistryPath string

	// WorktreesDir overrides the default worktree parent dir for the
	// native adaptor's StartSession provisioner. Empty defaults to a
	// sibling "worktrees" directory next to the repo root.
	WorktreesDir string

	DangerouslySkipPermissions bool

	// ColdStartRedirect, when true, tells the server to redirect the SPA
	// root to /new because bd is present but no project was detected under
	// the configured default_dir (gm-root.17.4). Set by runServe before
	// NewRouter is called; not a CLI flag.
	ColdStartRedirect bool

	// PromURL is the base URL of an upstream Prometheus instance the
	// /api/v1/metrics/series proxy queries (gm-e9m0; D4 — gm-sf51).
	// Resolution order is CLI flag > PROM_URL env > [metrics].prom_url
	// in ~/.gemba/config.toml > "" (unconfigured). An empty value is
	// a legitimate run mode: the time-series endpoint returns a
	// 503 KindAdaptorDegraded telling the SPA to render the
	// "awaiting Prometheus" stub. Resolution happens once at boot,
	// inside applyPromURLDefault, so handlers see the final value.
	PromURL string

	// CORSAllowedOrigins enables CORS on /api/* and /events when
	// non-empty. CORS is OFF BY DEFAULT (gm-e4.1) — same-origin SPA
	// requests don't need it, and a public CORS surface is a footgun
	// (operators serve gemba on loopback by default). Wildcard "*"
	// allowed for trusted dev setups; production should list explicit
	// origins. Methods + headers are derived from the Gemba HTTP
	// surface (GET / POST / PATCH / DELETE; X-GEMBA-Confirm).
	CORSAllowedOrigins []string

	// EmbeddedDolt, when true, tells `gemba serve` to spawn and
	// supervise its own dolt sql-server subprocess instead of
	// connecting to an externally-managed one (gm-o9t8.1.2.3). The
	// runtime default lands in applyEmbeddedDoltDefault — true when
	// no --dolt-url is supplied, false otherwise. Both modes can
	// coexist; the bd CLI / Dolt adaptors don't care which one is
	// providing the SQL endpoint.
	EmbeddedDolt bool

	// EmbeddedDoltSet records whether --embedded-dolt was passed
	// explicitly on the command line. When false the runtime picks
	// the default ("true if no --dolt-url else false").
	EmbeddedDoltSet bool

	// DoltDataDir is the on-disk path the embedded dolt sql-server
	// uses as its working dir. Empty defaults to "<cwd>/data/dolt"
	// at startup. Created with 0700 on first launch.
	DoltDataDir string

	// DoltEmbeddedDB is the name of the MySQL database the dolt
	// adaptor will open against the embedded supervisor when
	// --embedded-dolt is on and no external --dolt-url is set
	// (gm-o9t8.1.7). Defaults to "gemba" at startup. Has no effect
	// when --dolt-url is set (external Dolt selects its own db).
	DoltEmbeddedDB string

	// WorkspacesRoot is the on-disk parent directory that holds per-
	// workspace <wsid>/repo/ trees (gm-o9t8.1.16). The router's
	// /api/v1/workspaces/{wsid}/diff handler resolves
	// <WorkspacesRoot>/<wsid>/repo/ to find the working tree it
	// streams `git diff` from. Empty → /diff returns 503
	// adaptor_not_configured. Defaults at startup to
	// "<dirname(DoltDataDir)>/workspaces" so the layout follows the
	// data-dir convention (see internal/server/workspacelayout).
	WorkspacesRoot string

	// PoolConfigPath points at a TOML file declaring the
	// [pool.<rig>.<persona>] blocks that drive the auto-dispatch
	// daemon (gm-s47n.12, spec §3.3). Empty means "no pool config" —
	// the Phase 0 zero-delta default; no daemons constructed.
	// Resolution order: explicit --pool-config flag > probe
	// .gemba/pool.toml in cwd > "" (no pools).
	//
	// Pool sizing CROSS-REFERENCES MaxParallel (the orchestration
	// manifest's per-host pane cap). The clamp computes
	//
	//	effective_size = min(declared_size, MaxParallel - reserved_for_manual)
	//
	// reserved_for_manual defaults to 1 — at least one slot is held
	// back from the pool so a human's manual drag is never starved
	// by a saturated pool. When the clamp activates, gemba logs a
	// WARN naming declared/cap/effective sizes and the SPA's
	// /api/pools endpoint surfaces both numbers. See
	// docs/deployment/pool-sizing.md for the full operator guide.
	PoolConfigPath string
}

// NormalizeListen splits c.Listen if it is in host:port form (e.g.
// "127.0.0.1:7666" or "[::1]:7666"), routing the port half into c.Port and
// reducing c.Listen to the host. This lets users type the universal Unix
// host:port idiom for --listen instead of having to remember --port is a
// separate flag.
//
// portFlagSet should be true if the caller (CLI) saw an explicit --port flag;
// supplying both forms is an error so precedence is never ambiguous.
func (c *ServeConfig) NormalizeListen(portFlagSet bool) error {
	host, port, err := net.SplitHostPort(c.Listen)
	if err != nil {
		// Not host:port form (missing colon, IPv6 literal without brackets,
		// empty string, etc). Leave Listen alone.
		return nil
	}
	if portFlagSet {
		return fmt.Errorf(
			"port specified twice: --listen %q includes a port and --port "+
				"was also given; pass one or the other", c.Listen)
	}
	p, err := strconv.Atoi(port)
	if err != nil || p < 1 || p > 65535 {
		return fmt.Errorf("invalid port in --listen %q", c.Listen)
	}
	c.Listen = host
	c.Port = p
	return nil
}

// EffectiveAuthMode returns the auth mode that will actually be applied,
// after defaulting to "none" and normalizing.
func (c ServeConfig) EffectiveAuthMode() string {
	if c.AuthMode == "" {
		return "none"
	}
	return c.AuthMode
}

// ValidateBindPolicy refuses to start the server if the configuration would
// expose the API on a non-loopback interface without authentication.
//
// This is the single most important security check. See gm-e5.1 for the
// full rationale and test matrix.
func (c ServeConfig) ValidateBindPolicy() error {
	loopback, err := isLoopback(c.Listen)
	if err != nil {
		return fmt.Errorf("cannot resolve --listen %q: %w", c.Listen, err)
	}

	if loopback {
		return nil // localhost-only; always ok
	}

	if c.EffectiveAuthMode() == "none" {
		return fmt.Errorf(
			"non-loopback bind requires --auth; got --listen %s\n"+
				"  pass --auth=token to generate a token, or keep the default bind\n"+
				"  (127.0.0.1) — see docs/remote-access.md",
			c.listenDisplay())
	}
	return nil
}

// ValidateTLSFlags enforces the TLS selector contract: either pass
// --tls-cert AND --tls-key together (operator-supplied chain), or pass
// --tls-self-signed alone (server generates an ephemeral cert at boot),
// or pass nothing (plain HTTP). Mixing the two paths or supplying only
// one half of the cert/key pair is an error so the operator sees the
// problem before the listener opens.
//
// Per gm-root locked decision #8 (AUTH): TLS is the recommended
// transport for any non-loopback bind, and the self-signed mode exists
// so an operator can stand up an HTTPS listener with one flag while
// they wait for a real cert.
func (c ServeConfig) ValidateTLSFlags() error {
	hasCert := c.TLSCert != ""
	hasKey := c.TLSKey != ""
	if hasCert != hasKey {
		return fmt.Errorf(
			"--tls-cert and --tls-key must be set together; " +
				"got one without the other")
	}
	if (hasCert || hasKey) && c.TLSSelfSigned {
		return fmt.Errorf(
			"--tls-self-signed is mutually exclusive with --tls-cert/--tls-key; " +
				"pick one path\n" +
				"  --tls-cert/--tls-key      load an operator-supplied cert from disk\n" +
				"  --tls-self-signed         generate an ephemeral self-signed cert at startup")
	}
	return nil
}

// TLSEnabled reports whether the server should bind an HTTPS listener
// based on the configured TLS flags.
func (c ServeConfig) TLSEnabled() bool {
	return c.TLSCert != "" || c.TLSKey != "" || c.TLSSelfSigned
}

// ValidateWorkPlaneFlags enforces the WorkPlane selector contract:
// exactly one of --project-dir and --dolt-url must be set. Both select a
// beads backend but by different means; a server asked to honor both
// would have to pick one and ignore the other, and a server with
// neither has no WorkPlane to serve at all. In both error cases we
// fail before any further startup work so the operator gets a single
// actionable message instead of a later cryptic failure from the
// adaptor layer.
func (c ServeConfig) ValidateWorkPlaneFlags() error {
	projectDir := c.localProjectDir()
	if c.Noop && (projectDir != "" || c.DoltURL != "") {
		return fmt.Errorf(
			"--noop is mutually exclusive with --project-dir and --dolt-url; " +
				"the noop adaptor is itself a complete in-memory WorkPlane\n" +
				"  drop --project-dir / --dolt-url, or drop --noop")
	}
	if c.Noop {
		return nil
	}
	if projectDir != "" && c.DoltURL != "" {
		return fmt.Errorf(
			"--project-dir and --dolt-url are mutually exclusive; " +
				"pass one or the other\n" +
				"  --project-dir routes reads+writes through the bd CLI\n" +
				"  --dolt-url opens a direct SQL connection to Dolt")
	}
	// gm-o9t8.1.15: --project-dir owns its own Dolt config in .beads/
	// (managed by the bd subprocess). Booting the embedded-dolt
	// supervisor alongside it leaves the supervisor running but
	// unused. Reject the combination unless the operator explicitly
	// opted out via --embedded-dolt=false.
	if projectDir != "" && c.EmbeddedDoltSet && c.EmbeddedDolt {
		return fmt.Errorf(
			"--project-dir owns its own Dolt config; --embedded-dolt is mutually exclusive. Pick one.")
	}
	if projectDir == "" && c.DoltURL == "" {
		return fmt.Errorf(
			"no WorkPlane selected; pass --project-dir <path>, " +
				"--dolt-url <mysql://...>, or --noop\n" +
				"  --project-dir <path>        route through the bd CLI (reads + writes)\n" +
				"  --dolt-url <mysql://...>    direct SQL to a Dolt server\n" +
				"  --noop                      bind in-memory reference adaptors (dev/demo)")
	}
	return nil
}

// ResolveBeadsDir validates the configured local project directory and
// returns the directory `bd`
// should be invoked from. The bd CLI discovers its workspace by walking
// up from cwd looking for `.beads/`, so the returned path is the rig
// root: either ProjectDir itself (when it contains `.beads/`) or its
// parent (when ProjectDir *is* `.beads/`). Accepting both forms matches
// how users talk about projects in practice — `--project-dir ~/gt/gemba`
// and `--project-dir ~/gt/gemba/.beads` both mean the same project.
//
// An empty local project dir returns ("", nil); callers decide whether that's
// an error. Mutual exclusion with --dolt-url is handled separately by
// ValidateWorkPlaneFlags.
func (c ServeConfig) ResolveBeadsDir() (string, error) {
	dir := c.localProjectDir()
	flag := c.localProjectDirFlag()
	if dir == "" {
		return "", nil
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("%s %q: %w", flag, dir, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf(
				"%s %q: path does not exist", flag, dir)
		}
		return "", fmt.Errorf("%s %q: %w", flag, dir, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf(
			"%s %q: not a directory", flag, dir)
	}
	// Accept the rig root (contains .beads/) or the .beads/ dir itself.
	if filepath.Base(abs) == ".beads" {
		return filepath.Dir(abs), nil
	}
	beads := filepath.Join(abs, ".beads")
	binfo, err := os.Stat(beads)
	if err != nil || !binfo.IsDir() {
		return "", fmt.Errorf(
			"%s %q: no .beads/ directory found at %s\n"+
				"  pass the project root (containing .beads/) or the "+
				".beads/ directory itself", flag, dir, beads)
	}
	return abs, nil
}

func (c ServeConfig) ProjectRoot() string {
	if strings.TrimSpace(c.ProjectDir) != "" {
		return c.ProjectDir
	}
	return c.BeadsDir
}

func (c ServeConfig) localProjectDir() string {
	return c.ProjectRoot()
}

func (c ServeConfig) localProjectDirFlag() string {
	if strings.TrimSpace(c.ProjectDir) != "" {
		return "--project-dir"
	}
	return "--beads-dir"
}

// BeadsSource describes the configured WorkPlane source in a form safe to
// expose to operators over the API. Credentials in --dolt-url are stripped
// from Detail so the SPA can render the workspace identity in the topbar
// without leaking secrets to anyone viewing the page or its devtools.
type BeadsSource struct {
	// Kind is one of "project-dir", "dolt-url", "noop", "unconfigured".
	Kind string `json:"kind"`
	// Label is the short human-friendly identifier — basename of the
	// project-dir path, or the database name parsed from a dolt-url.
	Label string `json:"label"`
	// Detail is the redacted full source: absolute project-dir path, or
	// a mysql:// DSN with the user-info segment removed. Empty for
	// "unconfigured" and for dolt-urls that fail to parse.
	Detail string `json:"detail,omitempty"`
}

// BeadsSource derives the displayable WorkPlane identity from the configured
// flags. The mutual-exclusion contract (see ValidateWorkPlaneFlags) means at
// most one of BeadsDir / DoltURL is set in a successfully booted server; the
// "unconfigured" branch is reached only by tests that build a ServeConfig
// directly.
func (c ServeConfig) BeadsSource() BeadsSource {
	if c.Noop {
		return BeadsSource{Kind: "noop", Label: "noop", Detail: "in-memory reference adaptor"}
	}
	if dir := c.localProjectDir(); dir != "" {
		return BeadsSource{
			Kind:   "project-dir",
			Label:  filepath.Base(dir),
			Detail: dir,
		}
	}
	if c.DoltURL != "" {
		u, err := url.Parse(c.DoltURL)
		if err != nil {
			// Don't echo the raw string — it may carry creds we
			// couldn't strip. Operators still see the kind.
			return BeadsSource{Kind: "dolt-url", Label: "(unparseable dolt-url)"}
		}
		u.User = nil
		db := strings.TrimPrefix(u.Path, "/")
		if db == "" {
			db = u.Host
		}
		return BeadsSource{Kind: "dolt-url", Label: db, Detail: u.String()}
	}
	return BeadsSource{Kind: "unconfigured"}
}

// BeadsOnlyManifest resolves the append-only JSONL ledger path for
// Beads-only mode. The path is still returned when BeadsOnly=false so
// status/config endpoints can show a stable default in tests; callers
// decide whether to use it.
func (c ServeConfig) BeadsOnlyManifest() string {
	if c.BeadsOnlyManifestPath != "" {
		if filepath.IsAbs(c.BeadsOnlyManifestPath) {
			return c.BeadsOnlyManifestPath
		}
		if abs, err := filepath.Abs(c.BeadsOnlyManifestPath); err == nil {
			return abs
		}
		return filepath.Clean(c.BeadsOnlyManifestPath)
	}
	base := c.localProjectDir()
	if base == "" {
		if cwd, err := os.Getwd(); err == nil {
			base = cwd
		}
	}
	if base == "" {
		return filepath.Join(".gemba", "session-manifest.jsonl")
	}
	return filepath.Join(base, ".gemba", "session-manifest.jsonl")
}

// listenDisplay formats Listen+Port the way it would appear on the command
// line, so error messages can echo the user's intent verbatim.
func (c ServeConfig) listenDisplay() string {
	host := c.Listen
	if host == "" {
		host = "127.0.0.1"
	}
	if c.Port == 0 {
		return host
	}
	if ip := net.ParseIP(host); ip != nil && ip.To4() == nil {
		// IPv6 literal — bracket it for the host:port form.
		return fmt.Sprintf("[%s]:%d", host, c.Port)
	}
	return fmt.Sprintf("%s:%d", host, c.Port)
}

// isLoopback returns true if the given host (which may be a hostname,
// IPv4 literal, or IPv6 literal) resolves to a loopback address.
//
// We treat "unspecified" addresses (0.0.0.0, ::) as non-loopback since
// that's the practical user concern — "I'm exposing this to the world."
func isLoopback(host string) (bool, error) {
	if host == "" {
		return true, nil // default
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsUnspecified() {
			return false, nil
		}
		return ip.IsLoopback(), nil
	}
	// Hostname — resolve it.
	ips, err := net.LookupIP(host)
	if err != nil {
		return false, err
	}
	// Strict: only loopback if every resolved IP is loopback. If any is
	// non-loopback, treat as non-loopback (principle of least surprise).
	for _, ip := range ips {
		if !ip.IsLoopback() {
			return false, nil
		}
	}
	return true, nil
}
