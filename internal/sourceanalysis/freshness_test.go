package sourceanalysis

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCheckFreshness_FreshIndexReportsNotStale(t *testing.T) {
	now := time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)
	r := CheckFreshness(Capabilities{
		Backend:        "fake",
		Available:      true,
		IndexUpdatedAt: now.Add(-30 * time.Minute),
	}, 4*time.Hour, now)
	if r.Stale {
		t.Errorf("fresh index reported stale: %+v", r)
	}
	if r.Reason != "" {
		t.Errorf("happy path should not set Reason: %q", r.Reason)
	}
	if r.Threshold != 4*time.Hour {
		t.Errorf("threshold echoed: %v", r.Threshold)
	}
}

func TestCheckFreshness_StaleIndexReportsStaleWithReason(t *testing.T) {
	now := time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)
	r := CheckFreshness(Capabilities{
		Backend:        "fake",
		Available:      true,
		IndexUpdatedAt: now.Add(-5 * time.Hour),
	}, 4*time.Hour, now)
	if !r.Stale {
		t.Fatalf("expected stale: %+v", r)
	}
	if r.Reason == "" {
		t.Errorf("Reason must be populated when stale")
	}
}

func TestCheckFreshness_ZeroThresholdFallsBackToDefault(t *testing.T) {
	now := time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)
	r := CheckFreshness(Capabilities{Available: true, IndexUpdatedAt: now}, 0, now)
	if r.Threshold != DefaultFreshnessThreshold {
		t.Errorf("threshold = %v, want %v", r.Threshold, DefaultFreshnessThreshold)
	}
}

func TestCheckFreshness_UnavailableNotMarkedStale(t *testing.T) {
	// An already-unavailable backend doesn't double-report as
	// stale — the gate would otherwise log two warnings for one
	// underlying problem.
	r := CheckFreshness(Capabilities{Backend: "noop", Available: false, Reason: "no backend"},
		time.Hour, time.Now())
	if r.Stale {
		t.Errorf("unavailable should not mark stale: %+v", r)
	}
	if r.Reason != "no backend" {
		t.Errorf("inner reason should propagate: %q", r.Reason)
	}
}

func TestCheckFreshness_UnavailableWithEmptyReasonGetsFallback(t *testing.T) {
	r := CheckFreshness(Capabilities{Available: false}, time.Hour, time.Now())
	if r.Reason == "" {
		t.Errorf("fallback reason missing")
	}
}

func TestCheckFreshness_ZeroIndexTimestampMeansFresh(t *testing.T) {
	// Backends that don't expose their index timestamp can't be
	// claimed stale — lean toward fresh and trust the responses.
	r := CheckFreshness(Capabilities{Available: true}, time.Hour, time.Now())
	if r.Stale {
		t.Errorf("zero IndexUpdatedAt should not be stale: %+v", r)
	}
}

// ---- FreshGate decorator ------------------------------------------------

// stubBackend is a programmable SourceAnalysis used by the gate
// tests. Every method tracks call count so tests can assert the
// gate short-circuits before reaching the inner backend.
type stubBackend struct {
	caps         Capabilities
	describeErr  error
	dependents   []Target
	depErr       error
	dependencies []Target
	pubChanges   []Symbol

	calls struct {
		describe     int
		dependents   int
		dependencies int
		pubchanges   int
	}
}

func (s *stubBackend) Describe(_ context.Context) (Capabilities, error) {
	s.calls.describe++
	return s.caps, s.describeErr
}
func (s *stubBackend) Dependents(_ context.Context, _ Target) ([]Target, error) {
	s.calls.dependents++
	return s.dependents, s.depErr
}
func (s *stubBackend) Dependencies(_ context.Context, _ Target) ([]Target, error) {
	s.calls.dependencies++
	return s.dependencies, nil
}
func (s *stubBackend) PublicContractChanges(_ context.Context, _ Diff) ([]Symbol, error) {
	s.calls.pubchanges++
	return s.pubChanges, nil
}

func TestNewFreshGate_NilInnerReturnsNil(t *testing.T) {
	if NewFreshGate(nil, FreshGateConfig{}) != nil {
		t.Errorf("nil inner → nil gate")
	}
}

func TestFreshGate_FreshBackendPassesThrough(t *testing.T) {
	now := time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)
	stub := &stubBackend{
		caps: Capabilities{
			Backend: "stub", Available: true,
			IndexUpdatedAt: now.Add(-10 * time.Minute),
		},
		dependents: []Target{{Repository: "r", Path: "a.go"}},
	}
	gate := NewFreshGate(stub, FreshGateConfig{
		Threshold: time.Hour,
		Now:       func() time.Time { return now },
	})
	got, err := gate.Dependents(context.Background(), Target{Repository: "r", Path: "main.go"})
	if err != nil {
		t.Fatalf("Dependents: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("expected pass-through; got %+v", got)
	}
	if stub.calls.dependents != 1 {
		t.Errorf("inner Dependents called %d times", stub.calls.dependents)
	}
}

