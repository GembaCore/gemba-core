package scoring

import (
	"math"
	"testing"

	"github.com/MikeBengtson/gemba/core"
)

func TestEpicAffinity_FirstPickInStreakScoresOne(t *testing.T) {
	got := EpicAffinity("gm-epic-A", EpicStreak{
		CurrentEpicID: "gm-epic-A", ContiguousCount: 1,
	})
	if got != 1.0 {
		t.Errorf("got %v, want 1.0", got)
	}
}

func TestEpicAffinity_FadesAcrossStreakWithDefaultDecay(t *testing.T) {
	streak := EpicStreak{CurrentEpicID: "gm-epic-A"}
	for n := 1; n <= 11; n++ {
		streak.ContiguousCount = n
		got := EpicAffinity("gm-epic-A", streak)
		want := 1 - 0.1*float64(n-1)
		if want < 0 {
			want = 0
		}
		if math.Abs(got-want) > 1e-9 {
			t.Errorf("n=%d: got %v, want %v", n, got, want)
		}
	}
}

func TestEpicAffinity_DifferentEpicAlwaysZero(t *testing.T) {
	got := EpicAffinity("gm-epic-B", EpicStreak{
		CurrentEpicID: "gm-epic-A", ContiguousCount: 1,
	})
	if got != 0 {
		t.Errorf("got %v, want 0", got)
	}
}

func TestEpicAffinity_EmptyCurrentEpicReturnsZero(t *testing.T) {
	got := EpicAffinity("gm-epic-A", EpicStreak{})
	if got != 0 {
		t.Errorf("got %v, want 0 (no streak)", got)
	}
}

func TestEpicAffinity_EmptyCandidateEpicReturnsZero(t *testing.T) {
	// Candidate has no parent epic. Don't credit it.
	got := EpicAffinity("", EpicStreak{
		CurrentEpicID: "gm-epic-A", ContiguousCount: 1,
	})
	if got != 0 {
		t.Errorf("got %v, want 0", got)
	}
}

func TestEpicAffinity_ZeroContiguousCountReturnsZero(t *testing.T) {
	got := EpicAffinity("gm-epic-A", EpicStreak{
		CurrentEpicID: "gm-epic-A", ContiguousCount: 0,
	})
	if got != 0 {
		t.Errorf("got %v, want 0", got)
	}
}

func TestEpicAffinity_CustomDecayOverridesDefault(t *testing.T) {
	got := EpicAffinity("gm-epic-A", EpicStreak{
		CurrentEpicID: "gm-epic-A", ContiguousCount: 3, DecayPerTurn: 0.4,
	})
	// 1 - 0.4*(3-1) = 0.2
	if math.Abs(got-0.2) > 1e-9 {
		t.Errorf("got %v, want 0.2", got)
	}
}

func TestEpicAffinity_DecayClampsAtZero(t *testing.T) {
	got := EpicAffinity("gm-epic-A", EpicStreak{
		CurrentEpicID: "gm-epic-A", ContiguousCount: 50, DecayPerTurn: 0.5,
	})
	if got != 0 {
		t.Errorf("over-saturated streak should clamp to 0; got %v", got)
	}
}

// ── BuildEpicStreak ─────────────────────────────────────────────

func TestBuildEpicStreak_SingleCloseStartsStreak(t *testing.T) {
	got := BuildEpicStreak([]core.WorkItemID{"gm-epic-A"})
	if got.CurrentEpicID != "gm-epic-A" || got.ContiguousCount != 1 {
		t.Errorf("got %+v", got)
	}
}

func TestBuildEpicStreak_FiveSameEpicCloses(t *testing.T) {
	got := BuildEpicStreak([]core.WorkItemID{
		"gm-epic-A", "gm-epic-A", "gm-epic-A", "gm-epic-A", "gm-epic-A",
	})
	if got.ContiguousCount != 5 {
		t.Errorf("count = %d, want 5", got.ContiguousCount)
	}
}

func TestBuildEpicStreak_DifferentEpicBreaksStreak(t *testing.T) {
	// newest first: A, A, B, A — streak is A,A then B breaks it.
	got := BuildEpicStreak([]core.WorkItemID{
		"gm-epic-A", "gm-epic-A", "gm-epic-B", "gm-epic-A",
	})
	if got.CurrentEpicID != "gm-epic-A" {
		t.Errorf("current = %q, want gm-epic-A", got.CurrentEpicID)
	}
	if got.ContiguousCount != 2 {
		t.Errorf("count = %d, want 2 (only the newest A,A)", got.ContiguousCount)
	}
}

func TestBuildEpicStreak_EmptyEpicsSkipped(t *testing.T) {
	// "" rows are no-signal; they should neither start nor break.
	got := BuildEpicStreak([]core.WorkItemID{
		"gm-epic-A", "", "gm-epic-A", "",
	})
	if got.ContiguousCount != 2 {
		t.Errorf("count = %d, want 2", got.ContiguousCount)
	}
}

func TestBuildEpicStreak_EmptyHistoryIsZero(t *testing.T) {
	got := BuildEpicStreak(nil)
	if got.CurrentEpicID != "" || got.ContiguousCount != 0 {
		t.Errorf("got %+v, want zero", got)
	}
}

// Spec example: "1 if candidate is sibling, decays per-turn, hard 0
// once a different epic has been contiguously worked." Pin the
// behaviour with a scenario test.
func TestEpicAffinity_SpecScenario(t *testing.T) {
	// Session has closed 3 in epic A; next candidate in A scores
	// at 1 - 0.1*2 = 0.8.
	got := EpicAffinity("gm-epic-A", BuildEpicStreak([]core.WorkItemID{
		"gm-epic-A", "gm-epic-A", "gm-epic-A",
	}))
	if math.Abs(got-0.8) > 1e-9 {
		t.Errorf("got %v, want 0.8", got)
	}

	// Same session, but a candidate in epic B → hard 0.
	got = EpicAffinity("gm-epic-B", BuildEpicStreak([]core.WorkItemID{
		"gm-epic-A", "gm-epic-A", "gm-epic-A",
	}))
	if got != 0 {
		t.Errorf("different-epic candidate = %v, want 0", got)
	}

	// Streak switched: most recent close was B, before that A,A.
	// Candidate in A → 0 because streak's current epic is B.
	got = EpicAffinity("gm-epic-A", BuildEpicStreak([]core.WorkItemID{
		"gm-epic-B", "gm-epic-A", "gm-epic-A",
	}))
	if got != 0 {
		t.Errorf("after epic switch, candidate = %v, want 0", got)
	}
}
