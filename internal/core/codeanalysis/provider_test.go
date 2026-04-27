package codeanalysis

import (
	"context"
	"testing"
	"time"
)

// stubProvider is a test-local zero-behavior implementation used
// to assert at compile time that the [Provider] interface is
// satisfiable. The methods return zero values; the registry +
// config tests exercise everything else.
type stubProvider struct{}

func (stubProvider) Manifest(context.Context) (Manifest, error) {
	return Manifest{Backend: "stub"}, nil
}
func (stubProvider) ListRepos(context.Context) ([]RepoRef, error) {
	return nil, nil
}
func (stubProvider) Reindex(context.Context, RepoRef, ReindexFlags) error {
	return nil
}
func (stubProvider) Modules(context.Context, RepoRef) ([]Module, error) {
	return nil, nil
}
func (stubProvider) Health(context.Context, RepoRef) (HealthReport, error) {
	return HealthReport{}, nil
}
func (stubProvider) Impact(context.Context, RepoRef, string, ImpactDirection) (ImpactReport, error) {
	return ImpactReport{}, nil
}

// Compile-time assertion that stubProvider implements Provider.
// Tests fail to build (rather than at runtime) if the interface
// drifts away from the stub.
var _ Provider = (*stubProvider)(nil)

func TestReindexPolicy_IsValid(t *testing.T) {
	cases := []struct {
		policy ReindexPolicy
		want   bool
	}{
		{PolicyPostMerge, true},
		{PolicyScheduled, true},
		{PolicyManual, true},
		{PolicyOnDemand, true},
		{"", false},
		{"weekly", false},
	}
	for _, tc := range cases {
		if got := tc.policy.IsValid(); got != tc.want {
			t.Errorf("ReindexPolicy(%q).IsValid() = %v, want %v",
				tc.policy, got, tc.want)
		}
	}
}

func TestImpactDirection_IsValid(t *testing.T) {
	cases := []struct {
		dir  ImpactDirection
		want bool
	}{
		{ImpactUpstream, true},
		{ImpactDownstream, true},
		{ImpactBoth, true},
		{"", false},
		{"sideways", false},
	}
	for _, tc := range cases {
		if got := tc.dir.IsValid(); got != tc.want {
			t.Errorf("ImpactDirection(%q).IsValid() = %v, want %v",
				tc.dir, got, tc.want)
		}
	}
}

func TestHealthReport_IsStale(t *testing.T) {
	now := time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)
	t.Run("zero stale_after never stales", func(t *testing.T) {
		h := HealthReport{FetchedAt: now.Add(-30 * 24 * time.Hour)}
		if h.IsStale(now) {
			t.Fatalf("expected zero StaleAfter to never report stale")
		}
	})
	t.Run("within window is fresh", func(t *testing.T) {
		h := HealthReport{
			FetchedAt:  now.Add(-30 * time.Minute),
			StaleAfter: time.Hour,
		}
		if h.IsStale(now) {
			t.Fatalf("expected fresh report")
		}
	})
	t.Run("past window is stale", func(t *testing.T) {
		h := HealthReport{
			FetchedAt:  now.Add(-2 * time.Hour),
			StaleAfter: time.Hour,
		}
		if !h.IsStale(now) {
			t.Fatalf("expected stale report")
		}
	})
}
