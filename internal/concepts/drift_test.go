package concepts

import (
	"testing"
	"time"
)

func TestDetectDrift_NearDuplicateAtThreshold(t *testing.T) {
	// rq + react-query: same 4 beads → Jaccard 1.0, ratio 1.0.
	beads := []BeadConcepts{
		{BeadID: "b1", Concepts: []string{"rq", "react-query"}},
		{BeadID: "b2", Concepts: []string{"rq", "react-query", "auth"}},
		{BeadID: "b3", Concepts: []string{"rq", "react-query"}},
		{BeadID: "b4", Concepts: []string{"rq", "react-query"}},
	}
	d := DetectDrift(beads, DefaultDriftOpts())
	if len(d.NearDuplicates) != 1 {
		t.Fatalf("expected 1 near-duplicate; got %d (%+v)", len(d.NearDuplicates), d.NearDuplicates)
	}
	nd := d.NearDuplicates[0]
	wantA, wantB := "react-query", "rq"
	if (nd.A != wantA || nd.B != wantB) && (nd.A != wantB || nd.B != wantA) {
		t.Errorf("near-duplicate pair = (%q, %q), want %q + %q", nd.A, nd.B, wantA, wantB)
	}
	if nd.Jaccard < 0.999 {
		t.Errorf("Jaccard = %f, want ~1.0", nd.Jaccard)
	}
}

func TestDetectDrift_BelowJaccardThreshold(t *testing.T) {
	// auth + rq overlap on one bead out of three each → Jaccard ~0.2.
	beads := []BeadConcepts{
		{BeadID: "b1", Concepts: []string{"auth"}},
		{BeadID: "b2", Concepts: []string{"auth"}},
		{BeadID: "b3", Concepts: []string{"auth", "rq"}},
		{BeadID: "b4", Concepts: []string{"rq"}},
		{BeadID: "b5", Concepts: []string{"rq"}},
	}
	d := DetectDrift(beads, DefaultDriftOpts())
	if len(d.NearDuplicates) != 0 {
		t.Errorf("low-overlap pairs should not flag: %+v", d.NearDuplicates)
	}
}

func TestDetectDrift_UseRatioGuardsAgainstAsymmetry(t *testing.T) {
	// "core" used 10 times, "fluke" used twice — both overlap on the
	// two fluke beads → Jaccard 2/10=0.2 (already too low). Tighten
	// the case so Jaccard passes but the use-ratio guard catches it.
	beads := []BeadConcepts{}
	// core only: 6 beads
	for i := 0; i < 6; i++ {
		beads = append(beads, BeadConcepts{BeadID: id("c", i), Concepts: []string{"core"}})
	}
	// shared: 2 beads (puts core's use count at 8, fluke at 2)
	beads = append(beads,
		BeadConcepts{BeadID: "shared-1", Concepts: []string{"core", "fluke"}},
		BeadConcepts{BeadID: "shared-2", Concepts: []string{"core", "fluke"}},
	)
	d := DetectDrift(beads, DriftOpts{
		NearDuplicateJaccard:  0.2,  // very permissive — would otherwise flag
		NearDuplicateUseRatio: 0.5,  // 2/8 = 0.25 < 0.5 → guard rejects
	})
	for _, nd := range d.NearDuplicates {
		if (nd.A == "core" && nd.B == "fluke") || (nd.A == "fluke" && nd.B == "core") {
			t.Errorf("use-ratio guard should reject asymmetric pair: %+v", nd)
		}
	}
}

func TestDetectDrift_SingletonOnlyWhenDormant(t *testing.T) {
	closedRecently := time.Now().UTC().Add(-5 * 24 * time.Hour)
	closedLongAgo := time.Now().UTC().Add(-120 * 24 * time.Hour) // > 90d (default dormant)
	beads := []BeadConcepts{
		{BeadID: "b1", Concepts: []string{"fresh-singleton"}, ClosedAt: &closedRecently},
		{BeadID: "b2", Concepts: []string{"stale-singleton"}, ClosedAt: &closedLongAgo},
		{BeadID: "b3", Concepts: []string{"open-singleton"}}, // ClosedAt nil
	}
	d := DetectDrift(beads, DefaultDriftOpts())
	terms := make(map[string]bool)
	for _, s := range d.Singletons {
		terms[s.Term] = true
	}
	if !terms["stale-singleton"] {
		t.Errorf("90-day-closed singleton should flag: %+v", d.Singletons)
	}
	if terms["fresh-singleton"] {
		t.Errorf("5-day-closed singleton should NOT flag: %+v", d.Singletons)
	}
	if terms["open-singleton"] {
		t.Errorf("open-bead singleton should NOT flag: %+v", d.Singletons)
	}
}

func TestDetectDrift_DormantDaysDisableGate(t *testing.T) {
	// SingletonDormantDays=0 → every singleton flags, even open ones.
	beads := []BeadConcepts{
		{BeadID: "b1", Concepts: []string{"open-singleton"}},
	}
	d := DetectDrift(beads, DriftOpts{SingletonDormantDays: 0})
	if len(d.Singletons) != 1 {
		t.Errorf("dormant=0 should flag all singletons: %+v", d.Singletons)
	}
}

func TestDetectDrift_DeterministicOutput(t *testing.T) {
	// Same input must produce the same output across runs — required
	// so the suggestion-queue dedup works across invocations.
	beads := []BeadConcepts{
		{BeadID: "b1", Concepts: []string{"a", "b"}},
		{BeadID: "b2", Concepts: []string{"a", "b"}},
		{BeadID: "b3", Concepts: []string{"a", "b"}},
	}
	d1 := DetectDrift(beads, DefaultDriftOpts())
	d2 := DetectDrift(beads, DefaultDriftOpts())
	if len(d1.NearDuplicates) != len(d2.NearDuplicates) {
		t.Errorf("nondeterministic output: %+v vs %+v", d1.NearDuplicates, d2.NearDuplicates)
	}
}

func TestDetectDrift_NormalizesConceptNames(t *testing.T) {
	// "React-Query" and "react-query" must collapse before the
	// detector compares anything.
	beads := []BeadConcepts{
		{BeadID: "b1", Concepts: []string{"React-Query", "react query"}},
	}
	d := DetectDrift(beads, DefaultDriftOpts())
	// One distinct term; no pair to flag.
	if len(d.NearDuplicates) != 0 {
		t.Errorf("normalization should fold variants; got %+v", d.NearDuplicates)
	}
}

func id(prefix string, i int) string {
	return prefix + "-" + itoa(i)
}
