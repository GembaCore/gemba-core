// Package config: see doc.go for the overview.
//
// userconfig.go resolves the user-level ~/.gemba/config.toml used by
// gemba serve's cold-start detection (gm-root.17.4). The config file is
// optional; a missing file silently applies built-in defaults so an
// operator who just installed gemba gets the right behaviour without
// touching any TOML.

package config

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// DefaultProjectsDir is the built-in fallback location for new project
// directories when [projects].default_dir is absent from config.toml.
// The directory is not required to exist at probe time — it is created
// on first project ratification.
const DefaultProjectsDir = "gemba/projects"

// UserConfig mirrors the shape of ~/.gemba/config.toml.
// Only the fields relevant to startup-time decisions live here; the
// file may contain additional keys that are silently ignored.
type UserConfig struct {
	Projects ProjectsConfig `toml:"projects"`
}

// ProjectsConfig holds the [projects] table from config.toml.
type ProjectsConfig struct {
	// DefaultDir is the parent directory under which new projects are
	// created and under which gemba scans for existing projects at
	// cold-start. A relative path is resolved against the user's home
	// directory. Empty string means "use the built-in default"
	// (~/gemba/projects/).
	DefaultDir string `toml:"default_dir"`
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

// ProjectEntry is one discovered project under the default_dir.
type ProjectEntry struct {
	// Name is the directory basename — the project's human-readable
	// identifier (e.g. "my-project").
	Name string
	// Path is the absolute path to the project root (the directory that
	// contains .gemba/workspace.toml).
	Path string
}

// ListProjectsUnder enumerates projects under defaultDir. A project is
// a direct child directory that contains a .gemba/workspace.toml file.
// Results are returned in the order ReadDir provides them (alphabetical
// by name on most filesystems). A missing or empty defaultDir returns
// an empty slice and nil error. Any other I/O error is returned so the
// caller can decide how to surface it.
//
// This is the shared scanning primitive used by both HasProjectUnder
// (cold-start gate, gm-root.17.4) and the /api/v1/projects list
// endpoint (gm-root.18).
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
		wsToml := filepath.Join(absPath, ".gemba", "workspace.toml")
		if _, err := os.Stat(wsToml); err == nil {
			projects = append(projects, ProjectEntry{
				Name: e.Name(),
				Path: absPath,
			})
		}
	}
	return projects, nil
}
