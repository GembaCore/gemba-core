package linters

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// configRelPath is the workspace-relative location of the QA linters
// config. Centralized so callers and tests agree.
const configRelPath = ".gemba/qa/linters.toml"

// Load reads `.gemba/qa/linters.toml` from workspaceRoot and returns
// the parsed config. A missing file is non-fatal: the zero
// [LintersConfig] (empty slices) is returned and err is nil. This lets
// QA boot cleanly in workspaces that haven't authored a linter catalog
// yet — they simply expose zero suites until one is written.
//
// Other I/O errors (permission denied, malformed TOML, validation
// failure) are propagated.
func Load(workspaceRoot string) (LintersConfig, error) {
	path := filepath.Join(workspaceRoot, configRelPath)
	return LoadFile(path)
}

// LoadFile parses a linters.toml at an explicit path. Same missing-file
// semantics as [Load]; split out so tests can point at a tempdir
// without synthesizing a nested .gemba/qa tree.
func LoadFile(path string) (LintersConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return LintersConfig{Linters: []Linter{}, Style: []Style{}}, nil
		}
		return LintersConfig{}, fmt.Errorf("linters: read %s: %w", path, err)
	}
	return decode(path, raw)
}

// decode parses TOML bytes into a LintersConfig and validates each
// entry. Split out so a future in-memory loader can share validation.
func decode(path string, raw []byte) (LintersConfig, error) {
	var cfg LintersConfig
	if _, err := toml.Decode(string(raw), &cfg); err != nil {
		return LintersConfig{}, fmt.Errorf("linters: decode %s: %w", path, err)
	}

	// Normalize nil → empty so downstream ranges are clean.
	if cfg.Linters == nil {
		cfg.Linters = []Linter{}
	}
	if cfg.Style == nil {
		cfg.Style = []Style{}
	}

	for i := range cfg.Linters {
		if err := validateLinter(&cfg.Linters[i]); err != nil {
			return LintersConfig{}, fmt.Errorf(
				"linters: %s: linters[%d]: %w", path, i, err)
		}
	}
	for i := range cfg.Style {
		if err := validateStyle(&cfg.Style[i]); err != nil {
			return LintersConfig{}, fmt.Errorf(
				"linters: %s: style[%d]: %w", path, i, err)
		}
	}
	return cfg, nil
}

func validateLinter(l *Linter) error {
	if strings.TrimSpace(l.ID) == "" {
		return errors.New("id is required")
	}
	if strings.TrimSpace(l.Command) == "" {
		return fmt.Errorf("%s: command is required", l.ID)
	}
	if err := l.Depth.Validate(); err != nil {
		return fmt.Errorf("%s: %w", l.ID, err)
	}
	return nil
}

func validateStyle(s *Style) error {
	if strings.TrimSpace(s.ID) == "" {
		return errors.New("id is required")
	}
	if strings.TrimSpace(s.Command) == "" {
		return fmt.Errorf("%s: command is required", s.ID)
	}
	if err := s.Depth.Validate(); err != nil {
		return fmt.Errorf("%s: %w", s.ID, err)
	}
	return nil
}
