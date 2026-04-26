// Auto-dispatch policy + gate (gm-s47n.6.5).
//
// The auto-dispatch daemon (gm-s47n.6.3) cannot land safely without
// two guards in front of it:
//
//   1. Rate limit. A bad scorer on a fast loop can push a fleet of
//      agents into a corner of the codebase for hours. Spec §11
//      pins the example as ≤1 bead / session / 5min — the
//      MinIntervalPerSession knob is the in-code expression of
//      that contract.
//   2. Kill switch. Per-rig, runtime-toggleable, opt-in by default.
//      An operator who notices the planner doing the wrong thing
//      flips it once and the daemon stops pulling from the ready
//      set immediately.
//
// This file ships the in-memory primitive + a file-backed loader.
// The daemon checks AllowDispatch() before each pull. A Disabled
// policy short-circuits to "no" regardless of rate-limit state.
//
// Operator surface: gemba dispatch (CLI in dispatch_cli.go inside
// internal/cli) inspects + toggles the policy.

package planner

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// DefaultMinIntervalPerSession matches the spec example (§11).
// Operators tune via the policy file or the CLI.
const DefaultMinIntervalPerSession = 5 * time.Minute

// DefaultMaxConcurrent is a fleet-wide cap on auto-dispatched
// sessions in flight. Holds the planner's blast radius even when
// the per-session rate limit is generous.
const DefaultMaxConcurrent = 4

// DispatchPolicy is the data that gates the auto-dispatch daemon.
// Mutable by the operator at runtime; persisted via PolicyStore.
//
// Wire shape on disk: JSON. Stable field tags so a rig file
// written by gemba 1.0 keeps loading on gemba 2.0.
type DispatchPolicy struct {
	// Enabled is the master kill switch. false → AllowDispatch
	// always returns ("no", "auto-dispatch disabled"). Default
	// false (opt-in per spec §4 Layer 5).
	Enabled bool `json:"enabled"`
	// MinIntervalPerSession is the minimum wall-clock gap between
	// successive auto-dispatches against the same session id.
	// Zero falls back to DefaultMinIntervalPerSession.
	MinIntervalPerSession time.Duration `json:"min_interval_per_session"`
	// MaxConcurrent caps the number of auto-dispatches the gate
	// will allow to be in flight at any instant. Zero falls back
	// to DefaultMaxConcurrent.
	MaxConcurrent int `json:"max_concurrent"`
}

// resolved fills in zero-value fields from defaults so callers
// don't have to remember the fallback rules at every check site.
func (p DispatchPolicy) resolved() DispatchPolicy {
	if p.MinIntervalPerSession <= 0 {
		p.MinIntervalPerSession = DefaultMinIntervalPerSession
	}
	if p.MaxConcurrent <= 0 {
		p.MaxConcurrent = DefaultMaxConcurrent
	}
	return p
}

// DispatchGate is the runtime guard the auto-dispatch loop calls.
// Owns mutable per-session timestamps + the inflight counter.
//
// Concurrency: every method holds a sync.Mutex for the duration of
// its read/write. Auto-dispatch is fundamentally serial (one
// decision at a time per loop tick), so the mutex isn't a hot
// path; correctness over micro-optimisation.
type DispatchGate struct {
	mu             sync.Mutex
	policy         DispatchPolicy
	lastDispatched map[string]time.Time
	inflight       int
}

// NewDispatchGate constructs a gate seeded with the given policy.
// Pass DispatchPolicy{} for the all-defaults flavour (Enabled=false,
// 5-minute interval, 4 concurrent).
func NewDispatchGate(policy DispatchPolicy) *DispatchGate {
	return &DispatchGate{
		policy:         policy,
		lastDispatched: make(map[string]time.Time),
	}
}

// AllowDispatch reports whether the daemon may dispatch against
// sessionID right now. Returns (true, "") when the dispatch is
// permitted; (false, reason) when blocked. The reason string is
// safe to log + surface to the operator and follows a stable
// vocabulary so future tooling can pattern-match on it without
// regex'ing free text:
//
//	"auto-dispatch disabled"
//	"max-concurrent reached"
//	"per-session rate limit"
//
// Time is injected so tests are deterministic.
func (g *DispatchGate) AllowDispatch(sessionID string, now time.Time) (bool, string) {
	if sessionID == "" {
		return false, "empty session id"
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	pol := g.policy.resolved()
	if !pol.Enabled {
		return false, "auto-dispatch disabled"
	}
	if g.inflight >= pol.MaxConcurrent {
		return false, "max-concurrent reached"
	}
	if last, ok := g.lastDispatched[sessionID]; ok {
		if now.Sub(last) < pol.MinIntervalPerSession {
			return false, "per-session rate limit"
		}
	}
	return true, ""
}

// RecordDispatch stamps the dispatch decision so subsequent
// AllowDispatch calls see it. Increments the inflight counter.
// Pair with RecordCompletion when the session's bead finishes.
func (g *DispatchGate) RecordDispatch(sessionID string, now time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.lastDispatched[sessionID] = now
	g.inflight++
}

// RecordCompletion decrements the inflight counter. Safe to call
// when no prior RecordDispatch was issued — clamps at zero so a
// double-completion event doesn't push the counter negative.
func (g *DispatchGate) RecordCompletion(sessionID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.inflight > 0 {
		g.inflight--
	}
}

// Policy returns the current policy snapshot. The returned value
// is a copy — callers may inspect freely without affecting the
// gate's internal state.
func (g *DispatchGate) Policy() DispatchPolicy {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.policy
}

// SetPolicy replaces the policy wholesale. Common operator path:
// load from disk, mutate one field, write back.
func (g *DispatchGate) SetPolicy(p DispatchPolicy) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.policy = p
}

// SetEnabled is the runtime kill switch. Pulled out as its own
// method because the operator-facing CLI needs a one-call toggle
// without round-tripping the whole policy. Pair-symmetric with
// the spec wording: "the kill-switch is one command away."
func (g *DispatchGate) SetEnabled(on bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.policy.Enabled = on
}

// Inflight reports the current count of in-flight auto-dispatches.
// Read-only; useful for observability + the CLI's status command.
func (g *DispatchGate) Inflight() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.inflight
}

// LoadPolicyFile reads a DispatchPolicy from a JSON file. A
// missing file returns (DispatchPolicy{}, nil) — the zero value
// with Enabled=false, which matches the "opt-in, default safe"
// contract. Other errors propagate.
func LoadPolicyFile(path string) (DispatchPolicy, error) {
	if path == "" {
		return DispatchPolicy{}, nil
	}
	bs, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return DispatchPolicy{}, nil
		}
		return DispatchPolicy{}, fmt.Errorf("planner.LoadPolicyFile: %w", err)
	}
	var p DispatchPolicy
	if len(bs) == 0 {
		return p, nil
	}
	if err := json.Unmarshal(bs, &p); err != nil {
		return DispatchPolicy{}, fmt.Errorf("planner.LoadPolicyFile: decode: %w", err)
	}
	return p, nil
}

// SavePolicyFile writes the policy as JSON to path. Creates parent
// directories as needed (0o755). The file itself is 0o644 — the
// kill switch is not a secret.
func SavePolicyFile(path string, p DispatchPolicy) error {
	if path == "" {
		return errors.New("planner.SavePolicyFile: empty path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("planner.SavePolicyFile: mkdir: %w", err)
	}
	bs, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("planner.SavePolicyFile: encode: %w", err)
	}
	if err := os.WriteFile(path, bs, 0o644); err != nil {
		return fmt.Errorf("planner.SavePolicyFile: write: %w", err)
	}
	return nil
}
