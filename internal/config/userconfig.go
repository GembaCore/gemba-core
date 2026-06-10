// Package config: see doc.go for the overview.
//
// userconfig.go resolves the user-level ~/.gemba/config.toml used by
// gemba serve's cold-start detection (gm-root.17.4) and Beads URL
// resolution (gm-root.19). The config file is optional; a missing file
// silently applies built-in defaults so an operator who just installed
// gemba gets the right behaviour without touching any TOML.

package config

import (
	"errors"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// DefaultProjectsDir is the built-in fallback location for new project
// directories when [projects].default_dir is absent from config.toml.
// The directory is not required to exist at probe time — it is created
// on first project ratification.
const DefaultProjectsDir = "gemba/projects"

// DefaultBeadsURL is the built-in fallback Beads server URL when
// neither --dolt-url nor [beads].url in config.toml is set. It
// matches the default Dolt server address used by `bd` and Gas Town's
// `gt dolt start` (port 3307, root user, gemba database).
const DefaultBeadsURL = "mysql://root@127.0.0.1:3307/gemba"

// UserConfig mirrors the shape of ~/.gemba/config.toml.
// Only the fields relevant to startup-time decisions live here; the
// file may contain additional keys that are silently ignored.
type UserConfig struct {
	Projects ProjectsConfig `toml:"projects"`
	LLM      LLMConfig      `toml:"llm"`
	Beads    BeadsConfig    `toml:"beads"`
	Metrics  MetricsConfig  `toml:"metrics"`
}

// MetricsConfig holds the [metrics] table from config.toml — the
// upstream Prometheus URL the /api/v1/metrics/series proxy queries
// (gm-e9m0; ratified under gm-sf51, "Path A" — Prometheus proxy).
//
// When PromURL is empty the time-series endpoint returns a
// 503 KindAdaptorDegraded explaining that the operator must either
// set PROM_URL in the environment or [metrics].prom_url in this
// config file. The Insights panel renders an "awaiting Prometheus"
// stub off that response.
type MetricsConfig struct {
	// PromURL is the base URL of a Prometheus instance scraping
	// gemba's /metrics endpoint, e.g. "http://prometheus:9090".
	// The proxy appends /api/v1/query_range to this base. Empty
	// string means "no Prometheus configured."
	PromURL string `toml:"prom_url"`
}

// BeadsConfig holds the [beads] table from config.toml — the Beads
// server connection settings used by gemba serve when --dolt-url is
// not passed on the command line (gm-root.19).
type BeadsConfig struct {
	// URL is a mysql://user[:pass]@host:port/dbname connection string
	// for the Dolt server hosting beads databases. When set it takes
	// precedence over the built-in DefaultBeadsURL. Empty means "fall
	// back to DefaultBeadsURL." The URL is consumed only when --dolt-url
	// is absent from the CLI — an explicit flag always wins.
	URL string `toml:"url"`
}

// ProjectsConfig holds the [projects] table from config.toml.
type ProjectsConfig struct {
	// DefaultDir is the parent directory under which new projects are
	// created and under which gemba scans for existing projects at
	// cold-start. A relative path is resolved against the user's home
	// directory. Empty string means "use the built-in default"
	// (~/gemba/projects/).
	DefaultDir string `toml:"default_dir"`

	// ExtraRoots are additional directories whose immediate children
	// are scanned for projects (gm-root.17.14). Each entry behaves like
	// DefaultDir at scan time: every direct child that contains a beads
	// DB or a .gemba/workspace.toml is reported. Tilde-prefixed paths
	// are expanded; relative paths are taken as-is (filepath.Clean
	// applied) so callers can point at fully-qualified locations like
	// "/srv/team-projects". Missing or unreadable roots produce a log
	// warning at scan time but do not fail the request.
	ExtraRoots []string `toml:"extra_roots"`
}

// LLMConfig holds the [llm] table from config.toml — the shared
// "default chat client" credentials referenced by docs/design/
// newproject.md §"Credential resolution". Today consumed only by the
// Onboarder persona (gm-root.17.10); future agents that want an
// in-process LLM client read the same table.
type LLMConfig struct {
	// Provider names the chat backend. Today the only recognized
	// value is "anthropic"; an empty string means "no LLM client
	// configured" and consumers fail closed with a clear diagnostic.
	// Open string on purpose so a future provider doesn't break the
	// loader on machines that haven't upgraded.
	Provider string `toml:"provider"`
	// APIKey is the credential. When empty, consumers MAY fall back
	// to a provider-specific environment variable (e.g.
	// ANTHROPIC_API_KEY) — that fallback is the consumer's choice,
	// not the loader's.
	APIKey string `toml:"api_key"`
	// Model selects the model identifier (e.g. claude-3-5-sonnet-...).
	// Empty means "let the consumer pick a default".
	Model string `toml:"model"`
	// Endpoint optionally overrides the provider's default API URL.
	// Empty means "use the provider's default endpoint".
	Endpoint string `toml:"endpoint"`
}

// LoadUserConfig reads ~/.gemba/config.toml (or the path pointed to by
// override if non-empty) and returns the parsed config. A missing file
// is not an error — callers receive a zero-value UserConfig and can
// apply their own defaults. Any other I/O or parse error is returned
// so the caller can decide how to surface it.
func LoadUserConfig(override string) (UserConfig, error) {
	path := override
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return UserConfig{}, err
		}
		path = filepath.Join(home, ".gemba", "config.toml")
	}

	var cfg UserConfig
	_, err := toml.DecodeFile(path, &cfg)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return UserConfig{}, nil
		}
		return UserConfig{}, err
	}
	return cfg, nil
}