func TestFreshGate_StaleBackendReturnsErrUnavailable(t *testing.T) {
	now := time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)
	stub := &stubBackend{
		caps: Capabilities{
			Backend: "stub", Available: true,
			IndexUpdatedAt: now.Add(-5 * time.Hour),
		},
		dependents: []Target{{Repository: "r", Path: "stale.go"}},
	}
	gate := NewFreshGate(stub, FreshGateConfig{
		Threshold: time.Hour,
		Now:       func() time.Time { return now },
	})
	got, err := gate.Dependents(context.Background(), Target{Repository: "r"})
	if !errors.Is(err, ErrUnavailable) {
		t.Errorf("err = %v, want ErrUnavailable", err)
	}
	if got != nil {
		t.Errorf("stale gate returned non-nil result: %+v", got)
	}
	// Inner Dependents MUST NOT be called when the gate refuses.
	if stub.calls.dependents != 0 {
		t.Errorf("inner Dependents called %d times despite stale gate", stub.calls.dependents)
	}
}

func TestFreshGate_DescribeRewritesUnavailableWhenStale(t *testing.T) {
	now := time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)
	stub := &stubBackend{
		caps: Capabilities{
			Backend: "stub", Available: true,
			IndexUpdatedAt: now.Add(-5 * time.Hour),
		},
	}
	gate := NewFreshGate(stub, FreshGateConfig{
		Threshold: time.Hour,
		Now:       func() time.Time { return now },
	})
	caps, err := gate.Describe(context.Background())
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if caps.Available {
		t.Errorf("stale backend's Describe must report unavailable")
	}
	if caps.Reason == "" {
		t.Errorf("stale Describe should carry Reason")
	}
}

func TestFreshGate_DescribeKeepsAvailableWhenFresh(t *testing.T) {
	now := time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)
	stub := &stubBackend{
		caps: Capabilities{
			Backend: "stub", Available: true,
			IndexUpdatedAt: now.Add(-30 * time.Minute),
		},
	}
	gate := NewFreshGate(stub, FreshGateConfig{
		Threshold: time.Hour,
		Now:       func() time.Time { return now },
	})
	caps, err := gate.Describe(context.Background())
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if !caps.Available {
		t.Errorf("fresh backend should pass through Available=true")
	}
}

func TestFreshGate_DescribeCachedAcrossMultipleQueries(t *testing.T) {
	// Each per-query path consults the gate's cached freshness,
	// so a flurry of Dependents calls inside one planner pass
	// pays exactly one Describe round-trip.
	now := time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)
	stub := &stubBackend{
		caps: Capabilities{
			Backend: "stub", Available: true,
			IndexUpdatedAt: now.Add(-30 * time.Minute),
		},
	}
	gate := NewFreshGate(stub, FreshGateConfig{
		Threshold: time.Hour,
		CacheTTL:  10 * time.Second,
		Now:       func() time.Time { return now },
	})
	for i := 0; i < 5; i++ {
		_, _ = gate.Dependents(context.Background(), Target{})
	}
	if stub.calls.describe != 1 {
		t.Errorf("Describe called %d times, want 1 (caching broken)", stub.calls.describe)
	}
}

func TestFreshGate_PassesContractWhenFresh(t *testing.T) {
	now := time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)
	stub := &stubBackend{
		caps: Capabilities{
			Backend: "stub", Available: true,
			IndexUpdatedAt: now.Add(-30 * time.Minute),
		},
	}
	gate := NewFreshGate(stub, FreshGateConfig{
		Threshold: time.Hour,
		Now:       func() time.Time { return now },
	})
	RunContract(t, gate, Target{Repository: "r", Path: "main.go"})
}

func TestFreshGate_PassesContractWhenStale(t *testing.T) {
	now := time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)
	stub := &stubBackend{
		caps: Capabilities{
			Backend: "stub", Available: true,
			IndexUpdatedAt: now.Add(-5 * time.Hour),
		},
	}
	gate := NewFreshGate(stub, FreshGateConfig{
		Threshold: time.Hour,
		Now:       func() time.Time { return now },
	})
	RunContract(t, gate, Target{Repository: "r"})
}

func TestFreshGate_UnavailableInnerNotDoubleStale(t *testing.T) {
	// The inner is already unavailable for an unrelated reason;
	// the gate must not report its own Stale on top.
	now := time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)
	stub := &stubBackend{
		caps: Capabilities{
			Backend: "stub", Available: false,
			Reason: "daemon down",
		},
	}
	gate := NewFreshGate(stub, FreshGateConfig{
		Threshold: time.Hour,
		Now:       func() time.Time { return now },
	})
	caps, err := gate.Describe(context.Background())
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if caps.Reason != "daemon down" {
		t.Errorf("inner reason overridden: %q", caps.Reason)
	}
}
