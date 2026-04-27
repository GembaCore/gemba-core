// Pure-function tests for AgeByDays + daysBetween (gm-v5z2.2).

package agentprofile

import (
	"math"
	"testing"
	"time"

	"github.com/MikeBengtson/gemba/internal/planner"
)

func TestAgeByDays_NilInIsNilOut(t *testing.T) {
	if got := AgeByDays(nil, 1, 14); got != nil {
		t.Errorf("nil → nil; got %v", got)
	}
	if got := AgeFilesByDays(nil, 1, 14); got != nil {
		t.Errorf("nil → nil; got %v", got)
	}
}

func TestAgeByDays_ZeroDaysIsCopy(t *testing.T) {
	in := map[planner.ConceptTag]float64{"auth": 1.0, "spa": 0.5}
	got := AgeByDays(in, 0, 14)
	if len(got) != 2 || got["auth"] != 1.0 || got["spa"] != 0.5 {
		t.Errorf("zero-days should not change weights; got %v", got)
	}
	// Mutating output must not affect input.
	got["auth"] = 9
	if in["auth"] != 1.0 {
		t.Error("AgeByDays returned a shared map; mutation leaked")
	}
}

func TestAgeByDays_NegativeDaysIsClamped(t *testing.T) {
	// Clock skew can never PROMOTE weights — same as zero-days.
	in := map[planner.ConceptTag]float64{"auth": 1.0}
	got := AgeByDays(in, -3, 14)
	if got["auth"] != 1.0 {
		t.Errorf("negative days should clamp to 0; got %f", got["auth"])
	}
}

func TestAgeByDays_HalfLifeMatchesSpec(t *testing.T) {
	// At days == halfLife, every weight halves.
	in := map[planner.ConceptTag]float64{"auth": 1.0, "spa": 0.4}
	got := AgeByDays(in, 14, 14)
	if math.Abs(got["auth"]-0.5) > 1e-9 {
		t.Errorf("auth at half-life: got %f, want 0.5", got["auth"])
	}
	if math.Abs(got["spa"]-0.2) > 1e-9 {
		t.Errorf("spa at half-life: got %f, want 0.2", got["spa"])
	}
}

func TestAgeByDays_TwoHalfLivesQuarters(t *testing.T) {
	in := map[planner.ConceptTag]float64{"x": 1.0}
	got := AgeByDays(in, 28, 14)
	if math.Abs(got["x"]-0.25) > 1e-9 {
		t.Errorf("two half-lives: got %f, want 0.25", got["x"])
	}
}

func TestAgeByDays_ZeroHalfLifeFallsBack(t *testing.T) {
	// halfLife <= 0 should default to DefaultDecayHalfLifeDays.
	in := map[planner.ConceptTag]float64{"x": 1.0}
	got := AgeByDays(in, DefaultDecayHalfLifeDays, 0)
	if math.Abs(got["x"]-0.5) > 1e-9 {
		t.Errorf("zero half-life should use default; got %f", got["x"])
	}
}

func TestAgeFilesByDays_HappyPath(t *testing.T) {
	in := map[string]float64{"src/auth.go": 0.8}
	got := AgeFilesByDays(in, 14, 14)
	if math.Abs(got["src/auth.go"]-0.4) > 1e-9 {
		t.Errorf("file decay at half-life: got %f, want 0.4", got["src/auth.go"])
	}
}

func TestDaysBetween_BasicCases(t *testing.T) {
	now := time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name  string
		since time.Time
		want  float64
	}{
		{"zero since → 0", time.Time{}, 0},
		{"future since → 0", now.Add(time.Hour), 0},
		{"exactly 1 day", now.Add(-24 * time.Hour), 1},
		{"half a day", now.Add(-12 * time.Hour), 0.5},
		{"7 days", now.Add(-7 * 24 * time.Hour), 7},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := daysBetween(now, c.since); math.Abs(got-c.want) > 1e-9 {
				t.Errorf("daysBetween(%v): got %f want %f", c.since, got, c.want)
			}
		})
	}
}