// ResolvePromURL returns the Prometheus base URL the
// /api/v1/metrics/series proxy should query, applying the resolution
// order ratified under gm-sf51:
//
//  1. cliFlag — the --prom-url CLI flag if the operator set it
//     explicitly.
//  2. envVar — the PROM_URL environment variable (callers pass
//     os.Getenv("PROM_URL")).
//  3. cfg.Metrics.PromURL — the [metrics].prom_url entry from
//     ~/.gemba/config.toml.
//  4. "" — no Prometheus configured. The handler surfaces this as
//     a 503 KindAdaptorDegraded so the SPA can render the
//     "awaiting Prometheus" stub.
//
// Unlike ResolveBeadsURL there is no built-in default: an unconfigured
// Prometheus is a legitimate mode (the operator may not have one),
// not a startup error.
func ResolvePromURL(cliFlag, envVar string, cfg UserConfig) string {
	if cliFlag != "" {
		return cliFlag
	}
	if envVar != "" {
		return envVar
	}
	return cfg.Metrics.PromURL
}

// ResolveBeadsURL returns the Beads server URL to use, applying the
// documented resolution order (gm-root.19):
//
//  1. cliFlag — the value of --dolt-url if the operator set it explicitly
//  2. cfg.Beads.URL — the [beads].url entry from ~/.gemba/config.toml
//  3. DefaultBeadsURL — the built-in fallback (mysql://root@127.0.0.1:3307/gemba)
//
// The returned string is always non-empty. Callers that need to
// distinguish "came from CLI" from "came from config/default" should
// compare the return value against cliFlag themselves.
func ResolveBeadsURL(cliFlag string, cfg UserConfig) string {
	if cliFlag != "" {
		return cliFlag
	}
	if cfg.Beads.URL != "" {
		return cfg.Beads.URL
	}
	return DefaultBeadsURL
}

// RedactBeadsURL strips the password component from a mysql:// URL
// before it enters a log stream. If the URL has no password or cannot
// be parsed, the original string is returned unchanged.
func RedactBeadsURL(raw string) string {
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

// ResolveDefaultDir returns the absolute path of the projects default
// directory, applying the resolution order from the design doc:
//  1. UserConfig.Projects.DefaultDir if set (relative → home-relative).
//  2. ~/gemba/projects/ (built-in default).
//
// The returned path may not exist yet; callers that need the directory
// to be present must create it themselves.
func ResolveDefaultDir(cfg UserConfig) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	raw := cfg.Projects.DefaultDir
	if raw == "" {
		return filepath.Join(home, DefaultProjectsDir), nil
	}
	if filepath.IsAbs(raw) {
		return raw, nil
	}
	return filepath.Join(home, raw), nil
}

// HasProjectUnder reports whether defaultDir contains at least one
// project directory — i.e. a direct child directory that contains a
// .gemba/workspace.toml file. A missing or empty defaultDir is treated
// as "no projects" (returns false, nil).
func HasProjectUnder(defaultDir string) (bool, error) {
	projects, err := ListProjectsUnder(defaultDir)
	if err != nil {
		return false, err
	}
	return len(projects) > 0, nil
}

// ProjectKind classifies a discovered project entry by its setup
// completeness (gm-root.17.14). The picker UI dispatches on this to
// render either an "open" affordance (KindComplete) or a
// "finish setup" affordance (KindNeedsWorkspace, KindNeedsRepo).
type ProjectKind string

const (
	// KindComplete: the directory has both a beads DB (a `.beads/`
	// subdir, by convention) AND a `.gemba/workspace.toml`. The picker
	// can open this project directly.
	KindComplete ProjectKind = "complete"

	// KindNeedsWorkspace: the directory has a beads DB but no
	// `.gemba/workspace.toml`. The picker invites the operator to
	// finish setup via /v1/projects/bind.
	KindNeedsWorkspace ProjectKind = "needs_workspace"

	// KindNeedsRepo: the directory has a beads DB AND a
	// `.gemba/workspace.toml` but is not a git repo (no `.git/`). The
	// picker invites the operator to bind to an existing repo or run
	// `git init` via /v1/projects/bind.
	KindNeedsRepo ProjectKind = "needs_repo"
)

// ProjectEntry is one discovered project — a direct child of one of
// the configured discovery roots that has either a beads DB or a
// .gemba/workspace.toml.
type ProjectEntry struct {
	// Name is the directory basename — the project's human-readable
	// identifier (e.g. "my-project").
	Name string
	// Path is the absolute path to the project root (the directory that
	// contains .gemba/workspace.toml or a .beads/ DB).
	Path string
	// Kind classifies the entry's setup state. See ProjectKind for the
	// three values.
	Kind ProjectKind
}

