package dispatch

import (
	"testing"
	"time"
)

func TestPickEmptyReturnsNoCandidate(t *testing.T) {
	if pane, ok := Pick(nil); ok || pane != "" {
		t.Errorf("empty: want ('', false) got (%q, %v)", pane, ok)
	}
}

func TestPickSkipsAtCap(t *testing.T) {
	cands := []Candidate{
		{PaneID: "%1", InFlight: 2, MaxParallel: 2},
	}
	if pane, ok := Pick(cands); ok {
		t.Errorf("at-cap should be skipped, got %q", pane)
	}
}

func TestPickPrefersLowestInFlight(t *testing.T) {
	t0 := time.Now()
	cands := []Candidate{
		{PaneID: "%full", InFlight: 2, MaxParallel: 3, StartedAt: t0},
		{PaneID: "%empty", InFlight: 0, MaxParallel: 3, StartedAt: t0.Add(time.Second)},
		{PaneID: "%mid", InFlight: 1, MaxParallel: 3, StartedAt: t0},
	}
	pane, ok := Pick(cands)
	if !ok {
		t.Fatal("want a candidate")
	}
	if pane != "%empty" {
		t.Errorf("want %%empty, got %q", pane)
	}
}

func TestPickTiebreakOldestStartedAt(t *testing.T) {
	t0 := time.Now()
	cands := []Candidate{
		{PaneID: "%new", InFlight: 1, MaxParallel: 3, StartedAt: t0.Add(time.Hour)},
		{PaneID: "%old", InFlight: 1, MaxParallel: 3, StartedAt: t0},
		{PaneID: "%mid", InFlight: 1, MaxParallel: 3, StartedAt: t0.Add(time.Minute)},
	}
	pane, _ := Pick(cands)
	if pane != "%old" {
		t.Errorf("oldest tiebreak: want %%old, got %q", pane)
	}
}

func TestPickIsDeterministic(t *testing.T) {
	t0 := time.Now()
	cands := []Candidate{
		{PaneID: "%a", InFlight: 0, MaxParallel: 2, StartedAt: t0},
		{PaneID: "%b", InFlight: 0, MaxParallel: 2, StartedAt: t0.Add(time.Second)},
	}
	for i := 0; i < 5; i++ {
		pane, _ := Pick(cands)
		if pane != "%a" {
			t.Errorf("run %d: drift, got %q", i, pane)
		}
	}
}

func TestHasCapacity(t *testing.T) {
	if !(Candidate{InFlight: 1, MaxParallel: 2}).HasCapacity() {
		t.Error("1/2 should have capacity")
	}
	if (Candidate{InFlight: 2, MaxParallel: 2}).HasCapacity() {
		t.Error("2/2 should not have capacity")
	}
	if (Candidate{InFlight: 0, MaxParallel: 0}).HasCapacity() {
		t.Error("0/0 should not have capacity (treat as no parallelism)")
	}
}
