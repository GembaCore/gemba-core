// Package serverconfig resolves the gemba server URL for CLI verbs.
//
// gm-o9t8.1.1.3 — single, documented precedence order for every CLI
// command that talks to a gemba server. Today each CLI verb had its own
// ad-hoc "flag wins; else GEMBA_SERVER; else error" snippet (see the
// old resolveServer in internal/cli/bead_crud.go). That left no room
// for an on-disk default and made it impossible to operate against a
// remote server without typing --server every time.
//
// Resolution precedence (highest to lowest):
//
//  1. The --server flag (or any explicit per-call override).
//  2. The GEMBA_SERVER environment variable.
//  3. server.url in the user's config file:
//     $XDG_CONFIG_HOME/gemba/config.toml (default ~/.config/gemba/config.toml).
//  4. The compiled-in DefaultURL.
//
// DefaultURL is "http://127.0.0.1:7666" — matches the default port the
// embedded `gemba serve` binds (internal/cli/serve.go). The bead spec
// originally proposed 7373 as a placeholder; that would not match a
// running gemba server and was rejected in implementation.
//
// The credentials store (internal/auth/credstore) is a separate
// concern: it stores tokens keyed by server URL. The resolver runs
// first; its output is the credstore lookup key.
package serverconfig

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// DefaultURL is the fallback server URL when nothing else is set.
//
// This is intentionally the loopback default port for `gemba serve`.
// It matches the value baked into the serve command so a local dev
// workflow (one terminal `gemba serve`, another running `gemba bead
// list`) just works with no flags or env vars.
const DefaultURL = "http://127.0.0.1:7666"

// EnvVar is the environment variable name the resolver consults.
const EnvVar = "GEMBA_SERVER"

// Source describes where a resolved URL came from. The CLI prints this
// via `gemba config show` so operators can see why a verb hit a given
// server.
type Source string

const (
	SourceFlag    Source = "flag"
	SourceEnv     Source = "env"
	SourceFile    Source = "file"
	SourceDefault Source = "default"
)

// Config is the on-disk TOML shape. We only read [server].url today
// but the struct is intentionally a nested form so future knobs (e.g.
// [profile], [output]) can land without breaking existing files.
type Config struct {
	Server ServerConfig `toml:"server"`
}

// ServerConfig holds the [server] table.
type ServerConfig struct {
	URL string `toml:"url"`
}

// Result is the verbose form Resolve produces internally. Most callers
// just want the URL string — use Resolve. Callers that need to know
// the source (config-show) use ResolveDetail.
type Result struct {
	URL    string
	Source Source
	// File is the path of the loaded config.toml when Source is
	// SourceFile; empty otherwise.
	File string
}

// Resolve returns the resolved server URL, following the documented
// precedence. A whitespace-only flag value counts as unset.
func Resolve(flag string) (string, error) {
	r, err := ResolveDetail(flag)
	if err != nil {
		return "", err
	}
	return r.URL, nil
}

// ResolveDetail is Resolve plus where the URL came from. Used by
// `gemba config show`.
func ResolveDetail(flag string) (Result, error) {
	if v := strings.TrimSpace(flag); v != "" {
		return Result{URL: v, Source: SourceFlag}, nil
	}
	if v := strings.TrimSpace(os.Getenv(EnvVar)); v != "" {
		return Result{URL: v, Source: SourceEnv}, nil
	}
	cfg, path, err := loadConfigWithPath()
	if err != nil {
		return Result{}, err
	}
	if cfg != nil {
		if v := strings.TrimSpace(cfg.Server.URL); v != "" {
			return Result{URL: v, Source: SourceFile, File: path}, nil
		}
	}
	return Result{URL: DefaultURL, Source: SourceDefault}, nil
}

// LoadConfig reads the user's config.toml, returning an empty *Config
// (not nil) with nil error when the file is missing — a missing
// config file is normal, not an error. Returns an error only when the
// file exists but cannot be read or parsed.
func LoadConfig() (*Config, error) {
	cfg, _, err := loadConfigWithPath()
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return &Config{}, nil
	}
	return cfg, nil
}

// loadConfigWithPath is the internal form that also returns the path
// it ended up reading from (empty when no file was present). Returns
// (nil, "", nil) when the file simply doesn't exist.
func loadConfigWithPath() (*Config, string, error) {
	path, err := ConfigPath()
	if err != nil {
		return nil, "", err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, "", nil
		}
		return nil, path, fmt.Errorf("read %s: %w", path, err)
	}
	var cfg Config
	if err := toml.Unmarshal(b, &cfg); err != nil {
		return nil, path, fmt.Errorf("parse %s: %w", path, err)
	}
	return &cfg, path, nil
}

// ConfigPath returns the absolute path to the gemba config.toml,
// honoring XDG_CONFIG_HOME. It does NOT check that the file exists.
func ConfigPath() (string, error) {
	dir := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("locate home dir: %w", err)
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "gemba", "config.toml"), nil
}
