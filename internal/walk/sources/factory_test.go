// gm-vch2: factory tests — composition of multiple sub-listers
// preserves order and propagates errors; the LiveSources factory
// produces a non-empty agenda when wired against fakes; nil
// configuration produces a zero-value walk.Sources.

package sources

import (
	"context"
	"errors"
	"testing"

	"github.com/MikeBengtson/gemba/core"
	"github.com/MikeBengtson/gemba/internal/adapter/registry"
	"github.com/MikeBengtson/gemba/internal/walk"
)

type stubLister struct {
	out []core.EscalationRequest
	err error
}

func (s *stubLister) ListOpenEscalations(context.Context, string) ([]core.EscalationRequest, error) {
	return s.out, s.err
}

func TestCombinedEscalationLister_PreservesOrder(t *testing.T) {
	a := &stubLister{out: []core.EscalationRequest{{ID: "a-1"}, {ID: "a-2"}}}
	b := &stubLister{out: []core.EscalationRequest{{ID: "b-1"}}}
	c := &stubLister{out: []core.EscalationRequest{{ID: "c-1"}}}
	combined := NewCombinedEscalationLister(a, b, c)
	got, err := combined.ListOpenEscalations(context.Background(), "")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	wantIDs := []string{"a-1", "a-2", "b-1", "c-1"}
	if len(got) != len(wantIDs) {
		t.Fatalf("got %d; want %d", len(got), len(wantIDs))
	}
	for i, w := range wantIDs {
		if got[i].ID != w {
			t.Errorf("got[%d].ID = %q; want %q", i, got[i].ID, w)
		}
	}
}

func TestCombinedEscalationLister_PropagatesError(t *testing.T) {
	wantErr := errors.New("upstream b failed")
	combined := NewCombinedEscalationLister(
		&stubLister{out: []core.EscalationRequest{{ID: "a"}}},
		&stubLister{err: wantErr},
		&stubLister{out: []core.EscalationRequest{{ID: "c"}}},
	)
	_, err := combined.ListOpenEscalations(context.Background(), "")
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v; want %v", err, wantErr)
	}
}

func TestCombinedEscalationLister_SkipsTypedNil(t *testing.T) {
	// Pass typed-nil values from each constructor — the combined
	// lister must skip them rather than dereference and panic.
	combined := NewCombinedEscalationLister(
		(*OrchestrationPlaneEscalationLister)(nil),
		(*WitnessLister)(nil),
		(*RefineryRejectionLister)(nil),
		(*BeadsDegradedLister)(nil),
	)
	if combined != nil {
		t.Errorf("expected nil combined when every entry is typed-nil; got %v", combined)
	}
}

func TestCombinedEscalationLister_NilReceiver(t *testing.T) {
	var c *CombinedEscalationLister
	got, err := c.ListOpenEscalations(context.Background(), "")
	if err != nil {
		t.Errorf("nil receiver err: %v", err)
	}
	if got != nil {
		t.Errorf("got %v; want nil", got)
	}
}

func TestLiveSources_BuildsNonEmptyAgenda(t *testing.T) {
	bus := &fakeHealthBus{snap: []registry.AdaptorStatus{
		{Name: "bd", Plane: registry.WorkPlane, Healthy: false, Reason: "dolt down"},
	}}
	src := LiveSources(LiveSourcesConfig{
		HealthBus: bus,
		Witness: WitnessFindingSourceFunc(func(_ context.Context, _ string) ([]WitnessFinding, error) {
			return []WitnessFinding{{ID: "wf-1", Title: "fishy"}}, nil
		}),
		Refinery: RefineryRejectionSourceFunc(func(_ context.Context, _ string) ([]RefineryRejection, error) {
			return []RefineryRejection{{ID: "pr-1", Title: "rejected"}}, nil
		}),
	})
	if src.Escalations == nil {
		t.Fatal("Escalations lister is nil; expected combined")
	}
	got, err := src.Escalations.ListOpenEscalations(context.Background(), "")
	if err != nil {
		t.Fatalf("ListOpenEscalations: %v", err)
	}
	// One witness + one refinery + one degraded.
	if len(got) != 3 {
		t.Errorf("got %d items; want 3 (witness+refinery+degraded). %+v", len(got), got)
	}

	// Now build the agenda — every row should land.
	agenda, err := walk.BuildAgenda(context.Background(), src, walk.AggregateOptions{})
	if err != nil {
		t.Fatalf("BuildAgenda: %v", err)
	}
	if len(agenda) != 3 {
		t.Errorf("agenda has %d items; want 3", len(agenda))
	}
}

func TestLiveSources_AllNilProducesEmpty(t *testing.T) {
	src := LiveSources(LiveSourcesConfig{})
	if src.Escalations != nil {
		t.Errorf("expected nil Escalations when no live source wired; got %v", src.Escalations)
	}
	// BuildAgenda must still succeed with an entirely zero-value Sources.
	agenda, err := walk.BuildAgenda(context.Background(), src, walk.AggregateOptions{})
	if err != nil {
		t.Fatalf("BuildAgenda on empty sources: %v", err)
	}
	if len(agenda) != 0 {
		t.Errorf("got %d items; want 0", len(agenda))
	}
}
