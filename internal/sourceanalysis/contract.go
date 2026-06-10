package sourceanalysis

import (
	"context"
	"errors"
	"testing"
)

// RunContract exercises the universal SourceAnalysis contract a
// binding MUST satisfy regardless of backend (gm-s47n.3.2 noop and
// gm-s47n.3.3 GitNexus both pass it). Bindings call this from a
// test that supplies a fresh SourceAnalysis instance plus a single
// known-existing target in the bound index.
//
// Contract:
//   - Dependents and Dependencies return nil (not nil-safe slice)
//     OR a non-nil slice; never panic.
//   - When the backend is unavailable they return (nil, ErrUnavailable)
//     wrapped via errors.Is — no other error sentinels.
//   - PublicContractChanges over an empty diff returns an empty
//     slice with no error (degenerate case, never spuriously fails).
//   - Describe never returns ErrUnavailable itself; the field is
//     reported via Capabilities.Available + Capabilities.Reason.
//
// The probe target may be the zero value when the backend is the
// noop binding — the contract still holds because every method
// returns the empty/unavailable answer for any input.
func RunContract(t *testing.T, sa SourceAnalysis, probe Target) {
	t.Helper()
	ctx := context.Background()

	t.Run("Dependents/never-panics", func(t *testing.T) {
		t.Helper()
		out, err := sa.Dependents(ctx, probe)
		if err != nil && !errors.Is(err, ErrUnavailable) {
			t.Fatalf("Dependents returned non-ErrUnavailable error: %v", err)
		}
		if err != nil && out != nil {
			t.Fatalf("Dependents: when err is set, result must be nil; got %d entries", len(out))
		}
	})

	t.Run("Dependencies/never-panics", func(t *testing.T) {
		t.Helper()
		out, err := sa.Dependencies(ctx, probe)
		if err != nil && !errors.Is(err, ErrUnavailable) {
			t.Fatalf("Dependencies returned non-ErrUnavailable error: %v", err)
		}
		if err != nil && out != nil {
			t.Fatalf("Dependencies: when err is set, result must be nil; got %d entries", len(out))
		}
	})

	t.Run("PublicContractChanges/empty-diff", func(t *testing.T) {
		t.Helper()
		out, err := sa.PublicContractChanges(ctx, Diff{Repository: probe.Repository})
		if err != nil && !errors.Is(err, ErrUnavailable) {
			t.Fatalf("PublicContractChanges returned non-ErrUnavailable error: %v", err)
		}
		// Empty input with an Available backend MUST yield empty
		// output and no error — anything else is a bug.
		if err == nil && len(out) != 0 {
			t.Fatalf("PublicContractChanges over empty diff returned %d symbols, want 0", len(out))
		}
	})

	t.Run("Describe/no-ErrUnavailable", func(t *testing.T) {
		t.Helper()
		caps, err := sa.Describe(ctx)
		if errors.Is(err, ErrUnavailable) {
			t.Fatal("Describe returned ErrUnavailable; it must report unavailability via Capabilities.Available instead")
		}
		if err != nil {
			t.Fatalf("Describe returned unexpected error: %v", err)
		}
		if caps.Backend == "" {
			t.Fatal("Describe.Capabilities.Backend is empty; bindings MUST self-identify")
		}
	})
}
