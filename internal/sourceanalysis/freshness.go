// Index-freshness check + skipped-with-warning semantics
// (gm-s47n.3.4).
//
// The Layer 2 source analysis interface is only useful if its index
// is fresh. A stale index produces silently wrong dependent sets,
// which produces silently missed semantic conflicts (spec §5.3),
// which produces parallel-dispatched beads that turn out to collide.
// Spec §11 (failure modes): "Detect this on every call to the source
// analysis interface; degrade to 'skipped semantic check' with a
// warning rather than silently returning stale dependents."
//
// This file adds two surfaces:
//
//   1. CheckFreshness — pure helper that turns a Capabilities
//      snapshot + an age threshold into a FreshnessReport. The
//      planner / CLI can consult this without wrapping the
//      backend.
//
//   2. NewFreshGate — decorator that wraps a SourceAnalysis,
//      consults Describe on each query, and returns
//      (nil, ErrUnavailable) when the index is stale. Existing
//      callers (planner.SemanticConflicts, etc.) treat
//      ErrUnavailable as a soft skip + log, so wrapping a backend
//      in the gate is the one-liner that opts a deployment into
//      "stale → skipped" semantics.
//
// Both surfaces honour the same threshold rule:
//
//   - threshold ≤ 0 → DefaultFreshnessThreshold (4h, matches the
//     spec §8.1 wall-clock floor).
//   - Capabilities.IndexUpdatedAt zero → "no freshness signal";
//     treat as fresh (the backend can't claim staleness on data
//     it never reported).

package sourceanalysis

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// DefaultFreshnessThreshold matches spec §8.1's wall-clock floor.
// Operators tune per rig via the planner's settings; this is the
// fallback when callers pass 0.
const DefaultFreshnessThreshold = 4 * time.Hour

// FreshnessReport summarises whether the backend's index is fresh
// enough to trust for semantic-conflict detection. Returned by
// CheckFreshness; also threaded through the gate's stale-skip
// reason.
//
// Wire shape: stable JSON tags so the planner's audit / notices
// payloads can carry the report without reinventing the schema.
type FreshnessReport struct {
	Backend   string        `json:"backend"`
	Available bool          `json:"available"`
	// Stale is true when Available is true AND IndexUpdatedAt is
	// non-zero AND the index is older than Threshold. False on
	// every other path — including unavailable backends and
	// missing IndexUpdatedAt — so the planner doesn't double-fire
	// "skip + warn" against a backend already returning
	// ErrUnavailable for an unrelated reason.
	Stale     bool          `json:"stale"`
	Age       time.Duration `json:"age"`
	Threshold time.Duration `json:"threshold"`
	// Reason is a one-line operator-facing explanation. Empty
	// when the index is fresh (callers don't need a "why" for
	// the happy path). Surface text only.
	Reason string `json:"reason,omitempty"`
}

// CheckFreshness inspects the given Capabilities snapshot and
// returns the freshness report. Pure — same inputs, same output.
//
// now is injected for deterministic tests; pass time.Now in
// production.
func CheckFreshness(caps Capabilities, threshold time.Duration, now time.Time) FreshnessReport {
	if threshold <= 0 {
		threshold = DefaultFreshnessThreshold
	}
	r := FreshnessReport{
		Backend:   caps.Backend,
		Available: caps.Available,
		Threshold: threshold,
	}
	if !caps.Available {
		r.Reason = fallbackReason(caps.Reason, "backend reports unavailable")
		return r
	}
	if caps.IndexUpdatedAt.IsZero() {
		// Backend doesn't expose its index timestamp — we can't
		// claim it's stale. Lean toward "fresh" and trust the
		// downstream Dependents() responses. The drift signal in
		// spec §8.1 (a high-priority trigger) is the layer that
		// surfaces "the index thinks it's stale on its own."
		return r
	}
	r.Age = now.Sub(caps.IndexUpdatedAt)
	if r.Age > threshold {
		r.Stale = true
		r.Reason = fmt.Sprintf("index age %s exceeds threshold %s",
			r.Age.Round(time.Second), threshold)
	}
	return r
}

// freshGate decorates a SourceAnalysis to refuse stale responses.
// Each query method calls Describe (cached for cacheTTL) and
// short-circuits to ErrUnavailable when the index is stale. The
// gate's own Describe surfaces a Capabilities with Reason set so
// the operator sees why the planner is skipping.
type freshGate struct {
	inner     SourceAnalysis
	threshold time.Duration
	now       func() time.Time

	cacheTTL time.Duration
	mu       sync.Mutex
	cached   *cachedReport
}

