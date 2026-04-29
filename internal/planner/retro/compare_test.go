// Tests for the retrospective comparator (gm-s47n.8.1).
//
// Each scenario reads as a table: declared inputs → expected diff.
// The comparator is a pure function so the tests are pure data —
// no fixtures, no mocks.

package retro

import (
	"reflect"
	"testing"

	"github.com/GembaCore/gemba-core/internal/planner"
	"github.com/GembaCore/gemba-core/internal/planner/targets"
)

// ── target comparison ────────────────────────────────────────────

func TestCompare_PerfectLiteralMatch(t *testing.T) {
	d := Compare(
		Declared{Targets: []targets.Pattern{"src/auth.go", "src/handlers.go"}},
		Actual{Files: []string{"src/auth.go", "src/handlers.go"}},
	)
	if len(d.UnmatchedDeclared) != 0 {
		t.Errorf("expected no unmatched declared; got %v", d.UnmatchedDeclared)
	}
	if len(d.UndeclaredActual) != 0 {
		t.Errorf("expected no undeclared actual; got %v", d.UndeclaredActual)
	}
	if d.TargetDivergence != 0 {
		t.Errorf("expected 0 divergence on perfect match; got %f", d.TargetDivergence)
	}
}

func TestCompare_GlobMatchesMultipleFiles(t *testing.T) {
	// `src/auth/**` covers both files → divergence 0 (perfect).
	d := Compare(
		Declared{Targets: []targets.Pattern{"src/auth/**"}},
		Actual{Files: []string{"src/auth/login.go", "src/auth/session.go"}},
	)
	if len(d.UnmatchedDeclared) != 0 {
		t.Errorf("expected glob to match; got unmatched %v", d.UnmatchedDeclared)
	}
	if len(d.UndeclaredActual) != 0 {
		t.Errorf("expected all files covered; got undeclared %v", d.UndeclaredActual)
	}
	if d.TargetDivergence != 0 {
		t.Errorf("expected 0 divergence; got %f", d.TargetDivergence)
	}
	if len(d.MatchedTargets) != 1 || len(d.MatchedTargets[0].MatchedFiles) != 2 {
		t.Errorf("expected 1 covered pattern with 2 files; got %+v", d.MatchedTargets)
	}
}

func TestCompare_OverDeclared(t *testing.T) {
	// Declared 2 patterns, only 1 was touched.
	d := Compare(
		Declared{Targets: []targets.Pattern{"src/auth.go", "src/never_touched.go"}},
		Actual{Files: []string{"src/auth.go"}},
	)
	if len(d.UnmatchedDeclared) != 1 || d.UnmatchedDeclared[0] != "src/never_touched.go" {
		t.Errorf("expected src/never_touched.go unmatched; got %v", d.UnmatchedDeclared)
	}
	if len(d.UndeclaredActual) != 0 {
		t.Errorf("expected no undeclared actual; got %v", d.UndeclaredActual)
	}
	// Jaccard: |{auth} ∩ {auth, never_touched}| / |union| = 1/2 → distance 0.5
	if d.TargetDivergence != 0.5 {
		t.Errorf("expected 0.5 divergence on 1-of-2 match; got %f", d.TargetDivergence)
	}
}

func TestCompare_UnderDeclared(t *testing.T) {
	d := Compare(
		Declared{Targets: []targets.Pattern{"src/auth.go"}},
		Actual{Files: []string{"src/auth.go", "src/handlers.go", "src/middleware.go"}},
	)
	if len(d.UnmatchedDeclared) != 0 {
		t.Errorf("expected all declared matched; got unmatched %v", d.UnmatchedDeclared)
	}
	want := []string{"src/handlers.go", "src/middleware.go"}
	if !reflect.DeepEqual(d.UndeclaredActual, want) {
		t.Errorf("expected undeclared %v; got %v", want, d.UndeclaredActual)
	}
}

func TestCompare_NoOverlap(t *testing.T) {
	d := Compare(
		Declared{Targets: []targets.Pattern{"src/auth.go"}},
		Actual{Files: []string{"src/handlers.go"}},
	)
	if len(d.MatchedTargets) != 0 {
		t.Errorf("expected no matched patterns; got %+v", d.MatchedTargets)
	}
	if d.TargetDivergence != 1.0 {
		t.Errorf("expected divergence 1.0 on disjoint sets; got %f", d.TargetDivergence)
	}
}

func TestCompare_EmptyBothSidesIsZeroDivergence(t *testing.T) {
	d := Compare(Declared{}, Actual{})
	if d.TargetDivergence != 0 {
		t.Errorf("vacuous match should be divergence 0; got %f", d.TargetDivergence)
	}
	if d.ConceptDivergence != 0 {
		t.Errorf("vacuous concepts should be divergence 0; got %f", d.ConceptDivergence)
	}
}

func TestCompare_EmptyDeclaredOnly(t *testing.T) {
	d := Compare(Declared{}, Actual{Files: []string{"src/auth.go"}})
	if d.TargetDivergence != 1.0 {
		t.Errorf("nothing declared but files touched should be 1.0; got %f", d.TargetDivergence)
	}
	if len(d.UndeclaredActual) != 1 {
		t.Errorf("expected 1 undeclared; got %v", d.UndeclaredActual)
	}
}

func TestCompare_EmptyActualOnly(t *testing.T) {
	d := Compare(Declared{Targets: []targets.Pattern{"src/auth.go"}}, Actual{})
	if d.TargetDivergence != 1.0 {
		t.Errorf("declared but nothing touched should be 1.0; got %f", d.TargetDivergence)
	}
	if len(d.UnmatchedDeclared) != 1 {
		t.Errorf("expected 1 unmatched; got %v", d.UnmatchedDeclared)
	}
}

