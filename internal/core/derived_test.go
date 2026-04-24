package core

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

// manifestWithStateMap is the shared "adaptor declared its states"
// manifest used by the truth table. Tests that want to exercise the
// HasDeclaredState=false branch construct their own zero-StateMap
// manifest inline.
var manifestWithStateMap = CapabilityManifest{
	AdaptorName:     "test",
	ProtocolVersion: "v1",
	Transport:       TransportAPI,
	StateMap: StateMap{
		"open":        StateUnstarted,
		"in_progress": StateStarted,
		"closed":      StateCompleted,
	},
}

func openEsc(source EscalationKind, urgency EscalationUrgency) EscalationRequest {
	return EscalationRequest{
		ID:        "e1",
		Source:    source,
		Urgency:   urgency,
		State:     EscalationOpen,
		Title:     "t",
		Prompt:    "p",
		CreatedAt: time.Unix(1_700_000_000, 0).UTC(),
	}
}

// TestDeriveTruthTable walks the canonical inputs and asserts the
// three-boolean output. Each row is a single case; the comment names
// the rule being exercised.
func TestDeriveTruthTable(t *testing.T) {
	humanRef := &AgentRef{ID: "gemba/crew/mike", Name: "mike", Kind: AgentKindHuman}

	cases := []struct {
		name       string
		item       WorkItem
		esc        []EscalationRequest
		manifest   CapabilityManifest
		wantClaim  bool
		wantHuman  bool
		wantReview bool
	}{
		{
			name:      "unstarted + unassigned + no escalations + declared state → claimable",
			item:      WorkItem{StateCategory: StateUnstarted},
			manifest:  manifestWithStateMap,
			wantClaim: true,
		},
		{
			name:     "unstarted + assignee set → NOT claimable",
			item:     WorkItem{StateCategory: StateUnstarted, Assignee: humanRef},
			manifest: manifestWithStateMap,
		},
		{
			name:     "backlog → NOT claimable (only unstarted qualifies)",
			item:     WorkItem{StateCategory: StateBacklog},
			manifest: manifestWithStateMap,
		},
		{
			name:     "started → NOT claimable",
			item:     WorkItem{StateCategory: StateStarted},
			manifest: manifestWithStateMap,
		},
		{
			name:     "completed → NOT claimable",
			item:     WorkItem{StateCategory: StateCompleted},
			manifest: manifestWithStateMap,
		},
		{
			name:     "empty StateMap → NOT claimable even if otherwise claimable",
			item:     WorkItem{StateCategory: StateUnstarted},
			manifest: CapabilityManifest{},
		},
		{
			name:       "open blocking HITL approval → human + review, and not claimable",
			item:       WorkItem{StateCategory: StateUnstarted},
			esc:        []EscalationRequest{openEsc(EscalationHITLApproval, UrgencyBlocking)},
			manifest:   manifestWithStateMap,
			wantHuman:  true,
			wantReview: true,
		},
		{
			name:      "open blocking orchestrator pause → human only (not a review source)",
			item:      WorkItem{StateCategory: StateStarted},
			esc:       []EscalationRequest{openEsc(EscalationOrchestratorPause, UrgencyBlocking)},
			manifest:  manifestWithStateMap,
			wantHuman: true,
		},
		{
			name:       "open advisory permission prompt → review only (not blocking)",
			item:       WorkItem{StateCategory: StateStarted},
			esc:        []EscalationRequest{openEsc(EscalationPermissionPrompt, UrgencyAdvisory)},
			manifest:   manifestWithStateMap,
			wantReview: true,
		},
		{
			name:       "open blocking MCP elicitation → human + review",
			item:       WorkItem{StateCategory: StateStarted},
			esc:        []EscalationRequest{openEsc(EscalationMCPElicitation, UrgencyBlocking)},
			manifest:   manifestWithStateMap,
			wantHuman:  true,
			wantReview: true,
		},
		{
			name:       "open blocking A2A input-required → human + review",
			item:       WorkItem{StateCategory: StateStarted},
			esc:        []EscalationRequest{openEsc(EscalationA2AInputRequired, UrgencyBlocking)},
			manifest:   manifestWithStateMap,
			wantHuman:  true,
			wantReview: true,
		},
		{
			// gm-97w7.1: Manager-authored "## Blockers" section. In
			// balanced/cautious mode the scanner stamps Urgency=Blocking.
			name:       "open blocking transcript blocker → human + review",
			item:       WorkItem{StateCategory: StateStarted},
			esc:        []EscalationRequest{openEsc(EscalationBlocker, UrgencyBlocking)},
			manifest:   manifestWithStateMap,
			wantHuman:  true,
			wantReview: true,
		},
		{
			// gm-97w7.1: Coach "## Questions" in balanced mode (advisory).
			// Review-worthy but not blocking.
			name:       "open advisory transcript question → review only",
			item:       WorkItem{StateCategory: StateStarted},
			esc:        []EscalationRequest{openEsc(EscalationQuestion, UrgencyAdvisory)},
			manifest:   manifestWithStateMap,
			wantReview: true,
		},
		{
			// gm-97w7.1: "## Questions" in cautious mode (blocking).
			name:       "open blocking transcript question → human + review",
			item:       WorkItem{StateCategory: StateStarted},
			esc:        []EscalationRequest{openEsc(EscalationQuestion, UrgencyBlocking)},
			manifest:   manifestWithStateMap,
			wantHuman:  true,
			wantReview: true,
		},
		{
			name: "resolved HITL escalation → ignored (state != open)",
			item: WorkItem{StateCategory: StateUnstarted},
			esc: []EscalationRequest{
				{
					ID: "e1", Source: EscalationHITLApproval,
					Urgency: UrgencyBlocking, State: EscalationResolved,
				},
			},
			manifest:  manifestWithStateMap,
			wantClaim: true, // still claimable: the only escalation is closed
		},
		{
			name: "canceled escalation → ignored",
			item: WorkItem{StateCategory: StateStarted},
			esc: []EscalationRequest{
				{
					ID: "e1", Source: EscalationHITLApproval,
					Urgency: UrgencyBlocking, State: EscalationCanceled,
				},
			},
			manifest: manifestWithStateMap,
		},
		{
			name: "multiple open escalations → signals aggregate (OR)",
			item: WorkItem{StateCategory: StateStarted},
			esc: []EscalationRequest{
				openEsc(EscalationOrchestratorPause, UrgencyBlocking),
				openEsc(EscalationPermissionPrompt, UrgencyAdvisory),
			},
			manifest:   manifestWithStateMap,
			wantHuman:  true,
			wantReview: true,
		},
		{
			name:      "open blocking pause on unstarted → blocks claim",
			item:      WorkItem{StateCategory: StateUnstarted},
			esc:       []EscalationRequest{openEsc(EscalationOrchestratorPause, UrgencyBlocking)},
			manifest:  manifestWithStateMap,
			wantHuman: true,
			// AgentClaimable must be false because a blocking escalation is open
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Derive(tc.item, tc.esc, tc.manifest)
			want := DerivedSignals{
				AgentClaimable:      tc.wantClaim,
				HumanActionRequired: tc.wantHuman,
				ReviewPending:       tc.wantReview,
			}
			if got != want {
				t.Errorf("Derive mismatch:\n  got:  %+v\n  want: %+v", got, want)
			}
		})
	}
}