type cachedReport struct {
	report   FreshnessReport
	caps     Capabilities
	expires  time.Time
}

// FreshGateConfig configures NewFreshGate. Zero-value fields fall
// back to defaults so a caller building a "use the typical setup"
// gate is one line:
//
//	gated := sourceanalysis.NewFreshGate(real, sourceanalysis.FreshGateConfig{})
type FreshGateConfig struct {
	// Threshold is the staleness budget. Zero → DefaultFreshnessThreshold.
	Threshold time.Duration
	// CacheTTL bounds how often the gate consults Describe. Zero
	// → 30 seconds. The cache is per-instance, in-memory, only
	// active during a single planner pass.
	CacheTTL time.Duration
	// Now is injected for tests. Zero → time.Now.
	Now func() time.Time
}

// NewFreshGate wraps inner so its responses are gated on freshness.
// nil inner returns nil (callers building optional decorators chain
// freely). The gate itself implements SourceAnalysis.
func NewFreshGate(inner SourceAnalysis, cfg FreshGateConfig) SourceAnalysis {
	if inner == nil {
		return nil
	}
	if cfg.Threshold <= 0 {
		cfg.Threshold = DefaultFreshnessThreshold
	}
	if cfg.CacheTTL <= 0 {
		cfg.CacheTTL = 30 * time.Second
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &freshGate{
		inner:     inner,
		threshold: cfg.Threshold,
		cacheTTL:  cfg.CacheTTL,
		now:       cfg.Now,
	}
}

// Compile-time interface check.
var _ SourceAnalysis = (*freshGate)(nil)

// freshness consults the inner backend's Describe (cached) and
// returns the current FreshnessReport + the underlying Capabilities.
// One mutex; cheap to call on the hot path.
func (g *freshGate) freshness(ctx context.Context) (FreshnessReport, Capabilities, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.cached != nil && g.now().Before(g.cached.expires) {
		return g.cached.report, g.cached.caps, nil
	}
	caps, err := g.inner.Describe(ctx)
	if err != nil {
		return FreshnessReport{}, Capabilities{}, err
	}
	report := CheckFreshness(caps, g.threshold, g.now())
	g.cached = &cachedReport{
		report:  report,
		caps:    caps,
		expires: g.now().Add(g.cacheTTL),
	}
	return report, caps, nil
}

func (g *freshGate) gateOrCall(ctx context.Context) error {
	report, _, err := g.freshness(ctx)
	if err != nil {
		return err
	}
	if !report.Available || report.Stale {
		// Callers (SemanticConflicts) treat ErrUnavailable as a
		// soft skip + warning. The gate's Describe surfaces the
		// Reason for operator audit; the per-query path here
		// just signals "don't trust the response."
		return ErrUnavailable
	}
	return nil
}

func (g *freshGate) Dependents(ctx context.Context, target Target) ([]Target, error) {
	if err := g.gateOrCall(ctx); err != nil {
		return nil, err
	}
	return g.inner.Dependents(ctx, target)
}

func (g *freshGate) Dependencies(ctx context.Context, target Target) ([]Target, error) {
	if err := g.gateOrCall(ctx); err != nil {
		return nil, err
	}
	return g.inner.Dependencies(ctx, target)
}

func (g *freshGate) PublicContractChanges(ctx context.Context, diff Diff) ([]Symbol, error) {
	if err := g.gateOrCall(ctx); err != nil {
		return nil, err
	}
	return g.inner.PublicContractChanges(ctx, diff)
}

// Describe propagates the inner Describe but rewrites Available +
// Reason to reflect the gate's verdict. Callers consulting Describe
// directly (the planner's freshness check, the doctor command) see
// the SAME signal the per-query path uses — the gate is a single
// source of truth for "is this index trustworthy right now."
func (g *freshGate) Describe(ctx context.Context) (Capabilities, error) {
	report, caps, err := g.freshness(ctx)
	if err != nil {
		return Capabilities{}, err
	}
	if report.Stale {
		caps.Available = false
		caps.Reason = report.Reason
	}
	return caps, nil
}

// fallbackReason returns the inner reason when set; otherwise the
// caller-supplied default. Centralised so unavailability paths
// don't bury the backend's own message.
func fallbackReason(inner, fallback string) string {
	if inner != "" {
		return inner
	}
	return fallback
}