func TestCompare_PathNormalisation(t *testing.T) {
	// './src/auth.go' should match 'src/auth.go' — normalisation
	// folds the leading './'.
	d := Compare(
		Declared{Targets: []targets.Pattern{"./src/auth.go"}},
		Actual{Files: []string{"src/auth.go"}},
	)
	if d.TargetDivergence != 0 {
		t.Errorf("path normalisation should treat './src/x' == 'src/x'; got divergence %f", d.TargetDivergence)
	}
}

// ── concept comparison ───────────────────────────────────────────

func TestCompare_ConceptsPerfectMatch(t *testing.T) {
	d := Compare(
		Declared{Concepts: []planner.ConceptTag{"auth", "session"}},
		Actual{Concepts: []planner.ConceptTag{"auth", "session"}},
	)
	if d.ConceptDivergence != 0 {
		t.Errorf("expected 0 concept divergence; got %f", d.ConceptDivergence)
	}
	if len(d.MissingConcepts) != 0 || len(d.NewConcepts) != 0 {
		t.Errorf("expected no missing/new; got missing=%v new=%v", d.MissingConcepts, d.NewConcepts)
	}
}

func TestCompare_ConceptsMissing(t *testing.T) {
	// Declared 'session' was never actually surfaced.
	d := Compare(
		Declared{Concepts: []planner.ConceptTag{"auth", "session"}},
		Actual{Concepts: []planner.ConceptTag{"auth"}},
	)
	if !reflect.DeepEqual(d.MissingConcepts, []planner.ConceptTag{"session"}) {
		t.Errorf("expected missing=[session]; got %v", d.MissingConcepts)
	}
	if len(d.NewConcepts) != 0 {
		t.Errorf("expected no new concepts; got %v", d.NewConcepts)
	}
	// 1 ∩ 2 = 1; union = 2 → distance = 0.5
	if d.ConceptDivergence != 0.5 {
		t.Errorf("expected divergence 0.5; got %f", d.ConceptDivergence)
	}
}

func TestCompare_NewConcepts(t *testing.T) {
	d := Compare(
		Declared{Concepts: []planner.ConceptTag{"auth"}},
		Actual{Concepts: []planner.ConceptTag{"auth", "ratelimit"}},
	)
	if !reflect.DeepEqual(d.NewConcepts, []planner.ConceptTag{"ratelimit"}) {
		t.Errorf("expected new=[ratelimit]; got %v", d.NewConcepts)
	}
}

func TestCompare_ConceptsDuplicatesCollapse(t *testing.T) {
	d := Compare(
		Declared{Concepts: []planner.ConceptTag{"auth", "auth"}},
		Actual{Concepts: []planner.ConceptTag{"auth"}},
	)
	if len(d.KeptConcepts) != 1 {
		t.Errorf("duplicates should collapse; got %v", d.KeptConcepts)
	}
	if d.ConceptDivergence != 0 {
		t.Errorf("expected 0 divergence after dedup; got %f", d.ConceptDivergence)
	}
}

// ── HasDrift ─────────────────────────────────────────────────────

func TestHasDrift_AboveThreshold(t *testing.T) {
	d := Diff{TargetDivergence: 0.6, ConceptDivergence: 0.2}
	if !d.HasDrift(0.5) {
		t.Error("expected drift at threshold 0.5 when target divergence is 0.6")
	}
}

func TestHasDrift_BelowThreshold(t *testing.T) {
	d := Diff{TargetDivergence: 0.3, ConceptDivergence: 0.4}
	if d.HasDrift(0.5) {
		t.Error("expected no drift when both axes below threshold")
	}
}

func TestHasDrift_BoundaryNotInclusive(t *testing.T) {
	// HasDrift uses strict `>` so a divergence exactly at the
	// threshold is NOT drift. Operators should set thresholds
	// just below the warn point to capture borderline cases.
	d := Diff{TargetDivergence: 0.5, ConceptDivergence: 0.5}
	if d.HasDrift(0.5) {
		t.Error("strict > comparison: 0.5 at threshold 0.5 should be no-drift")
	}
}

// ── ordering / determinism ───────────────────────────────────────

func TestCompare_OutputsAreSorted(t *testing.T) {
	d := Compare(
		Declared{
			Targets:  []targets.Pattern{"z.go", "a.go", "m.go"},
			Concepts: []planner.ConceptTag{"z", "a", "m"},
		},
		Actual{
			Files:    []string{"z.go", "y.go", "b.go"},
			Concepts: []planner.ConceptTag{"a", "n"},
		},
	)
	wantUnmatched := []targets.Pattern{"a.go", "m.go"}
	if !reflect.DeepEqual(d.UnmatchedDeclared, wantUnmatched) {
		t.Errorf("unmatched declared not sorted: got %v", d.UnmatchedDeclared)
	}
	wantUndeclared := []string{"b.go", "y.go"}
	if !reflect.DeepEqual(d.UndeclaredActual, wantUndeclared) {
		t.Errorf("undeclared actual not sorted: got %v", d.UndeclaredActual)
	}
	wantMissing := []planner.ConceptTag{"m", "z"}
	if !reflect.DeepEqual(d.MissingConcepts, wantMissing) {
		t.Errorf("missing concepts not sorted: got %v", d.MissingConcepts)
	}
}
