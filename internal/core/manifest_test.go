package core

import (
	"encoding/json"
	"strings"
	"testing"
)

// baseManifest returns a minimal Validate()-clean manifest for derivation
// tests. Each sub-test copies it and toggles one field so the test only
// exercises the projection rule under examination.
func baseManifest() CapabilityManifest {
	return CapabilityManifest{
		AdaptorName:     "beads",
		AdaptorVersion:  "0.1.0",
		ProtocolVersion: "v1",
		Transport:       TransportJSONL,
		StateMap:        StateMap{"open": StateUnstarted},
	}
}

func TestFlagsFromEmptyManifest(t *testing.T) {
	m := baseManifest()
	f := m.Flags()

	if !f.HasDeclaredState {
		t.Error("HasDeclaredState should be true for a manifest with a non-empty StateMap")
	}
	// A Validate()-clean manifest with no optional fields set projects
	// to every capability flag being false (except HasDeclaredState).
	if f.HasSprints || f.HasBudget || f.HasEvidence ||
		f.HasExtensionEdges || f.HasExtensionFields || f.HasExtensionRelFields {
		t.Errorf("unexpected truthy flag on empty manifest: %+v", f)
	}
}

func TestFlagsSprintNative(t *testing.T) {
	m := baseManifest()
	if m.Flags().HasSprints {
		t.Fatal("HasSprints must default to false")
	}
	m.SprintNative = true
	if !m.Flags().HasSprints {
		t.Error("HasSprints must follow SprintNative")
	}
}

func TestFlagsTokenBudget(t *testing.T) {
	m := baseManifest()
	if m.Flags().HasBudget {
		t.Fatal("HasBudget must default to false")
	}
	m.TokenBudgetEnforced = true
	if !m.Flags().HasBudget {
		t.Error("HasBudget must follow TokenBudgetEnforced")
	}
}

func TestFlagsEvidenceSynthesis(t *testing.T) {
	m := baseManifest()
	if m.Flags().HasEvidence {
		t.Fatal("HasEvidence must default to false")
	}
	m.EvidenceSynthesisRequired = true
	if !m.Flags().HasEvidence {
		t.Error("HasEvidence must follow EvidenceSynthesisRequired")
	}
}

func TestFlagsDeclaredState(t *testing.T) {
	m := baseManifest()
	if !m.Flags().HasDeclaredState {
		t.Fatal("HasDeclaredState must reflect a non-empty StateMap")
	}
	m.StateMap = nil
	if m.Flags().HasDeclaredState {
		t.Error("HasDeclaredState must be false when StateMap is nil")
	}
	m.StateMap = StateMap{}
	if m.Flags().HasDeclaredState {
		t.Error("HasDeclaredState must be false when StateMap is empty")
	}
}

func TestFlagsExtensionEdges(t *testing.T) {
	m := baseManifest()
	if m.Flags().HasExtensionEdges {
		t.Fatal("HasExtensionEdges must default to false")
	}
	m.EdgeExtensions = []EdgeExtension{{Name: "duplicates", Directed: true}}
	if !m.Flags().HasExtensionEdges {
		t.Error("HasExtensionEdges must be true when EdgeExtensions is non-empty")
	}
}

func TestFlagsExtensionFields(t *testing.T) {
	m := baseManifest()
	if m.Flags().HasExtensionFields {
		t.Fatal("HasExtensionFields must default to false")
	}
	m.FieldExtensions = []FieldExtension{{Name: "story_points", Type: "number"}}
	if !m.Flags().HasExtensionFields {
		t.Error("HasExtensionFields must be true when FieldExtensions is non-empty")
	}
}

func TestFlagsExtensionRelFields(t *testing.T) {
	m := baseManifest()
	if m.Flags().HasExtensionRelFields {
		t.Fatal("HasExtensionRelFields must default to false")
	}
	m.RelationshipExtensions = []RelationshipExtension{{Name: "confidence", Type: "number"}}
	if !m.Flags().HasExtensionRelFields {
		t.Error("HasExtensionRelFields must be true when RelationshipExtensions is non-empty")
	}
}

func TestFlagsIsPure(t *testing.T) {
	m := baseManifest()
	m.SprintNative = true
	m.TokenBudgetEnforced = true
	m.EdgeExtensions = []EdgeExtension{{Name: "duplicates"}}

	got := m.Flags()
	if got != m.Flags() {
		t.Error("Flags() must be deterministic across repeated calls")
	}
	// Mutating the returned Flags must not affect subsequent calls.
	got.HasSprints = false
	if !m.Flags().HasSprints {
		t.Error("Flags() must return a fresh value each call")
	}
}

func TestFlagsJSONTagsSnakeCase(t *testing.T) {
	// Wire-format parity with the TS mirror: every field must serialize
	// as snake_case so the codegen test in codegen_test.go can grep for
	// the tag in CoreTypesTS.
	f := Flags{
		HasSprints:            true,
		HasBudget:             true,
		HasEvidence:           true,
		HasDeclaredState:      true,
		HasExtensionEdges:     true,
		HasExtensionFields:    true,
		HasExtensionRelFields: true,
	}
	data, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := []string{
		`"has_sprints":true`,
		`"has_budget":true`,
		`"has_evidence":true`,
		`"has_declared_state":true`,
		`"has_extension_edges":true`,
		`"has_extension_fields":true`,
		`"has_extension_rel_fields":true`,
	}
	got := string(data)
	for _, substr := range want {
		if !strings.Contains(got, substr) {
			t.Errorf("Flags JSON missing %s; got %s", substr, got)
		}
	}
}
