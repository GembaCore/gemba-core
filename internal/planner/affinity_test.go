package planner

import (
	"math"
	"testing"

	"github.com/MikeBengtson/gemba/core"
)

const eps = 1e-9

func approx(t *testing.T, label string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > eps {
		t.Errorf("%s: got %v, want %v", label, got, want)
	}
}

func TestAffinity_NilContextEverythingZeroExceptHeadroom(t *testing.T) {
	// Empty context: no profile, no workspace, no health. Concept
	// + file + workspace + recency all 0. Headroom defaults to 1
	// (nil health = no signal = assume room). Combined =
	// 0.15 * 1.0 = 0.15 by default weights.
	got := Affinity(
		AffinityBeadInputs{BeadID: "gm-1", Concepts: []ConceptTag{"auth"}},
		OperationalContext{},
		nil,
	)
	approx(t, "concept_overlap", got.ConceptOverlap, 0)
	approx(t, "file_familiarity", got.FileFamiliarity, 0)
	approx(t, "workspace_match", got.WorkspaceMatch, 0)
	approx(t, "recency", got.Recency, 0)
	approx(t, "headroom", got.Headroom, 1.0)
	approx(t, "combined", got.Combined, 0.15)
}

func TestAffinity_ConceptOverlap_ProfileSharesAllTags(t *testing.T) {
	// Bead has 1 concept; profile has the same concept with
	// weight 1. Cosine = 1 / (sqrt(1) * sqrt(1)) = 1.
	profile := &SessionProfile{
		Concepts: map[ConceptTag]float64{"auth": 1.0},
	}
	got := Affinity(
		AffinityBeadInputs{BeadID: "gm-1", Concepts: []ConceptTag{"auth"}},
		OperationalContext{Profile: profile},
		nil,
	)
	approx(t, "concept_overlap", got.ConceptOverlap, 1.0)
}

func TestAffinity_ConceptOverlap_NoSharedTagsZero(t *testing.T) {
	profile := &SessionProfile{
		Concepts: map[ConceptTag]float64{"spa-routing": 0.8},
	}
	got := Affinity(
		AffinityBeadInputs{BeadID: "gm-1", Concepts: []ConceptTag{"auth"}},
		OperationalContext{Profile: profile},
		nil,
	)
	approx(t, "concept_overlap", got.ConceptOverlap, 0)
}

func TestAffinity_FileFamiliarity_AveragesAcrossTargets(t *testing.T) {
	// Two targets; profile has weight 0.6 for one and 0.0 for the
	// other (absent). Average = (0.6 + 0) / 2 = 0.3.
	profile := &SessionProfile{
		Files: map[string]float64{"src/auth.go": 0.6},
	}
	got := Affinity(
		AffinityBeadInputs{
			BeadID:  "gm-1",
			Targets: []string{"src/auth.go", "src/login.go"},
		},
		OperationalContext{Profile: profile},
		nil,
	)
	approx(t, "file_familiarity", got.FileFamiliarity, 0.3)
}

func TestAffinity_FileFamiliarity_PathCanonicalisation(t *testing.T) {
	// Profile stored "src/auth.go"; bead targets "./src/auth.go".
	// Cleaned key matches; familiarity is 1.0.
	profile := &SessionProfile{
		Files: map[string]float64{"src/auth.go": 1.0},
	}
	got := Affinity(
		AffinityBeadInputs{BeadID: "gm-1", Targets: []string{"./src/auth.go"}},
		OperationalContext{Profile: profile},
		nil,
	)
	approx(t, "file_familiarity", got.FileFamiliarity, 1.0)
}

func TestAffinity_WorkspaceMatch_SameRepoAndBranch(t *testing.T) {
	got := Affinity(
		AffinityBeadInputs{
			BeadID:       "gm-1",
			Repositories: []string{"gemba"},
			Branch:       "main",
		},
		OperationalContext{Workspace: &core.Workspace{Repository: "gemba", Branch: "main"}},
		nil,
	)
	approx(t, "workspace_match", got.WorkspaceMatch, 1.0)
}

func TestAffinity_WorkspaceMatch_SameRepoDifferentBranchHalf(t *testing.T) {
	got := Affinity(
		AffinityBeadInputs{
			BeadID:       "gm-1",
			Repositories: []string{"gemba"},
			Branch:       "feature/x",
		},
		OperationalContext{Workspace: &core.Workspace{Repository: "gemba", Branch: "main"}},
		nil,
	)
	approx(t, "workspace_match", got.WorkspaceMatch, 0.5)
}

func TestAffinity_WorkspaceMatch_DifferentRepoZero(t *testing.T) {
	got := Affinity(
		AffinityBeadInputs{
			BeadID:       "gm-1",
			Repositories: []string{"gemba"},
		},
		OperationalContext{Workspace: &core.Workspace{Repository: "lume"}},
		nil,
	)
	approx(t, "workspace_match", got.WorkspaceMatch, 0)
}

func TestAffinity_WorkspaceMatch_MultiRepoTakesMax(t *testing.T) {
	// Bead declares two repos; one matches the workspace exactly,
	// one doesn't. Score is the max (1.0).
	got := Affinity(
		AffinityBeadInputs{
			BeadID:       "gm-1",
			Repositories: []string{"unrelated", "gemba"},
			Branch:       "main",
		},
		OperationalContext{Workspace: &core.Workspace{Repository: "gemba", Branch: "main"}},
		nil,
	)
	approx(t, "workspace_match (multi-repo)", got.WorkspaceMatch, 1.0)
}

func TestAffinity_Headroom_BelowHalfFull(t *testing.T) {
	got := Affinity(
		AffinityBeadInputs{BeadID: "gm-1"},
		OperationalContext{Health: &SessionHealth{ContextPressure: 0.4}},
		nil,
	)
	approx(t, "headroom (low pressure)", got.Headroom, 1.0)
}

