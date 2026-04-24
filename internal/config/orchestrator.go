// Orchestrator config loader (gm-root.4). Reads the JSON file at
// .gemba/orchestrator.json (or the path passed via
// --orchestrator-config) so gemba serve knows which Shader to wrap
// the WorkPlane adaptor in.
//
// Two-stage decode: the top-level `orchestrator` field selects the
// shader implementation; the nested `config` object is held as
// json.RawMessage and unmarshalled into the implementation's own
// Config struct (e.g. gastown.Config). Keeps each shader's wire
// schema where the shader lives.

package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// DefaultOrchestratorConfigPath is where serve looks when
// --orchestrator-config isn't passed. Relative paths are resolved
// against gemba serve's working directory (typically the rig root).
const DefaultOrchestratorConfigPath = ".gemba/orchestrator.json"

// OrchestratorConfig is the file's top-level shape.
type OrchestratorConfig struct {
	// Orchestrator picks the Shader implementation. Known values:
	// "gastown". Unknown values cause LoadOrchestratorConfig to
	// return an error so a typo doesn't silently fall back to NopShader.
	Orchestrator string `json:"orchestrator"`
	// Config is the implementation-specific config blob. Each shader
	// owns its own Config struct in its package.
	Config json.RawMessage `json:"config"`
}

// GastownShaderConfig mirrors gastown.Config on the wire. Lives in
// this package (not internal/shader/gastown) so the loader doesn't
// need to import the shader package — keeps config a leaf module.
type GastownShaderConfig struct {
	Rig          string            `json:"rig"`
	RigAbbr      string            `json:"rig_abbr"`
	IDFormat     string            `json:"id_format,omitempty"`
	TitleFormat  string            `json:"title_format,omitempty"`
	KindPrefixes map[string]string `json:"kind_prefixes,omitempty"`
}

// LoadOrchestratorConfig reads + parses path. Returns os.ErrNotExist
// when the file is missing — callers SHOULD check for that and treat
// it as "no shader configured" (NopShader takes over). Any other
// error is structural and should fail startup.
func LoadOrchestratorConfig(path string) (*OrchestratorConfig, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var oc OrchestratorConfig
	if err := json.Unmarshal(body, &oc); err != nil {
		return nil, fmt.Errorf("orchestrator config %s: %w", path, err)
	}
	if oc.Orchestrator == "" {
		return nil, fmt.Errorf("orchestrator config %s: missing required field 'orchestrator'", path)
	}
	return &oc, nil
}

// ResolveOrchestratorConfigPath returns the absolute path to the
// orchestrator config. When override is empty, falls back to the
// default location (./.gemba/orchestrator.json) anchored at workdir.
// Returns "" when the file doesn't exist — the caller treats that as
// "no shader configured".
func ResolveOrchestratorConfigPath(override, workdir string) string {
	candidate := override
	if candidate == "" {
		candidate = filepath.Join(workdir, DefaultOrchestratorConfigPath)
	} else if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(workdir, candidate)
	}
	if _, err := os.Stat(candidate); err != nil {
		return ""
	}
	return candidate
}
