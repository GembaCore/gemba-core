package bd

import (
	"testing"

	"github.com/MikeBengtson/gemba/internal/core"
)

func TestTargetsFromLabels_PreservesOrderAndDeduplicates(t *testing.T) {
	got := targetsFromLabels([]string{
		"target:web/src/**",
		"unrelated",
		"target:internal/auth/**",
		"target:web/src/**", // dupe — dropped
		"target:",           // empty — dropped
	})
	want := []string{"web/src/**", "internal/auth/**"}
	if !equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestConceptsFromLabels_BasicProjection(t *testing.T) {
	got := conceptsFromLabels([]string{
		"concept:auth-flow",
		"concept:budget-rollup",
		"target:not-a-concept",
	})
	want := []string{"auth-flow", "budget-rollup"}
	if !equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestDispatchStatusFromLabels_FirstCanonicalWins(t *testing.T) {
	cases := map[string]struct {
		in   []string
		want core.DispatchStatus
	}{
		"unset":             {nil, ""},
		"ready-explicit":    {[]string{"dispatch:ready"}, core.DispatchReady},
		"awaiting-design":   {[]string{"dispatch:awaiting-design"}, core.DispatchAwaitingDesign},
		"awaiting-vendor":   {[]string{"dispatch:awaiting-vendor"}, core.DispatchAwaitingVendor},
		"awaiting-review":   {[]string{"dispatch:awaiting-review"}, core.DispatchAwaitingReview},
		"not-now":           {[]string{"dispatch:not-now"}, core.DispatchNotNow},
		"unknown-dropped":   {[]string{"dispatch:bogus"}, ""},
		"first-wins":        {[]string{"dispatch:awaiting-vendor", "dispatch:ready"}, core.DispatchAwaitingVendor},
		"unknown-then-good": {[]string{"dispatch:bogus", "dispatch:not-now"}, core.DispatchNotNow},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := dispatchStatusFromLabels(tc.in); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestEstimatedSizeFromLabels_FirstCanonicalWins(t *testing.T) {
	cases := map[string]struct {
		in   []string
		want core.EstimatedSize
	}{
		"unset":         {nil, ""},
		"small":         {[]string{"size:small"}, core.SizeSmall},
		"medium":        {[]string{"size:medium"}, core.SizeMedium},
		"large":         {[]string{"size:large"}, core.SizeLarge},
		"unknown-drops": {[]string{"size:enormous"}, ""},
		"first-wins":    {[]string{"size:large", "size:small"}, core.SizeLarge},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := estimatedSizeFromLabels(tc.in); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestEnrichmentLabels_DoNotCollideWithSiblingLabelTypes(t *testing.T) {
	// All four enrichment label families coexisting with the
	// existing read:/write:/repo:/branch:/type:milestone surface —
	// each parser must filter to its own prefix without bleeding.
	labels := []string{
		"target:web/src/**",
		"concept:auth",
		"dispatch:awaiting-review",
		"size:large",
		"read:~/.aws/credentials",
		"write:/var/log/myapp",
		"repo:gemba",
		"branch:gemba=main",
		"type:milestone",
	}
	if got := targetsFromLabels(labels); !equal(got, []string{"web/src/**"}) {
		t.Errorf("targets bled: %v", got)
	}
	if got := conceptsFromLabels(labels); !equal(got, []string{"auth"}) {
		t.Errorf("concepts bled: %v", got)
	}
	if got := dispatchStatusFromLabels(labels); got != core.DispatchAwaitingReview {
		t.Errorf("dispatch bled: %v", got)
	}
	if got := estimatedSizeFromLabels(labels); got != core.SizeLarge {
		t.Errorf("size bled: %v", got)
	}
}

// End-to-end: a Bead with the four enrichment label families
// projects onto a WorkItem whose typed enrichment fields carry the
// parsed values. Pins the Layer 0 schema slice gm-s47n.1.1
// integration with toWorkItem.
func TestBeadToWorkItem_PopulatesEnrichmentFields(t *testing.T) {
	b := &Bead{
		ID:        "p27",
		Title:     "UI spec gate",
		Status:    "open",
		IssueType: "task",
		Labels: []string{
			"target:web/src/**",
			"target:docs/ui-spec.md",
			"concept:ui-spec",
			"concept:gate",
			"dispatch:awaiting-design",
			"size:large",
		},
	}
	wi := b.toWorkItem("gm", nil)
	if !equal(wi.Targets, []string{"web/src/**", "docs/ui-spec.md"}) {
		t.Errorf("Targets = %v", wi.Targets)
	}
	if !equal(wi.Concepts, []string{"ui-spec", "gate"}) {
		t.Errorf("Concepts = %v", wi.Concepts)
	}
	if wi.DispatchStatus != core.DispatchAwaitingDesign {
		t.Errorf("DispatchStatus = %q, want awaiting-design", wi.DispatchStatus)
	}
	if wi.EstimatedSize != core.SizeLarge {
		t.Errorf("EstimatedSize = %q, want large", wi.EstimatedSize)
	}
}

// A Bead with no enrichment labels projects onto a WorkItem whose
// fields are zero values — consumers normalise via Effective() to
// the planner-friendly defaults (DispatchReady / SizeMedium).
func TestBeadToWorkItem_OmitsEnrichmentWhenLabelsAbsent(t *testing.T) {
	b := &Bead{
		ID: "x", Title: "naked", Status: "open", IssueType: "task",
		Labels: []string{"area:test"},
	}
	wi := b.toWorkItem("gm", nil)
	if len(wi.Targets) != 0 || len(wi.Concepts) != 0 {
		t.Errorf("expected empty Targets+Concepts; got %v / %v", wi.Targets, wi.Concepts)
	}
	if wi.DispatchStatus != "" {
		t.Errorf("DispatchStatus = %q, want empty (consumer normalises to ready)", wi.DispatchStatus)
	}
	if wi.EstimatedSize != "" {
		t.Errorf("EstimatedSize = %q, want empty", wi.EstimatedSize)
	}
}

func equal(a, b []string) bool {
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
