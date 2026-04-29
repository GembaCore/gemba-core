package bd

import (
	"testing"

	"github.com/GembaCore/gemba-core/core"
)

// gm-e12.22.1 — matchesFilter handles IncludeTemplates + IncludeWisps.
// Default behavior is to hide both; the Workflow surface opts in.

func TestMatchesFilter_HidesTemplatesByDefault(t *testing.T) {
	wi := core.WorkItem{
		ID:     "gemba/gemba/gm-tmpl-1",
		Labels: []string{"template", "type:workflow"},
	}

	if matchesFilter(wi, core.WorkItemFilter{}) {
		t.Error("template-labelled bead should be hidden by default")
	}
	if !matchesFilter(wi, core.WorkItemFilter{IncludeTemplates: true}) {
		t.Error("template should pass when IncludeTemplates=true")
	}
}

func TestMatchesFilter_HidesWispsByDefault(t *testing.T) {
	wi := core.WorkItem{
		ID:     "gemba/gemba/wisp-abc-1",
		Labels: []string{},
	}

	if matchesFilter(wi, core.WorkItemFilter{}) {
		t.Error("wisp-id bead should be hidden by default")
	}
	if !matchesFilter(wi, core.WorkItemFilter{IncludeWisps: true}) {
		t.Error("wisp should pass when IncludeWisps=true")
	}
}

func TestMatchesFilter_IncludeFlagsCombine(t *testing.T) {
	template := core.WorkItem{
		ID:     "gemba/gemba/gm-tmpl-1",
		Labels: []string{"template"},
	}
	wisp := core.WorkItem{
		ID: "gemba/gemba/wisp-abc-1",
	}
	regular := core.WorkItem{
		ID: "gemba/gemba/gm-task-1",
	}

	// Default: only the regular bead passes.
	if matchesFilter(template, core.WorkItemFilter{}) {
		t.Error("template should be hidden by default")
	}
	if matchesFilter(wisp, core.WorkItemFilter{}) {
		t.Error("wisp should be hidden by default")
	}
	if !matchesFilter(regular, core.WorkItemFilter{}) {
		t.Error("regular bead should pass by default")
	}

	// Both opt-ins on: every kind passes.
	f := core.WorkItemFilter{IncludeTemplates: true, IncludeWisps: true}
	if !matchesFilter(template, f) {
		t.Error("template should pass with IncludeTemplates")
	}
	if !matchesFilter(wisp, f) {
		t.Error("wisp should pass with IncludeWisps")
	}
	if !matchesFilter(regular, f) {
		t.Error("regular bead should pass")
	}
}

func TestHasTemplateLabel(t *testing.T) {
	cases := []struct {
		labels []string
		want   bool
	}{
		{[]string{"template"}, true},
		{[]string{"type:workflow", "template"}, true},
		{[]string{"template-not-this-one"}, false},
		{[]string{}, false},
		{nil, false},
	}
	for _, tc := range cases {
		got := hasTemplateLabel(core.WorkItem{Labels: tc.labels})
		if got != tc.want {
			t.Errorf("labels=%v: got %v want %v", tc.labels, got, tc.want)
		}
	}
}

func TestIsWispID(t *testing.T) {
	cases := map[string]bool{
		"gemba/gemba/wisp-1":    true,
		"wisp-only":             true,
		"gemba/wisp-mid/path":   true,
		"gemba/gemba/gm-task-1": false,
		"gemba/gemba/wisper":    true, // substring match — by design, mirrors the gastown shader's rule
		"":                      false,
	}
	for id, want := range cases {
		got := isWispID(core.WorkItemID(id))
		if got != want {
			t.Errorf("isWispID(%q) = %v, want %v", id, got, want)
		}
	}
}
