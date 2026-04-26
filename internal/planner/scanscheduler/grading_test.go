package scanscheduler

import (
	"context"
	"testing"
	"time"
)

// ctxNoTimeout returns context.Background; tiny helper so test
// readers see "we don't care about cancellation in this test" by
// name rather than ceremony.
func ctxNoTimeout() context.Context { return context.Background() }

func TestGrade_FreshIndexAttributesElsewhere(t *testing.T) {
	now := t0()
	s, _ := newSchedAt(now)
	miss := MissedConflict{
		Repo:        "gt",
		ObservedAt:  now,
		IndexedAtObservation: Freshness{
			Repo:          "gt",
			IndexedAt:     now.Add(-2 * time.Minute),
			HeadCommit:    "abc",
			IndexedCommit: "abc",
		},
	}
	g := s.Grade(miss, nil)
	if g.IndexWasStale {
		t.Errorf("IndexWasStale=true with fresh observation; expected false")
	}
	if g.SuppressedTrigger != TriggerUnknown {
		t.Errorf("SuppressedTrigger = %s, want unknown", g.SuppressedTrigger)
	}
}

func TestGrade_StaleIndexBlamesWallClockFloor(t *testing.T) {
	now := t0()
	s, _ := newSchedAt(now)
	// Index 5 hours old at observation; commits-ahead but small
	// gap relative to drift threshold means wall-clock-floor is
	// the right attribution.
	miss := MissedConflict{
		Repo:       "gt",
		ObservedAt: now,
		IndexedAtObservation: Freshness{
			Repo:          "gt",
			IndexedAt:     now.Add(-5 * time.Hour),
			HeadCommit:    "abc",
			IndexedCommit: "def",
			CommitsAhead:  0, // bypass the drift heuristic
		},
	}
	g := s.Grade(miss, nil)
	if !g.IndexWasStale {
		t.Fatalf("IndexWasStale=false; expected true")
	}
	if g.SuppressedTrigger != TriggerWallClockFloor {
		t.Errorf("SuppressedTrigger = %s, want wall-clock-floor", g.SuppressedTrigger)
	}
	if g.Suggestion == "" {
		t.Error("Suggestion is empty; wall-clock-floor case must propose a tweak")
	}
}

func TestGrade_DriftSignalAttributedWhenBackendHadVisibleAhead(t *testing.T) {
	now := t0()
	s, _ := newSchedAt(now)
	// Index recent (5 min) but backend reports commits-ahead — drift
	// threshold (30m default) hadn't been crossed yet.
	miss := MissedConflict{
		Repo:       "gt",
		ObservedAt: now,
		IndexedAtObservation: Freshness{
			Repo:          "gt",
			IndexedAt:     now.Add(-5 * time.Minute),
			HeadCommit:    "abc",
			IndexedCommit: "def",
			CommitsAhead:  3,
		},
	}
	g := s.Grade(miss, nil)
	if g.SuppressedTrigger != TriggerDriftSignal {
		t.Errorf("SuppressedTrigger = %s, want drift-signal", g.SuppressedTrigger)
	}
}

func TestGrade_NoIndexAttributedAsPreDispatchDemand(t *testing.T) {
	now := t0()
	s, _ := newSchedAt(now)
	miss := MissedConflict{
		Repo:                 "gt",
		ObservedAt:           now,
		IndexedAtObservation: Freshness{Repo: "gt"}, // never scanned
	}
	g := s.Grade(miss, nil)
	if !g.IndexWasStale {
		t.Errorf("IndexWasStale=false on never-indexed; expected true")
	}
	// IndexedAt zero → IsStale true → no commits-ahead → no wall-
	// clock interval since IndexedAt is zero → falls through to
	// pre-dispatch demand.
	if g.SuppressedTrigger != TriggerPreDispatchDemand {
		t.Errorf("SuppressedTrigger = %s, want pre-dispatch-demand", g.SuppressedTrigger)
	}
}