// hasBeadsDB reports whether dir contains a beads database. The
// convention is a `.beads/` subdirectory (created by `bd init`); the
// directory's contents are not inspected — a stub `.beads/redirect`
// counts, matching how rigs symlink databases across worktrees.
func hasBeadsDB(dir string) bool {
	beadsDot := filepath.Join(dir, ".beads")
	if info, err := os.Stat(beadsDot); err == nil && info.IsDir() {
		return true
	}
	return false
}

// hasWorkspaceTOML reports whether dir contains .gemba/workspace.toml.
func hasWorkspaceTOML(dir string) bool {
	wsToml := filepath.Join(dir, ".gemba", "workspace.toml")
	_, err := os.Stat(wsToml)
	return err == nil
}

// hasGitRepo reports whether dir is the root of a git working tree.
// A `.git` may be either a directory (normal repo) or a file (git
// worktree pointer); both count.
func hasGitRepo(dir string) bool {
	gitDot := filepath.Join(dir, ".git")
	_, err := os.Stat(gitDot)
	return err == nil
}

// ClassifyProject returns the ProjectKind for dir, or "" if dir does
// not look like any kind of project (no beads DB and no
// .gemba/workspace.toml). Callers MUST check for "" before treating
// the result as a project.
func ClassifyProject(dir string) ProjectKind {
	beads := hasBeadsDB(dir)
	ws := hasWorkspaceTOML(dir)
	if !beads && !ws {
		return ""
	}
	if beads && !ws {
		return KindNeedsWorkspace
	}
	// At this point: ws is true; beads may or may not be — historically
	// (gm-root.18) we only checked workspace.toml. Treat ws+!repo as
	// KindNeedsRepo regardless of whether beads exists, since a
	// workspace.toml without a repo is the "partially-bootstrapped"
	// signal the picker wants to surface.
	if !hasGitRepo(dir) {
		return KindNeedsRepo
	}
	return KindComplete
}

// ListProjectsUnder enumerates projects under defaultDir. A project is
// a direct child directory whose ClassifyProject returns a non-empty
// kind — i.e. it contains a beads DB and/or a .gemba/workspace.toml.
// Results are returned in the order ReadDir provides them (alphabetical
// by name on most filesystems). A missing or empty defaultDir returns
// an empty slice and nil error. Any other I/O error is returned so the
// caller can decide how to surface it.
//
// This is the shared scanning primitive used by both HasProjectUnder
// (cold-start gate, gm-root.17.4) and the /api/v1/projects list
// endpoint (gm-root.18 / gm-root.17.14).
func ListProjectsUnder(defaultDir string) ([]ProjectEntry, error) {
	if defaultDir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(defaultDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var projects []ProjectEntry
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		absPath := filepath.Join(defaultDir, e.Name())
		kind := ClassifyProject(absPath)
		if kind == "" {
			continue
		}
		projects = append(projects, ProjectEntry{
			Name: e.Name(),
			Path: absPath,
			Kind: kind,
		})
	}
	return projects, nil
}

// ExpandRoot tilde-expands and cleans a single root path. A leading
// "~" is replaced with the user's home directory; relative paths are
// returned cleaned but otherwise untouched (callers that need
// absolute resolution can wrap with filepath.Abs).
func ExpandRoot(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	if trimmed == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return home, nil
	}
	if strings.HasPrefix(trimmed, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Clean(filepath.Join(home, trimmed[2:])), nil
	}
	return filepath.Clean(trimmed), nil
}

// ListAllProjects scans defaultDir + every entry in extraRoots,
// classifies each direct-child directory, and returns the deduped
// union (gm-root.17.14). Order: defaultDir results first, then
// each extra_root in order. Dedup key is the canonicalized absolute
// path; ties prefer the first-seen entry.
//
// Missing or unreadable roots are NOT fatal — they produce a slog
// warning and the scan continues. Only the defaultDir itself surfaces
// an error to the caller (via ListProjectsUnder), preserving the
// existing /api/v1/projects contract.
func ListAllProjects(defaultDir string, extraRoots []string) ([]ProjectEntry, error) {
	primary, err := ListProjectsUnder(defaultDir)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(primary))
	out := make([]ProjectEntry, 0, len(primary))
	for _, p := range primary {
		key := canonPath(p.Path)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, p)
	}

	for _, raw := range extraRoots {
		root, err := ExpandRoot(raw)
		if err != nil {
			slog.Warn("projects: extra_root expansion failed",
				"root", raw, "err", err)
			continue
		}
		if root == "" {
			continue
		}
		entries, err := ListProjectsUnder(root)
		if err != nil {
			slog.Warn("projects: extra_root scan failed",
				"root", root, "err", err)
			continue
		}
		for _, p := range entries {
			key := canonPath(p.Path)
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, p)
		}
	}
	return out, nil
}

// canonPath returns the canonical absolute form of p for dedup keys.
// Falls back to the cleaned input when EvalSymlinks fails (e.g., the
// path doesn't exist anymore between scans).
func canonPath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		abs = filepath.Clean(p)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return abs
	}
	return resolved
}
