package regression

import (
	"testing"

	"github.com/MikeBengtson/gemba/internal/qa/linters"
)

func sampleConfig() linters.LintersConfig {
	return linters.LintersConfig{
		Linters: []linters.Linter{
			{
				ID:         "golangci-lint",
				Scope:      []string{"**/*.go"},
				Command:    "golangci-lint run --out-format=json",
				Depth:      linters.DepthFast,
				GatePhases: []string{},
			},
			{
				ID:      "eslint-frontend",
				Scope:   []string{"web/src/**/*.{ts,tsx}"},
				Command: "pnpm eslint . --format json",
				Depth:   linters.DepthFast,
			},
		},
		Style: []linters.Style{
			{
				ID:         "vocabulary-drift",
				Scope:      []string{"docs/**/*.md", "web/src/**/*.tsx"},
				Command:    "gemba qa vocabulary-check --ratified-from docs/ui-spec.md",
				Depth:      linters.DepthSlow,
				GatePhases: []string{"ui_component_completion"},
			},
		},
	}
}

func TestLoadSuitesFromConfig_ProducesNPlusM(t *testing.T) {
	cfg := sampleConfig()
	suites := LoadSuitesFromConfig(cfg)
	want := len(cfg.Linters) + len(cfg.Style)
	if len(suites) != want {
		t.Fatalf("suite count = %d, want %d (N=%d linters + M=%d styles)",
			len(suites), want, len(cfg.Linters), len(cfg.Style))
	}
}

func TestLoadSuitesFromConfig_EmptyIsNonNil(t *testing.T) {
	suites := LoadSuitesFromConfig(linters.LintersConfig{})
	if suites == nil {
		t.Fatal("expected non-nil slice, got nil")
	}
	if len(suites) != 0 {
		t.Errorf("expected 0 suites, got %d", len(suites))
	}
}

func TestLoadSuitesFromConfig_PreservesFieldsAndOrder(t *testing.T) {
	cfg := sampleConfig()
	suites := LoadSuitesFromConfig(cfg)

	// Linters first, in TOML order.
	if got, want := suites[0].ID(), "golangci-lint"; got != want {
		t.Errorf("suites[0].ID() = %q, want %q", got, want)
	}
	if got, want := suites[1].ID(), "eslint-frontend"; got != want {
		t.Errorf("suites[1].ID() = %q, want %q", got, want)
	}
	// Style last.
	if got, want := suites[2].ID(), "vocabulary-drift"; got != want {
		t.Errorf("suites[2].ID() = %q, want %q", got, want)
	}

	gl := suites[0]
	if gl.Command() != "golangci-lint run --out-format=json" {
		t.Errorf("command preserved = %q", gl.Command())
	}
	if gl.Depth() != "fast" {
		t.Errorf("depth = %q, want fast", gl.Depth())
	}
	if len(gl.Scope()) != 1 || gl.Scope()[0] != "**/*.go" {
		t.Errorf("scope = %v", gl.Scope())
	}

	vd := suites[2]
	if vd.Depth() != "slow" {
		t.Errorf("style depth = %q, want slow", vd.Depth())
	}
	if len(vd.Scope()) != 2 {
		t.Errorf("style scope = %v, want 2 entries", vd.Scope())
	}
}

func TestLoadSuitesFromConfig_SourcedSuiteProvenance(t *testing.T) {
	suites := LoadSuitesFromConfig(sampleConfig())

	gl, ok := suites[0].(SourcedSuite)
	if !ok {
		t.Fatalf("suites[0] (%T) does not implement SourcedSuite", suites[0])
	}
	if gl.Kind() != KindLinter {
		t.Errorf("kind = %q, want %q", gl.Kind(), KindLinter)
	}
	if len(gl.GatePhases()) != 0 {
		t.Errorf("gate_phases = %v, want empty", gl.GatePhases())
	}

	vd, ok := suites[2].(SourcedSuite)
	if !ok {
		t.Fatalf("suites[2] (%T) does not implement SourcedSuite", suites[2])
	}
	if vd.Kind() != KindStyle {
		t.Errorf("kind = %q, want %q", vd.Kind(), KindStyle)
	}
	if len(vd.GatePhases()) != 1 || vd.GatePhases()[0] != "ui_component_completion" {
		t.Errorf("gate_phases = %v", vd.GatePhases())
	}
}
