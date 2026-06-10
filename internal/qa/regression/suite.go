// Package regression seeds the QA persona's regression-suite registry.
//
// A RegressionSuite is anything QA can run, gate on, and cite as part of
// its quality enforcement: linters, style checks, type-checkers,
// snapshot-style probes. v1 (gm-xst) introduces the interface and
// adapts entries from `.gemba/qa/linters.toml` so each configured
// Linter / Style becomes a suite.
//
// Out of scope for v1: actually executing Command() and capturing
// output — that lands in a follow-up bead together with the QA CLI and
// sling/pre-merge wiring. Today, the package only surfaces the suites
// for inspection and registration.
package regression

import "github.com/GembaCore/gemba-core/internal/qa/linters"

// RegressionSuite is the minimal contract every QA-runnable check
// satisfies. Kept deliberately small so future suite types (custom Go
// suites, snapshot probes, contract checks) can implement it without
// importing the linters package.
//
// Implementations must be safe for concurrent reads — QA may iterate
// the registry from multiple goroutines (planner, scheduler, view).
type RegressionSuite interface {
	// ID is the workspace-unique identifier the suite is cited by in
	// QA reports and gate decisions.
	ID() string
	// Scope is a list of glob patterns the suite applies to. Empty
	// means "the suite's command decides what to look at".
	Scope() []string
	// Command is the shell command QA invokes to run the suite. v1
	// surfaces this for inspection only; execution is a follow-up.
	Command() string
	// Depth is "fast" (sling-time) or "slow" (pre-merge / regression).
	// Surfaced as a string rather than the linters.Depth enum so future
	// suite types are not forced to depend on that package.
	Depth() string
}

// Kind names where a suite came from. Surfaced via [SourcedSuite] so
// the QA UI can group "Linters" and "Style" sections without having to
// re-parse the underlying TOML.
type Kind string

const (
	KindLinter Kind = "linter"
	KindStyle  Kind = "style"
)

// SourcedSuite extends RegressionSuite with provenance — which TOML
// section the suite came from and which QA phases it gates. Suites
// produced by [LoadSuitesFromConfig] satisfy this richer interface; the
// base RegressionSuite stays minimal for hand-rolled suite types that
// don't have a config-file origin.
type SourcedSuite interface {
	RegressionSuite
	Kind() Kind
	GatePhases() []string
}

// linterSuite adapts a [linters.Linter] to the RegressionSuite /
// SourcedSuite contracts.
type linterSuite struct{ l linters.Linter }

func (s linterSuite) ID() string           { return s.l.ID }
func (s linterSuite) Scope() []string      { return s.l.Scope }
func (s linterSuite) Command() string      { return s.l.Command }
func (s linterSuite) Depth() string        { return string(s.l.Depth) }
func (s linterSuite) Kind() Kind           { return KindLinter }
func (s linterSuite) GatePhases() []string { return s.l.GatePhases }

// styleSuite adapts a [linters.Style] to the RegressionSuite /
// SourcedSuite contracts.
type styleSuite struct{ s linters.Style }

func (s styleSuite) ID() string           { return s.s.ID }
func (s styleSuite) Scope() []string      { return s.s.Scope }
func (s styleSuite) Command() string      { return s.s.Command }
func (s styleSuite) Depth() string        { return string(s.s.Depth) }
func (s styleSuite) Kind() Kind           { return KindStyle }
func (s styleSuite) GatePhases() []string { return s.s.GatePhases }

// LoadSuitesFromConfig converts a parsed linters config into the
// regression suites QA can iterate. Linters appear before styles to
// keep ordering deterministic; within each section the original TOML
// order is preserved.
//
// The returned slice is always non-nil (possibly empty) so callers can
// range without nil-checks.
func LoadSuitesFromConfig(cfg linters.LintersConfig) []RegressionSuite {
	out := make([]RegressionSuite, 0, len(cfg.Linters)+len(cfg.Style))
	for _, l := range cfg.Linters {
		out = append(out, linterSuite{l: l})
	}
	for _, s := range cfg.Style {
		out = append(out, styleSuite{s: s})
	}
	return out
}