// TestDerivePure asserts Derive does not mutate its inputs. The rules
// are pure per the contract; a regression that starts sorting the
// escalation slice in place would silently break call sites that share
// the slice across concurrent readers.
func TestDerivePure(t *testing.T) {
	item := WorkItem{StateCategory: StateUnstarted}
	esc := []EscalationRequest{
		openEsc(EscalationHITLApproval, UrgencyBlocking),
	}
	original := append([]EscalationRequest(nil), esc...)

	_ = Derive(item, esc, manifestWithStateMap)

	if !reflect.DeepEqual(esc, original) {
		t.Errorf("Derive mutated escalations slice:\n  got:  %+v\n  want: %+v", esc, original)
	}
}

// TestDerivedSignalsRoundTrip asserts the wire form matches the Go
// field tags exactly so adaptors and the SPA agree on the keys.
func TestDerivedSignalsRoundTrip(t *testing.T) {
	in := DerivedSignals{
		AgentClaimable:      true,
		HumanActionRequired: false,
		ReviewPending:       true,
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var generic map[string]bool
	if err := json.Unmarshal(data, &generic); err != nil {
		t.Fatalf("decode generic: %v", err)
	}
	for _, k := range []string{"agent_claimable", "human_action_required", "review_pending"} {
		if _, ok := generic[k]; !ok {
			t.Errorf("key %q missing from wire form: %s", k, data)
		}
	}

	var round DerivedSignals
	if err := json.Unmarshal(data, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if round != in {
		t.Errorf("round trip mismatch:\n  got:  %+v\n  want: %+v", round, in)
	}
}

// TestWorkItemDerivedOmitempty asserts the `derived` key does not
// appear on a WorkItem that has no DerivedSignals attached (keeps
// existing wire formats identical when the server opts not to populate
// the field).
func TestWorkItemDerivedOmitempty(t *testing.T) {
	wi := WorkItem{
		ID: "w/r/a", Kind: "task", Title: "t", Status: "open",
		StateCategory: StateUnstarted,
		CreatedAt:     time.Unix(0, 0).UTC(),
		UpdatedAt:     time.Unix(0, 0).UTC(),
	}
	data, err := json.Marshal(wi)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var generic map[string]any
	if err := json.Unmarshal(data, &generic); err != nil {
		t.Fatalf("decode generic: %v", err)
	}
	if _, ok := generic["derived"]; ok {
		t.Errorf("derived key should be omitted when nil: %s", data)
	}
}

// TestWorkItemDerivedRoundTrip asserts Derived serialises and
// deserialises when populated.
func TestWorkItemDerivedRoundTrip(t *testing.T) {
	ts := time.Unix(1_700_000_000, 0).UTC()
	wi := WorkItem{
		ID: "w/r/a", Kind: "task", Title: "t", Status: "open",
		StateCategory: StateUnstarted,
		CreatedAt:     ts, UpdatedAt: ts,
		Derived: &DerivedSignals{AgentClaimable: true},
	}
	data, err := json.Marshal(wi)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var round WorkItem
	if err := json.Unmarshal(data, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(wi, round) {
		t.Errorf("round trip mismatch:\n  got:  %+v\n  want: %+v", round, wi)
	}
}

// TestAgentRefDialectOmitempty ensures Dialect absent → no key on the
// wire; Dialect present → string carried through.
func TestAgentRefDialectOmitempty(t *testing.T) {
	a := AgentRef{ID: "x/y/z", Name: "n", Kind: AgentKindAgent}
	data, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var generic map[string]any
	if err := json.Unmarshal(data, &generic); err != nil {
		t.Fatalf("decode generic: %v", err)
	}
	if _, ok := generic["dialect"]; ok {
		t.Errorf("dialect should be omitted when nil: %s", data)
	}

	dialect := "claude"
	a.Dialect = &dialect
	data, err = json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal with dialect: %v", err)
	}
	var round AgentRef
	if err := json.Unmarshal(data, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if round.Dialect == nil || *round.Dialect != "claude" {
		t.Errorf("dialect not round-tripped: got %+v", round.Dialect)
	}
}

// TestAgentRefDialectAcceptsUnknownValues asserts the field is
// free-form (not enum-validated) so adaptors can introduce new
// dialects without a core schema bump.
func TestAgentRefDialectAcceptsUnknownValues(t *testing.T) {
	raw := `{"id":"x/y/z","name":"n","agent_kind":"agent","dialect":"some-new-dialect-2099"}`
	var a AgentRef
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		t.Fatalf("unknown dialect should decode cleanly: %v", err)
	}
	if a.Dialect == nil || *a.Dialect != "some-new-dialect-2099" {
		t.Errorf("dialect not preserved: got %+v", a.Dialect)
	}
}
