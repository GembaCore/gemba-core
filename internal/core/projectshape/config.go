package projectshape

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
)

// ConfigFileName is the relative path (under the workspace's
// `.gemba/` directory) where operators pin the active shape.
const ConfigFileName = "project_shape.toml"

// configFile mirrors the on-disk shape of `.gemba/project_shape.toml`:
//
//	[project_shape]
//	active = "gemba"
//
// The block-and-key form (rather than a top-level key) keeps room
// for future fields without breaking backward compat — e.g. a later
// bead may add `[project_shape] custom = "path/to/local-shape.toml"`
// for operator-authored shapes living outside the seed registry.
type configFile struct {
	ProjectShape struct {
		Active string `toml:"active"`
	} `toml:"project_shape"`
}

// LoadConfig reads `.gemba/project_shape.toml` at path and resolves
// the named shape via the supplied registry. Behaviour:
//
//   - File missing → returns a zero-value Shape and a nil error. The
//     dispatcher's Render() then contributes nothing to the project-
//     values layer, which is the gm-4uso "no shape configured"
//     fallback.
//   - File present but [project_shape] block absent or `active`
//     empty → returns zero-value Shape, nil error (treat as "no
//     shape pinned").
//   - File present, `active` set, and the registry has no such
//     shape → returns a wrapped [ErrUnknownShape] so the operator
//     sees a clean error instead of silent fallback to generic.
//   - Malformed TOML → wrapped error naming the path.
//
// path MUST be the full path to the file (typically
// filepath.Join(workspaceDir, ".gemba", ConfigFileName)). The caller
// owns workspace-dir resolution; this loader is path-pure for ease
// of testing.
func LoadConfig(path string, reg *Registry) (Shape, error) {
	if reg == nil {
		return Shape{}, errors.New("projectshape: LoadConfig: registry is nil")
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// No file means no shape pinned. Generic fallback.
			return Shape{}, nil
		}
		return Shape{}, fmt.Errorf("projectshape: read %s: %w", path, err)
	}

	var cfg configFile
	if _, err := toml.Decode(string(raw), &cfg); err != nil {
		return Shape{}, fmt.Errorf("projectshape: decode %s: %w", path, err)
	}

	name := strings.TrimSpace(cfg.ProjectShape.Active)
	if name == "" {
		// File exists but is empty / missing the active key. Treat
		// the same as no file: generic fallback. An operator who
		// wants to disable injection but keep the file around can
		// leave `active = ""`.
		return Shape{}, nil
	}

	shape, err := reg.Resolve(name)
	if err != nil {
		return Shape{}, fmt.Errorf("projectshape: %s: %w", path, err)
	}
	return shape, nil
}
