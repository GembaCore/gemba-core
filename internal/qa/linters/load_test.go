package linters

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fullTOML = `
[[linters]]
id = "golangci-lint"
scope = ["**/*.go"]
command = "golangci-lint run --out-format=json"
depth = "fast"
gate_phases = []

[[linters]]
id = "eslint-frontend"
scope = ["web/src/**/*.{ts,tsx}"]
command = "pnpm eslint . --format json"
depth = "fast"

[[style]]
id = "vocabulary-drift"
scope = ["docs/**/*.md", "web/src/**/*.tsx"]
command = "gemba qa vocabulary-check --ratified-from docs/ui-spec.md"
depth = "slow"
gate_phases = ["ui_component_completion"]
`

func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func TestLoad_FullConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "linters.toml", fullTOML)

	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if len(cfg.Linters) != 2 {
		t.Fatalf("linters count = %d, want 2", len(cfg.Linters))
	}
	if len(cfg.Style) != 1 {
		t.Fatalf("style count = %d, want 1", len(cfg.Style))
	}

	gl := cfg.Linters[0]
	if gl.ID != "golangci-lint" {
		t.Errorf("linters[0].id = %q, want golangci-lint", gl.ID)
	}
	if gl.Command != "golangci-lint run --out-format=json" {
		t.Errorf("linters[0].command = %q", gl.Command)
	}
	if gl.Depth != DepthFast {
		t.Errorf("linters[0].depth = %q, want fast", gl.Depth)
	}
	if len(gl.Scope) != 1 || gl.Scope[0] != "**/*.go" {
		t.Errorf("linters[0].scope = %v", gl.Scope)
	}
	if len(gl.GatePhases) != 0 {
		t.Errorf("linters[0].gate_phases = %v, want empty", gl.GatePhases)
	}

	es := cfg.Linters[1]
	if es.ID != "eslint-frontend" {
		t.Errorf("linters[1].id = %q", es.ID)
	}

	vd := cfg.Style[0]
	if vd.ID != "vocabulary-drift" {
		t.Errorf("style[0].id = %q", vd.ID)
	}
	if vd.Depth != DepthSlow {
		t.Errorf("style[0].depth = %q, want slow", vd.Depth)
	}
	if len(vd.Scope) != 2 {
		t.Errorf("style[0].scope = %v, want 2 entries", vd.Scope)
	}
	if len(vd.GatePhases) != 1 || vd.GatePhases[0] != "ui_component_completion" {
		t.Errorf("style[0].gate_phases = %v", vd.GatePhases)
	}
}

func TestLoad_MissingFileReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(dir) // .gemba/qa/linters.toml does not exist
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}
	if cfg.Linters == nil {
		t.Errorf("Linters = nil, want empty slice")
	}
	if cfg.Style == nil {
		t.Errorf("Style = nil, want empty slice")
	}
	if len(cfg.Linters) != 0 || len(cfg.Style) != 0 {
		t.Errorf("expected zero entries, got %+v", cfg)
	}
}

func TestLoad_LoadFromWorkspaceRoot(t *testing.T) {
	dir := t.TempDir()
	qaDir := filepath.Join(dir, ".gemba", "qa")
	if err := os.MkdirAll(qaDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, qaDir, "linters.toml", fullTOML)

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Linters)+len(cfg.Style) != 3 {
		t.Errorf("expected 3 total entries, got %d", len(cfg.Linters)+len(cfg.Style))
	}
}

func TestLoad_UnknownDepthIsError(t *testing.T) {
	dir := t.TempDir()
	body := `
[[linters]]
id = "x"
command = "echo hi"
depth = "occasional"
`
	path := writeFile(t, dir, "linters.toml", body)
	_, err := LoadFile(path)
	if err == nil {
		t.Fatal("expected error for unknown depth, got nil")
	}
	if !strings.Contains(err.Error(), "depth") {
		t.Errorf("error %q does not mention depth", err)
	}
}

func TestLoad_MissingIDIsError(t *testing.T) {
	dir := t.TempDir()
	body := `
[[linters]]
command = "echo hi"
depth = "fast"
`
	path := writeFile(t, dir, "linters.toml", body)
	_, err := LoadFile(path)
	if err == nil {
		t.Fatal("expected error for missing id, got nil")
	}
	if !strings.Contains(err.Error(), "id") {
		t.Errorf("error %q does not mention id", err)
	}
}

func TestLoad_MissingCommandIsError(t *testing.T) {
	dir := t.TempDir()
	body := `
[[style]]
id = "vocab"
depth = "slow"
`
	path := writeFile(t, dir, "linters.toml", body)
	_, err := LoadFile(path)
	if err == nil {
		t.Fatal("expected error for missing command, got nil")
	}
	if !strings.Contains(err.Error(), "command") {
		t.Errorf("error %q does not mention command", err)
	}
}

func TestLoad_MissingDepthIsError(t *testing.T) {
	dir := t.TempDir()
	body := `
[[linters]]
id = "x"
command = "echo hi"
`
	path := writeFile(t, dir, "linters.toml", body)
	_, err := LoadFile(path)
	if err == nil {
		t.Fatal("expected error for missing depth, got nil")
	}
}

func TestLoad_EmptyFileIsValid(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "linters.toml", "")
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if len(cfg.Linters) != 0 || len(cfg.Style) != 0 {
		t.Errorf("expected empty config, got %+v", cfg)
	}
	if cfg.Linters == nil || cfg.Style == nil {
		t.Errorf("expected non-nil empty slices, got %+v", cfg)
	}
}
