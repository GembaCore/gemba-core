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

// gm-ekr: MinimumBar classifies a conforming manifest as at-bar.
func TestMinimumBar_ConformingAdaptor(t *testing.T) {
	m := baseManifest()
	m.AgentSessionDecoupling = true
	m.AgentNativeAPI = AgentAPICLI
	ok, reasons := m.MinimumBar()
	if !ok {
		t.Fatalf("want at-bar; got reasons=%v", reasons)
	}
	if len(reasons) != 0 {
		t.Errorf("at-bar manifest should produce no reasons; got %v", reasons)
	}
}

// gm-ekr: agent_session_decoupling=false is the canonical below-bar
// reason. Without it, the adaptor isn't an agentic data plane.
func TestMinimumBar_MissingSessionDecoupling(t *testing.T) {
	m := baseManifest() // AgentSessionDecoupling defaults to false
	ok, reasons := m.MinimumBar()
	if ok {
		t.Fatal("want below-bar when AgentSessionDecoupling is false")
	}
	joined := strings.Join(reasons, "|")
	if !strings.Contains(joined, "agent_session_decoupling") {
		t.Errorf("reasons should mention agent_session_decoupling; got %v", reasons)
	}
}

// gm-ekr: agent_native_api="rest-only" is below bar — no agent-native
// entry point.
func TestMinimumBar_RESTOnlyAPI(t *testing.T) {
	m := baseManifest()
	m.AgentSessionDecoupling = true
	m.AgentNativeAPI = AgentAPIRESTOnly
	ok, reasons := m.MinimumBar()
	if ok {
		t.Fatal("want below-bar when AgentNativeAPI is rest-only")
	}
	joined := strings.Join(reasons, "|")
	if !strings.Contains(joined, "rest-only") {
		t.Errorf("reasons should mention rest-only; got %v", reasons)
	}
}

// gm-ekr: Validate accepts manifests that leave the new R1–R8 fields
// empty (older adaptors pre-gm-ekr).
func TestValidate_AcceptsEmptyR1R8(t *testing.T) {
	m := baseManifest()
	if err := m.Validate(); err != nil {
		t.Errorf("Validate should accept empty R1-R8: %v", err)
	}
}

// gm-ekr: Validate rejects unknown enum values — a malformed token
// would silently mishandle downstream consumers.
func TestValidate_RejectsUnknownEnums(t *testing.T) {
	cases := []struct {
		name  string
		mutate func(*CapabilityManifest)
	}{
		{"schema_enforcement", func(m *CapabilityManifest) {
			m.SchemaEnforcement = SchemaEnforcement("wobble")
		}},
		{"query_languages", func(m *CapabilityManifest) {
			m.QueryLanguages = []QueryLanguage{QueryFilterOnly, "nonsense"}
		}},
		{"versioning_transport", func(m *CapabilityManifest) {
			m.VersioningTransport = []VersioningTransport{"carrier-pigeon"}
		}},
		{"concurrency_model", func(m *CapabilityManifest) {
			m.ConcurrencyModel = ConcurrencyModel("vibes")
		}},
		{"agent_native_api", func(m *CapabilityManifest) {
			m.AgentNativeAPI = AgentNativeAPI("telepathy")
		}},
		{"orchestrator_hooks", func(m *CapabilityManifest) {
			m.OrchestratorHooks = []OrchestratorHook{HookClaimAtomic, "sneaky"}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := baseManifest()
			c.mutate(&m)
			err := m.Validate()
			if err == nil {
				t.Fatalf("%s: want validation error for unknown enum; got nil", c.name)
			}
			if !strings.Contains(err.Error(), c.name) && !strings.Contains(err.Error(), "not recognised") && !strings.Contains(err.Error(), "unknown") {
				t.Errorf("%s: error should mention the field or 'unknown'; got %q", c.name, err.Error())
			}
		})
	}
}

// gm-ekr: Validate accepts a fully populated manifest matching the
// projected Beads shape.
func TestValidate_AcceptsConformingR1R8(t *testing.T) {
	m := baseManifest()
	m.SchemaEnforcement = SchemaNative
	m.QueryLanguages = []QueryLanguage{QueryFilterOnly, QuerySQLSubset}
	m.DependencyGraphNative = true
	m.ReadySetQuery = true
	m.VersioningTransport = []VersioningTransport{VersioningGit, VersioningDolt, VersioningJSONL}
	m.ConcurrencyModel = ConcurrencyDoltMerge
	m.AgentSessionDecoupling = true
	m.AgentNativeAPI = AgentAPICLI
	m.OrchestratorHooks = []OrchestratorHook{
		HookReadySetSubscribe, HookClaimAtomic,
		HookEscalationIngest, HookWorkCompleteAck,
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("conforming manifest rejected: %v", err)
	}
	if ok, reasons := m.MinimumBar(); !ok {
		t.Errorf("conforming manifest should clear the bar; got reasons %v", reasons)
	}
}
