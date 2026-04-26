package enrichment

import (
	"reflect"
	"testing"
)

func TestEnrichment_AddTargetSortsAndDedupes(t *testing.T) {
	e := Enrichment{BeadID: "gm-1"}
	e = e.AddTarget("internal/auth/")
	e = e.AddTarget("./web/src/Topbar.tsx")
	e = e.AddTarget("internal/auth/") // dup
	e = e.AddTarget("internal/auth")  // not dup — different normalized form
	want := []string{"internal/auth", "internal/auth/", "web/src/Topbar.tsx"}
	if !reflect.DeepEqual(e.Targets, want) {
		t.Errorf("Targets = %v, want %v", e.Targets, want)
	}
}

func TestEnrichment_RemoveTargetIsIdempotent(t *testing.T) {
	e := Enrichment{BeadID: "gm-1", Targets: []string{"a.go", "b.go"}}
	e = e.RemoveTarget("a.go")
	e = e.RemoveTarget("a.go") // already gone — no error path expected
	if !reflect.DeepEqual(e.Targets, []string{"b.go"}) {
		t.Errorf("Targets = %v, want [b.go]", e.Targets)
	}
}

func TestEnrichment_SetTargetsReplacesAndNormalizes(t *testing.T) {
	e := Enrichment{BeadID: "gm-1", Targets: []string{"old.go"}}
	e = e.SetTargets([]string{"./new.go", "new.go", " spaced.go "})
	want := []string{"new.go", "spaced.go"}
	if !reflect.DeepEqual(e.Targets, want) {
		t.Errorf("Targets = %v, want %v", e.Targets, want)
	}
}

func TestEnrichment_AddConceptCanonicalizes(t *testing.T) {
	e := Enrichment{BeadID: "gm-1"}
	e = e.AddConcept("React Query")
	e = e.AddConcept("react-query") // dup of canonical form
	e = e.AddConcept("AUTH")
	want := []string{"auth", "react-query"}
	if !reflect.DeepEqual(e.Concepts, want) {
		t.Errorf("Concepts = %v, want %v", e.Concepts, want)
	}
}

func TestEnrichment_RemoveConceptCanonicalizes(t *testing.T) {
	e := Enrichment{BeadID: "gm-1", Concepts: []string{"react-query"}}
	e = e.RemoveConcept("React-Query") // case-insensitive removal
	if len(e.Concepts) != 0 {
		t.Errorf("Concepts should be empty after canonical-form removal: %v", e.Concepts)
	}
}

func TestEnrichment_MutatorReturnsCopy(t *testing.T) {
	// Mutators must not alias the input slice — callers depend on
	// the immutable-style API to compose pipelines.
	original := Enrichment{BeadID: "gm-1", Targets: []string{"a"}}
	updated := original.AddTarget("b")
	if len(original.Targets) != 1 {
		t.Errorf("original mutated by AddTarget: %v", original.Targets)
	}
	if len(updated.Targets) != 2 {
		t.Errorf("updated should have 2 targets: %v", updated.Targets)
	}
}

func TestEnrichment_IsZero(t *testing.T) {
	if !(Enrichment{BeadID: "x"}).IsZero() {
		t.Error("empty targets+concepts should be IsZero")
	}
	if (Enrichment{BeadID: "x", Targets: []string{"a"}}).IsZero() {
		t.Error("with targets should not be IsZero")
	}
	if (Enrichment{BeadID: "x", Concepts: []string{"a"}}).IsZero() {
		t.Error("with concepts should not be IsZero")
	}
}

func TestNormalizeTarget(t *testing.T) {
	cases := map[string]string{
		"":                   "",
		"   ":                "",
		"./internal/auth":    "internal/auth",
		"internal//auth/":    "internal/auth/",
		"  /etc/foo  ":       "/etc/foo",
		"web/src/App.tsx":    "web/src/App.tsx",
	}
	for in, want := range cases {
		if got := normalizeTarget(in); got != want {
			t.Errorf("normalizeTarget(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeConcept(t *testing.T) {
	cases := map[string]string{
		"":             "",
		"react-query":  "react-query",
		"React Query":  "react-query",
		"react/query":  "react-query",
		"React.Query":  "react-query",
		"AUTH":         "auth",
		"  trailing- ": "trailing",
	}
	for in, want := range cases {
		if got := normalizeConcept(in); got != want {
			t.Errorf("normalizeConcept(%q) = %q, want %q", in, got, want)
		}
	}
}
