// TOML loader for `.gemba/code_analysis.toml` (gm-1ak,
// design docs/design/code-analysis.md §Configuration).
//
// The on-disk shape:
//
//	[code_analysis]
//	default_backend = "gitnexus"
//
//	[[repos]]
//	name = "primary"
//	path = "./"
//	reindex_policy = "post_merge"
//
//	[[repos]]
//	name = "sibling"
//	path = "../sibling-project"
//	backend = "gitnexus"     # optional, defaults to default_backend
//
// The design doc shows an alternate `[repos.<name>]` table form;
// we accept the array-of-tables form as canonical since it
// preserves declaration order and lets the loader detect
// duplicate names cleanly. A future loader extension MAY accept
// the table form too.

package codeanalysis

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/BurntSushi/toml"
)

// Config is the parsed bundle of `code_analysis.toml`. Top-
// level CodeAnalysis carries cross-repo defaults; Repos is the
// per-repo list. Backend-specific knobs live in BackendConfig
// (a free-form map keyed by backend name) so adapters can
// extract their own settings without coupling to this package.
type Config struct {
	CodeAnalysis CodeAnalysisSection      `toml:"code_analysis" json:"code_analysis"`
	Repos        []RepoConfig             `toml:"repos" json:"repos"`
	Backend      map[string]BackendConfig `toml:"backend" json:"backend,omitempty"`
}

// CodeAnalysisSection holds top-level cross-cutting settings.
// DefaultBackend is the backend used by any repo that does not
// declare its own; it MUST match a registered backend name.
type CodeAnalysisSection struct {
	DefaultBackend string `toml:"default_backend" json:"default_backend"`
}

// RepoConfig is one row of the `[[repos]]` array. Name is the
// operator-chosen handle (must be unique within the file).
// Path is the working-tree path (absolute or workspace-relative
// — the loader leaves it as written; the caller is responsible
// for resolving). Backend is optional and falls back to
// CodeAnalysis.DefaultBackend.
type RepoConfig struct {
	Name          string        `toml:"name" json:"name"`
	Path          string        `toml:"path" json:"path"`
	Remote        string        `toml:"remote,omitempty" json:"remote,omitempty"`
	Backend       string        `toml:"backend,omitempty" json:"backend,omitempty"`
	ReindexPolicy ReindexPolicy `toml:"reindex_policy,omitempty" json:"reindex_policy,omitempty"`
}

// BackendConfig is the open key/value bag for a single
// backend's tuning ("embeddings = false", "max_repo_size_mb =
// 500"). The loader does NOT validate the contents; each
// backend's factory inspects what it cares about.
type BackendConfig map[string]any

// ToRepoRef projects a parsed RepoConfig to a [RepoRef] for
// passing across the [Provider] interface.
func (r RepoConfig) ToRepoRef() RepoRef {
	return RepoRef{Name: r.Name, Path: r.Path, Remote: r.Remote}
}

// ResolvedBackend returns the backend name to use for this
// repo — its own Backend if set, otherwise the cross-cutting
// default. Empty string means "no default and none declared",
// which Validate flags.
func (r RepoConfig) ResolvedBackend(defaultBackend string) string {
	if r.Backend != "" {
		return r.Backend
	}
	return defaultBackend
}

// LoadConfig reads and parses path. Returns an empty (but
// valid) Config when path does not exist — workspaces opt in
// to code-analysis by authoring the file, and a missing file
// is not an error. Other I/O errors propagate.
func LoadConfig(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &Config{}, nil
		}
		return nil, fmt.Errorf("codeanalysis: read %s: %w", path, err)
	}
	return DecodeConfig(b)
}

// DecodeConfig parses raw TOML bytes into a [Config], applies
// defaults, and validates. Split out from [LoadConfig] so tests
// can exercise the parsing path without touching disk.
func DecodeConfig(raw []byte) (*Config, error) {
	var c Config
	if _, err := toml.Decode(string(raw), &c); err != nil {
		return nil, fmt.Errorf("codeanalysis: decode: %w", err)
	}
	c.applyDefaults()
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

// applyDefaults fills in cross-cutting defaults that the on-
// disk schema lets the operator omit. Currently:
//
//   - default_backend defaults to "gitnexus" when omitted.
//
// Per-repo defaults (backend fallback, etc.) are computed at
// query time via [RepoConfig.ResolvedBackend] rather than
// rewritten into the parsed struct so the round-trip preserves
// the operator's authored values.
func (c *Config) applyDefaults() {
	if c.CodeAnalysis.DefaultBackend == "" {
		c.CodeAnalysis.DefaultBackend = BackendGitNexus
	}
}

// Validate checks the parsed config for the four classes of
// error gm-1ak's loader is responsible for catching:
//
//  1. default_backend names a backend the binary knows about.
//  2. every repo declares a non-empty path.
//  3. repo names are unique within the file.
//  4. each repo's resolved backend is registered, and its
//     reindex_policy (when set) is one of the four canonical
//     values.
//
// Returns the FIRST validation error encountered with enough
// detail to point the operator at the offending line.
func (c *Config) Validate() error {
	if !IsRegistered(c.CodeAnalysis.DefaultBackend) {
		return fmt.Errorf("codeanalysis: default_backend %q: %w",
			c.CodeAnalysis.DefaultBackend, ErrUnknownBackend)
	}

	seen := make(map[string]int, len(c.Repos))
	for i, r := range c.Repos {
		if r.Name == "" {
			return fmt.Errorf("codeanalysis: repos[%d]: name is empty", i)
		}
		if r.Path == "" {
			return fmt.Errorf("codeanalysis: repos[%d] %q: path is empty", i, r.Name)
		}
		if prev, ok := seen[r.Name]; ok {
			return fmt.Errorf(
				"codeanalysis: duplicate repo name %q (entries %d and %d)",
				r.Name, prev, i)
		}
		seen[r.Name] = i

		backend := r.ResolvedBackend(c.CodeAnalysis.DefaultBackend)
		if !IsRegistered(backend) {
			return fmt.Errorf(
				"codeanalysis: repos[%d] %q: backend %q: %w",
				i, r.Name, backend, ErrUnknownBackend)
		}
		if r.ReindexPolicy != "" && !r.ReindexPolicy.IsValid() {
			return fmt.Errorf(
				"codeanalysis: repos[%d] %q: invalid reindex_policy %q "+
					"(want one of post_merge, scheduled, manual, on_demand)",
				i, r.Name, r.ReindexPolicy)
		}
	}
	return nil
}

// RepoByName returns the configured repo with the matching
// name. The bool is false when no such repo is configured.
// Linear scan; the typical config has a handful of repos so
// no map index is warranted.
func (c *Config) RepoByName(name string) (RepoConfig, bool) {
	for _, r := range c.Repos {
		if r.Name == name {
			return r, true
		}
	}
	return RepoConfig{}, false
}
