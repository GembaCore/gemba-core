package core

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestOrchestrationManifestRoundTrip(t *testing.T) {
	rate := 0.008
	m := OrchestrationCapabilityManifest{
		AdaptorID:               "gastown",
		AdaptorVersion:          "0.1.0",
		OrchestrationAPIVersion: "1.0",
		Transport:               TransportJSONL,
		WorkspaceKinds:          []WorkspaceKind{WorkspaceWorktree},
		DefaultWorkspaceKind:    WorkspaceWorktree,
		PerKindIsolation: map[WorkspaceKind]IsolationCapabilities{
			WorkspaceWorktree: {FSScoped: true},
		},
		GroupModes:           []GroupMode{GroupStatic, GroupPool},
		AssignmentStrategies: []AssignmentStrategy{StrategyPull, StrategyHook},
		CostAxes:             []CostAxis{CostTokens, CostWallclock},
		NativeCostUnit:       "ACU",
		NativeCostToDollars:  &rate,
		EscalationKinds:      []EscalationKind{EscalationPermissionPrompt, EscalationHITLApproval},
		PeekModes:            []PeekMode{PeekTranscript},
		EventDelivery:        EventDeliverySSE,
		Extension:            map[string]any{"orchestrator": "mayor"},
	}

	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var round OrchestrationCapabilityManifest
	if err := json.Unmarshal(data, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !reflect.DeepEqual(m, round) {
		t.Errorf("round trip mismatch:\n got: %+v\nwant: %+v", round, m)
	}
}

func TestOrchestrationManifestRequiredAxesOnWire(t *testing.T) {
	// All six bead-required axes must appear on the wire even when
	// the caller supplied empty slices, so a parsing UI never has to
	// distinguish "absent" from "explicitly empty".
	m := OrchestrationCapabilityManifest{
		AdaptorID:               "noop",
		AdaptorVersion:          "0.0.1",
		OrchestrationAPIVersion: "1.0",
		Transport:               TransportAPI,
		WorkspaceKinds:          []WorkspaceKind{},
		GroupModes:              []GroupMode{},
		CostAxes:                []CostAxis{},
		EscalationKinds:         []EscalationKind{},
		PeekModes:               []PeekMode{},
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var generic map[string]any
	if err := json.Unmarshal(data, &generic); err != nil {
		t.Fatalf("decode generic: %v", err)
	}
	for _, k := range []string{
		"transport", "workspace_kinds", "group_modes",
		"cost_axes", "escalation_kinds", "peek_modes",
	} {
		if _, ok := generic[k]; !ok {
			t.Errorf("manifest key %q missing from wire form: %s", k, data)
		}
	}
}

func TestWorkspaceTopologyRoundTrip(t *testing.T) {
	ts := time.Date(2026, 4, 19, 0, 0, 0, 0, time.UTC)
	topo := WorkspaceTopology{
		Agents: []AgentRef{{
			ID: "gemba/polecats/topaz", Name: "topaz",
			Kind: AgentKindAgent, Role: "polecat",
		}},
		Groups: []AgentGroup{{
			ID:          "convoy-alpha",
			DisplayName: "Alpha convoy",
			Mode:        GroupStatic,
			Members:     GroupMembers{Static: []AgentID{"gemba/polecats/topaz"}},
		}},
		Workspaces: []Workspace{{
			ID:         "ws-1",
			Kind:       WorkspaceWorktree,
			Repository: "gemba",
			Status:     WorkspaceInUse,
			Isolation:  IsolationCapabilities{FSScoped: true},
			CreatedAt:  ts,
		}},
		Assignments: []Assignment{{
			ID:         "asg-1",
			WorkItemID: "gemba/gemba/gm-e3.3",
			AgentID:    "gemba/polecats/topaz",
			Status:     AssignmentActive,
		}},
		CapturedAt: ts,
	}

	data, err := json.Marshal(topo)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var round WorkspaceTopology
	if err := json.Unmarshal(data, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(topo, round) {
		t.Errorf("round trip mismatch:\n got: %+v\nwant: %+v", round, topo)
	}
}

func TestEscalationRequestRoundTrip(t *testing.T) {
	ts := time.Date(2026, 4, 19, 1, 0, 0, 0, time.UTC)
	deadline := ts.Add(1 * time.Hour)
	er := EscalationRequest{
		ID:         "esc-1",
		Source:     EscalationPermissionPrompt,
		WorkItemID: "gemba/gemba/gm-e3.3",
		AgentID:    "gemba/polecats/topaz",
		Urgency:    UrgencyBlocking,
		Title:      "allow git push?",
		Prompt:     "Agent wants to run `git push origin HEAD`. Allow?",
		Options: []EscalationOption{
			{Value: "yes", Label: "Allow"},
			{Value: "no", Label: "Deny"},
		},
		Deadline:  &deadline,
		State:     EscalationOpen,
		CreatedAt: ts,
	}
	data, err := json.Marshal(er)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var round EscalationRequest
	if err := json.Unmarshal(data, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(er, round) {
		t.Errorf("round trip mismatch:\n got: %+v\nwant: %+v", round, er)
	}
}

// TestSessionCloseReasonRoundTrip exercises every canonical reason so a
// drift in the enum (missing constant, typo in wire token) surfaces at
// the type boundary rather than inside a specific adaptor.
func TestSessionCloseReasonRoundTrip(t *testing.T) {
	reasons := []SessionCloseReason{
		CloseProviderExit,
		CloseTransportError,
		CloseUserStop,
		CloseIdleTimeout,
		CloseFatalStderr,
		CloseProtocolError,
		CloseBudgetStop,
		CloseEscalationPause,
	}
	for _, r := range reasons {
		if !r.Valid() {
			t.Errorf("reason %q should be valid", r)
		}
		data, err := json.Marshal(r)
		if err != nil {
			t.Fatalf("marshal %q: %v", r, err)
		}
		var round SessionCloseReason
		if err := json.Unmarshal(data, &round); err != nil {
			t.Errorf("unmarshal %q: %v", r, err)
		}
		if round != r {
			t.Errorf("roundtrip %q != %q", round, r)
		}
	}
}

func TestSessionCloseReasonRejectsUnknown(t *testing.T) {
	var r SessionCloseReason
	err := json.Unmarshal([]byte(`"panic_attack"`), &r)
	if err == nil {
		t.Fatalf("expected error decoding unknown reason, got nil (value=%q)", r)
	}
}

// TestSessionActiveTurnAndCloseReasonWire asserts that a Session
// carrying an active_turn_id and a close_reason survives a JSON round
// trip. Reapers and the UI key off these fields, so silent loss here is
// a high-impact drift.
func TestSessionActiveTurnAndCloseReasonWire(t *testing.T) {
	reason := CloseIdleTimeout
	end := time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC)
	s := Session{
		ID:           "sess-1",
		AssignmentID: "asg-1",
		AgentID:      "gemba/polecats/obsidian",
		Status:       SessionFailed,
		ActiveTurnID: "turn-7",
		StartedAt:    end.Add(-10 * time.Minute),
		EndedAt:      &end,
		CloseReason:  &reason,
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var round Session
	if err := json.Unmarshal(data, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if round.ActiveTurnID != "turn-7" {
		t.Errorf("active_turn_id lost: got %q", round.ActiveTurnID)
	}
	if round.CloseReason == nil || *round.CloseReason != CloseIdleTimeout {
		t.Errorf("close_reason lost: got %v", round.CloseReason)
	}
	// An idle session with no close_reason must serialise without the
	// key at all — consumers distinguish "closed with reason X" from
	// "still running" by field presence.
	open := Session{ID: "sess-2", AssignmentID: "asg-1", Status: SessionRunning, StartedAt: end}
	openData, err := json.Marshal(open)
	if err != nil {
		t.Fatalf("marshal open: %v", err)
	}
	if got := string(openData); containsStr(got, "close_reason") || containsStr(got, "active_turn_id") {
		t.Errorf("optional fields leaked into wire form of open session: %s", got)
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// TestOrchestrationAdaptorHasListPendingRequests is a compile-time
// guard — removing the method from the interface breaks this test
// before it breaks every adaptor implementation.
func TestOrchestrationAdaptorHasListPendingRequests(t *testing.T) {
	var a OrchestrationPlaneAdaptor = noopOrchestrator{}
	got, err := a.ListPendingRequests(context.Background(), "sess-1")
	if err != nil {
		t.Fatalf("ListPendingRequests: %v", err)
	}
	if got == nil {
		t.Errorf("ListPendingRequests must return non-nil slice (empty is fine)")
	}
}

// TestOrchestrationPlaneAdaptorIsImplementable is a compile-time guard:
// if the interface drifts in a way that makes it impossible to satisfy
// from a zero-dep stub, the package stops compiling here rather than
// failing downstream adaptor builds.
func TestOrchestrationPlaneAdaptorIsImplementable(t *testing.T) {
	var _ OrchestrationPlaneAdaptor = (*noopOrchestrator)(nil)
}

type noopOrchestrator struct{}

func (noopOrchestrator) Describe() OrchestrationCapabilityManifest {
	return OrchestrationCapabilityManifest{
		AdaptorID:               "noop",
		AdaptorVersion:          "0.0.1",
		OrchestrationAPIVersion: "1.0",
		Transport:               TransportAPI,
		WorkspaceKinds:          []WorkspaceKind{WorkspaceExec},
		GroupModes:              []GroupMode{GroupStatic},
		CostAxes:                []CostAxis{CostWallclock},
		EscalationKinds:         []EscalationKind{},
		PeekModes:               []PeekMode{},
	}
}

func (noopOrchestrator) DeclaredState(context.Context) (WorkspaceTopology, error) {
	return WorkspaceTopology{CapturedAt: time.Unix(0, 0).UTC()}, nil
}

func (noopOrchestrator) ObservedState(context.Context) (WorkspaceTopology, error) {
	return WorkspaceTopology{CapturedAt: time.Unix(0, 0).UTC()}, nil
}

func (noopOrchestrator) ListAgents(context.Context, AgentFilter) ([]AgentRef, error) {
	return nil, nil
}
func (noopOrchestrator) ReadAgent(context.Context, AgentID) (*AgentRef, error) { return nil, nil }
func (noopOrchestrator) ListGroups(context.Context) ([]AgentGroup, error)      { return nil, nil }
func (noopOrchestrator) ResolveGroupMembers(context.Context, string) ([]AgentRef, error) {
	return nil, nil
}
func (noopOrchestrator) ClaimNextReady(context.Context, ReadyFilter, AgentRef) (*Reservation, error) {
	return nil, nil
}
func (noopOrchestrator) ReleaseReservation(context.Context, string) error { return nil }
func (noopOrchestrator) StartSession(context.Context, string, SessionPrompt) (Session, error) {
	return Session{}, nil
}
func (noopOrchestrator) PauseSession(context.Context, string, ConfirmNonce) (Session, error) {
	return Session{}, nil
}
func (noopOrchestrator) ResumeSession(context.Context, string, ConfirmNonce) (Session, error) {
	return Session{}, nil
}
func (noopOrchestrator) EndSession(context.Context, string, SessionEndMode, ConfirmNonce) (Session, error) {
	return Session{}, nil
}
func (noopOrchestrator) PeekSession(context.Context, string) (SessionPeek, error) {
	return SessionPeek{}, nil
}
func (noopOrchestrator) ListPendingRequests(context.Context, string) ([]EscalationRequest, error) {
	return []EscalationRequest{}, nil
}
func (noopOrchestrator) AcquireWorkspace(context.Context, WorkspaceRequest) (Workspace, error) {
	return Workspace{}, nil
}
func (noopOrchestrator) ReleaseWorkspace(context.Context, string) error { return nil }
func (noopOrchestrator) InspectWorkspace(context.Context, string) (Workspace, error) {
	return Workspace{}, nil
}
func (noopOrchestrator) ListOpenEscalations(context.Context, EscalationFilter) ([]EscalationRequest, error) {
	return nil, nil
}
func (noopOrchestrator) ResolveEscalation(context.Context, string, EscalationResolution, ConfirmNonce) (EscalationRequest, error) {
	return EscalationRequest{}, nil
}
func (noopOrchestrator) Subscribe(context.Context, SubscribeFilter) (<-chan OrchestrationEvent, error) {
	ch := make(chan OrchestrationEvent)
	close(ch)
	return ch, nil
}
