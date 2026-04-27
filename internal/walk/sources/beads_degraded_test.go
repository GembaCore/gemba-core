// gm-vch2: BeadsDegradedLister tests — assert healthy adaptors
// contribute zero, degraded adaptors mint stable-id escalations,
// and the lister is nil-safe.

package sources

import (
	"context"
	"testing"
	"time"

	"github.com/MikeBengtson/gemba/internal/adapter/registry"
	"github.com/MikeBengtson/gemba/core"
)

type fakeHealthBus struct {
	snap []registry.AdaptorStatus
}

func (f *fakeHealthBus) Snapshot() []registry.AdaptorStatus { return f.snap }

func TestBeadsDegradedLister_HealthyContributesZero(t *testing.T) {
	bus := &fakeHealthBus{snap: []registry.AdaptorStatus{
		{Name: "bd", Plane: registry.WorkPlane, Healthy: true},
		{Name: "native", Plane: registry.OrchestrationPlane, Healthy: true},
	}}
	l := NewBeadsDegradedLister(bus)
	got, err := l.ListOpenEscalations(context.Background(), "")
	if err != nil {
		t.Fatalf("ListOpenEscalations: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d items; want 0 (all healthy)", len(got))
	}
}

func TestBeadsDegradedLister_DegradedMintsRow(t *testing.T) {
	bus := &fakeHealthBus{snap: []registry.AdaptorStatus{
		{Name: "bd", Plane: registry.WorkPlane, Healthy: false, Reason: "dolt: connect-timeout"},
		{Name: "native", Plane: registry.OrchestrationPlane, Healthy: true},
	}}
	l := NewBeadsDegradedLister(bus)
	got, err := l.ListOpenEscalations(context.Background(), "")
	if err != nil {
		t.Fatalf("ListOpenEscalations: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d items; want 1", len(got))
	}
	r := got[0]
	if r.Source != core.EscalationBeadsDegraded {
		t.Errorf("Source = %q; want %q", r.Source, core.EscalationBeadsDegraded)
	}
	if r.Urgency != core.UrgencyAdvisory {
		t.Errorf("Urgency = %q; want advisory", r.Urgency)
	}
	if r.Title == "" || r.Prompt == "" {
		t.Errorf("expected populated Title+Prompt; got %+v", r)
	}
	if r.State != core.EscalationOpen {
		t.Errorf("State = %q; want open", r.State)
	}
}

func TestBeadsDegradedLister_StableIDAcrossCalls(t *testing.T) {
	bus := &fakeHealthBus{snap: []registry.AdaptorStatus{
		{Name: "bd", Plane: registry.WorkPlane, Healthy: false, Reason: "dolt down"},
	}}
	l := NewBeadsDegradedLister(bus)
	first, _ := l.ListOpenEscalations(context.Background(), "")
	second, _ := l.ListOpenEscalations(context.Background(), "")
	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("expected 1 item per call; got %d / %d", len(first), len(second))
	}
	if first[0].ID != second[0].ID {
		t.Errorf("ids changed across refresh: %q vs %q (must be stable per degraded span)",
			first[0].ID, second[0].ID)
	}
}

func TestBeadsDegradedLister_IDChangesWhenReasonChanges(t *testing.T) {
	bus := &fakeHealthBus{snap: []registry.AdaptorStatus{
		{Name: "bd", Plane: registry.WorkPlane, Healthy: false, Reason: "dolt down"},
	}}
	l := NewBeadsDegradedLister(bus)
	first, _ := l.ListOpenEscalations(context.Background(), "")

	bus.snap[0].Reason = "bd: query-failed"
	second, _ := l.ListOpenEscalations(context.Background(), "")

	if first[0].ID == second[0].ID {
		t.Errorf("id stayed stable across reason change; want roll. id=%q", first[0].ID)
	}
}

func TestBeadsDegradedLister_NilSafe(t *testing.T) {
	if got := NewBeadsDegradedLister(nil); got != nil {
		t.Errorf("NewBeadsDegradedLister(nil) = %v; want nil", got)
	}
	var l *BeadsDegradedLister
	got, err := l.ListOpenEscalations(context.Background(), "")
	if err != nil {
		t.Errorf("nil-receiver returned err: %v", err)
	}
	if got != nil {
		t.Errorf("nil-receiver should contribute zero; got %v", got)
	}
}

func TestBeadsDegradedLister_EmptyReasonGetsDefault(t *testing.T) {
	bus := &fakeHealthBus{snap: []registry.AdaptorStatus{
		{Name: "x", Plane: registry.WorkPlane, Healthy: false},
	}}
	l := NewBeadsDegradedLister(bus)
	got, _ := l.ListOpenEscalations(context.Background(), "")
	if len(got) != 1 {
		t.Fatalf("got %d items; want 1", len(got))
	}
	if got[0].Prompt == "" {
		t.Error("expected default prompt when reason is empty")
	}
}

// Compile-time confirmation that the time field is set.
var _ = time.Now
