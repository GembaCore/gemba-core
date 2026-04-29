package dod

import (
	"strings"
	"testing"

	"github.com/GembaCore/gemba-core/core"
)

func TestNeedsDefault(t *testing.T) {
	if !NeedsDefault(core.WorkItem{}) {
		t.Error("empty item: want true")
	}
	if !NeedsDefault(core.WorkItem{DoD: &core.DefinitionOfDone{}}) {
		t.Error("empty dod struct: want true")
	}
	if NeedsDefault(core.WorkItem{DoD: &core.DefinitionOfDone{AcceptanceCriteria: []string{"x"}}}) {
		t.Error("with criterion: want false")
	}
	if NeedsDefault(core.WorkItem{DoD: &core.DefinitionOfDone{Notes: "hand-written"}}) {
		t.Error("with notes: want false")
	}
}

func TestSynthesizeTaskDefault(t *testing.T) {
	got := Synthesize(core.WorkItem{Kind: "task"})
	if len(got.AcceptanceCriteria) < 2 {
		t.Errorf("task criteria too thin: %+v", got.AcceptanceCriteria)
	}
	if !hasLike(got.AcceptanceCriteria, "code pushed") {
		t.Errorf("missing 'code pushed' in task criteria: %+v", got.AcceptanceCriteria)
	}
	if got.Version != "synthesized-v1" {
		t.Errorf("version: %q", got.Version)
	}
}

func TestSynthesizeBugAddsRegression(t *testing.T) {
	got := Synthesize(core.WorkItem{Kind: "bug"})
	if !hasLike(got.AcceptanceCriteria, "regression test") {
		t.Errorf("bug missing regression criterion: %+v", got.AcceptanceCriteria)
	}
}

func TestSynthesizeEpicReferencesChildren(t *testing.T) {
	got := Synthesize(core.WorkItem{Kind: "epic"})
	if !hasLike(got.AcceptanceCriteria, "child bead") {
		t.Errorf("epic missing child DoD reference: %+v", got.AcceptanceCriteria)
	}
}

func TestSynthesizeLabelsAddCriteria(t *testing.T) {
	got := Synthesize(core.WorkItem{
		Kind:   "task",
		Labels: []string{"surface:frontend", "risk:high"},
	})
	if !hasLike(got.AcceptanceCriteria, "SPA tests") {
		t.Errorf("frontend label missing SPA tests: %+v", got.AcceptanceCriteria)
	}
	if !hasLike(got.AcceptanceCriteria, "peer review") {
		t.Errorf("risk:high label missing peer review: %+v", got.AcceptanceCriteria)
	}
}

func TestSynthesizeDeterministicUnderLabelReordering(t *testing.T) {
	a := Synthesize(core.WorkItem{
		Kind:   "task",
		Labels: []string{"surface:frontend", "risk:high"},
	})
	b := Synthesize(core.WorkItem{
		Kind:   "task",
		Labels: []string{"risk:high", "surface:frontend"},
	})
	if !equalStrings(a.AcceptanceCriteria, b.AcceptanceCriteria) {
		t.Errorf("label order must not change output:\n a=%+v\n b=%+v",
			a.AcceptanceCriteria, b.AcceptanceCriteria)
	}
}

func TestSynthesizeDedupesCriteria(t *testing.T) {
	// Frontend + backend + task all push "code pushed"-style messages;
	// dedupe prevents the preamble from reading like a broken record.
	got := Synthesize(core.WorkItem{
		Kind:   "task",
		Labels: []string{"surface:frontend", "surface:backend"},
	})
	seen := make(map[string]int)
	for _, c := range got.AcceptanceCriteria {
		seen[c]++
	}
	for c, n := range seen {
		if n > 1 {
			t.Errorf("dup criterion %q (count %d)", c, n)
		}
	}
}

func hasLike(xs []string, needle string) bool {
	for _, x := range xs {
		if strings.Contains(x, needle) {
			return true
		}
	}
	return false
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