func TestAffinity_Headroom_HardZeroAtNinetyPercent(t *testing.T) {
	got := Affinity(
		AffinityBeadInputs{BeadID: "gm-1"},
		OperationalContext{Health: &SessionHealth{ContextPressure: 0.9}},
		nil,
	)
	approx(t, "headroom (cliff)", got.Headroom, 0)
}

func TestAffinity_Headroom_MidRangeLinearDecay(t *testing.T) {
	// Spec §5.4: decay from 1 at 0.5 to 0 at 0.85.
	// At 0.675 (midpoint): 1 - (0.675 - 0.5) / 0.35 = 0.5.
	got := Affinity(
		AffinityBeadInputs{BeadID: "gm-1"},
		OperationalContext{Health: &SessionHealth{ContextPressure: 0.675}},
		nil,
	)
	approx(t, "headroom (mid)", got.Headroom, 0.5)
}

func TestAffinity_Recency_SharedConceptScores(t *testing.T) {
	// Profile has the bead's concept and a non-empty LastBeads
	// ring → recency fires.
	profile := &SessionProfile{
		Concepts:  map[ConceptTag]float64{"auth": 0.4},
		LastBeads: []core.WorkItemID{"gm-prev"},
	}
	got := Affinity(
		AffinityBeadInputs{BeadID: "gm-1", Concepts: []ConceptTag{"auth"}},
		OperationalContext{Profile: profile},
		nil,
	)
	approx(t, "recency (hit)", got.Recency, 1.0)
}

func TestAffinity_Recency_EmptyRingZero(t *testing.T) {
	profile := &SessionProfile{
		Concepts: map[ConceptTag]float64{"auth": 0.4},
		// No LastBeads.
	}
	got := Affinity(
		AffinityBeadInputs{BeadID: "gm-1", Concepts: []ConceptTag{"auth"}},
		OperationalContext{Profile: profile},
		nil,
	)
	approx(t, "recency (empty ring)", got.Recency, 0)
}

func TestAffinity_CombinedWithDefaultWeights(t *testing.T) {
	// All-1 sub-scores under the default weights → combined = 1.
	// (0.30 + 0.20 + 0.20 + 0.15 + 0.15 = 1.0)
	profile := &SessionProfile{
		Concepts:  map[ConceptTag]float64{"auth": 1.0},
		Files:     map[string]float64{"src/auth.go": 1.0},
		LastBeads: []core.WorkItemID{"gm-prev"},
	}
	got := Affinity(
		AffinityBeadInputs{
			BeadID:       "gm-1",
			Concepts:     []ConceptTag{"auth"},
			Targets:      []string{"src/auth.go"},
			Repositories: []string{"gemba"},
			Branch:       "main",
		},
		OperationalContext{
			Profile:   profile,
			Workspace: &core.Workspace{Repository: "gemba", Branch: "main"},
			Health:    &SessionHealth{ContextPressure: 0.1},
		},
		nil,
	)
	approx(t, "combined (all-1)", got.Combined, 1.0)
}

func TestAffinity_CustomWeightsApply(t *testing.T) {
	// Same all-1 sub-scores; custom weights all 0.2 → combined 1.0.
	profile := &SessionProfile{
		Concepts:  map[ConceptTag]float64{"auth": 1.0},
		Files:     map[string]float64{"src/auth.go": 1.0},
		LastBeads: []core.WorkItemID{"gm-prev"},
	}
	got := Affinity(
		AffinityBeadInputs{
			BeadID:       "gm-1",
			Concepts:     []ConceptTag{"auth"},
			Targets:      []string{"src/auth.go"},
			Repositories: []string{"gemba"},
			Branch:       "main",
		},
		OperationalContext{
			Profile:   profile,
			Workspace: &core.Workspace{Repository: "gemba", Branch: "main"},
			Health:    &SessionHealth{ContextPressure: 0.1},
		},
		&AffinityWeights{
			ConceptOverlap:  0.5, // Heavy concept emphasis
			FileFamiliarity: 0.2,
			WorkspaceMatch:  0.1,
			Recency:         0.1,
			Headroom:        0.1,
		},
	)
	approx(t, "combined (custom weights)", got.Combined, 1.0)
}

func TestAffinity_AllScoresInRange(t *testing.T) {
	// Sweep across plausible inputs; every sub-score MUST stay in
	// [0, 1] regardless of degenerate values (negative weights,
	// huge concept weights, etc).
	profile := &SessionProfile{
		Concepts: map[ConceptTag]float64{"auth": 999.0}, // pathological weight
		Files:    map[string]float64{"a.go": 999.0},
	}
	got := Affinity(
		AffinityBeadInputs{
			BeadID:   "gm-1",
			Concepts: []ConceptTag{"auth"},
			Targets:  []string{"a.go"},
		},
		OperationalContext{
			Profile:   profile,
			Workspace: &core.Workspace{Repository: "x", Branch: "y"},
			Health:    &SessionHealth{ContextPressure: 1.0},
		},
		nil,
	)
	for _, v := range []float64{
		got.ConceptOverlap,
		got.FileFamiliarity,
		got.WorkspaceMatch,
		got.Recency,
		got.Headroom,
		got.Combined,
	} {
		if v < 0 || v > 1.001 {
			t.Errorf("score out of range: got %v from %+v", v, got)
		}
	}
}

func TestAffinity_DefaultWeightsSumToOne(t *testing.T) {
	w := DefaultAffinityWeights
	sum := w.ConceptOverlap + w.FileFamiliarity + w.WorkspaceMatch + w.Recency + w.Headroom
	approx(t, "default weights sum", sum, 1.0)
}
