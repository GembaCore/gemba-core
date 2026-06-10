// Package linters models the QA persona's configurable lint and style
// catalog. The on-disk source of truth is `.gemba/qa/linters.toml` in a
// workspace root; each `[[linters]]` and `[[style]]` entry becomes a
// regression-suite member that QA can iterate, gate on, and cite.
//
// v1 (gm-xst) covers loading + validation only; the subprocess execution
// of the configured commands is a follow-up. See gm-xst for scope.
package linters

import "fmt"

// Depth classifies when a suite member runs. "fast" is sling-time (every
// commit / preview); "slow" is pre-merge / regression-time. Both values
// are surfaced verbatim to the QA scheduler — this package does not map
// them to a wall-clock budget.
type Depth string

const (
	// DepthFast — runs on every sling. Linters default here.
	DepthFast Depth = "fast"
	// DepthSlow — runs at pre-merge / regression time. Style configs
	// default here in the spec but the field is required on the wire so
	// authors stay explicit.
	DepthSlow Depth = "slow"
)

// Validate accepts only the documented values. The empty string is
// rejected — load.go normalizes / requires this field per entry.
func (d Depth) Validate() error {
	switch d {
	case DepthFast, DepthSlow:
		return nil
	default:
		return fmt.Errorf("unknown depth %q (want %q or %q)",
			string(d), DepthFast, DepthSlow)
	}
}

// Linter is one entry under `[[linters]]` in linters.toml. Linters are
// general code-correctness tools (golangci-lint, eslint, …) that QA runs
// at fast depth on every sling.
type Linter struct {
	// ID is a stable, workspace-unique identifier (e.g. "golangci-lint").
	// Required.
	ID string `toml:"id" json:"id"`

	// Scope is a list of glob patterns naming the files this linter
	// covers. Optional; an empty slice means "everything the command
	// itself decides to look at".
	Scope []string `toml:"scope,omitempty" json:"scope,omitempty"`

	// Command is the shell command QA invokes. Captured as a single
	// string so authors can express args naturally; v1 does not parse
	// or execute it. Required.
	Command string `toml:"command" json:"command"`

	// Depth is "fast" or "slow". Required.
	Depth Depth `toml:"depth" json:"depth"`

	// GatePhases names QA phases this linter blocks. Optional; an empty
	// slice means "advisory — never blocks a phase".
	GatePhases []string `toml:"gate_phases,omitempty" json:"gate_phases,omitempty"`
}

// Style is one entry under `[[style]]` in linters.toml. Style configs
// enforce house-style rules (vocabulary drift, prose conventions) and
// typically run at slow depth pre-merge, optionally gating named QA
// phases.
type Style struct {
	ID         string   `toml:"id" json:"id"`
	Scope      []string `toml:"scope,omitempty" json:"scope,omitempty"`
	Command    string   `toml:"command" json:"command"`
	Depth      Depth    `toml:"depth" json:"depth"`
	GatePhases []string `toml:"gate_phases,omitempty" json:"gate_phases,omitempty"`
}

// LintersConfig is the parsed shape of `.gemba/qa/linters.toml`. Both
// slices are non-nil after Load (possibly empty) so callers can range
// without nil-checks.
type LintersConfig struct {
	Linters []Linter `toml:"linters" json:"linters"`
	Style   []Style  `toml:"style" json:"style"`
}
