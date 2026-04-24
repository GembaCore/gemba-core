package events

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/MikeBengtson/gemba/internal/core"
)

func TestGembaEventJSONRoundTrip(t *testing.T) {
	at := time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)
	ev := GembaEvent{
		ID:         "evt-1",
		Kind:       WorkItemUpdated,
		At:         at,
		Source:     Source{Plane: PlaneWorkPlane, AdaptorID: "bd"},
		WorkItemID: "gm-foo",
		EpicID:     "gm-root",
		TraceID:    "00-abc-def-01",
		Payload:    map[string]any{"before": "open", "after": "in_progress"},
	}
	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	var round GembaEvent
	if err := json.Unmarshal(b, &round); err != nil {
		t.Fatal(err)
	}
	if round.Kind != WorkItemUpdated {
		t.Errorf("Kind=%q", round.Kind)
	}
	if round.WorkItemID != "gm-foo" || round.EpicID != "gm-root" {
		t.Errorf("scope fields lost: %+v", round)
	}
	if round.TraceID != "00-abc-def-01" {
		t.Errorf("trace_id lost: %q", round.TraceID)
	}
	if round.Source.AdaptorID != "bd" || round.Source.Plane != PlaneWorkPlane {
		t.Errorf("source: %+v", round.Source)
	}
}

func TestGembaEventOmitsEmptyOptionals(t *testing.T) {
	ev := GembaEvent{
		ID:     "evt-2",
		Kind:   SessionTransition,
		Source: Source{Plane: PlaneOrchestration, AdaptorID: "native"},
	}
	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, banned := range []string{
		"work_item_id", "epic_id", "trace_id", "session_id",
		"assignment_id", "agent_id", "sprint_id", "payload",
	} {
		if containsStr(got, banned) {
			t.Errorf("optional field %q leaked into wire form: %s", banned, got)
		}
	}
}

func TestFromOrchestrationEventCanonicalizesKnownKinds(t *testing.T) {
	cases := []struct {
		in   string
		want Kind
	}{
		{"session_state_reported", SessionStateReported},
		{"session_transition", SessionTransition},
		{"escalation_opened", EscalationOpened},
		{"escalation_resolved", EscalationResolved},
		{"reservation_claimed", ReservationClaimed},
		{"reservation_released", ReservationReleased},
		{"workspace_acquired", WorkspaceAcquired},
		{"workspace_released", WorkspaceReleased},
	}
	for _, c := range cases {
		got := FromOrchestrationEvent("native", core.OrchestrationEvent{
			ID:   "x",
			Kind: c.in,
			At:   time.Now(),
		})
		if got.Kind != c.want {
			t.Errorf("kind=%q got %q, want %q", c.in, got.Kind, c.want)
		}
	}
}

func TestFromOrchestrationEventNamespacesUnknownKinds(t *testing.T) {
	got := FromOrchestrationEvent("native", core.OrchestrationEvent{
		ID:   "x",
		Kind: "user_message",
		At:   time.Now(),
	})
	if got.Kind != Kind("orchestration.user_message") {
		t.Errorf("Kind=%q, want orchestration.user_message", got.Kind)
	}
}

func TestFromOrchestrationEventPropagatesScopeFields(t *testing.T) {
	got := FromOrchestrationEvent("native", core.OrchestrationEvent{
		ID:           "evt",
		Kind:         "session_transition",
		At:           time.Now(),
		AssignmentID: "asg-1",
		SessionID:    "sess-1",
		AgentID:      core.AgentID("gemba/polecats/obsidian"),
		Payload:      map[string]any{"status": "running"},
	})
	if got.AssignmentID != "asg-1" || got.SessionID != "sess-1" {
		t.Errorf("scope: %+v", got)
	}
	if got.AgentID != "gemba/polecats/obsidian" {
		t.Errorf("agent: %q", got.AgentID)
	}
	if got.Payload["status"] != "running" {
		t.Errorf("payload: %+v", got.Payload)
	}
	if got.Source.Plane != PlaneOrchestration || got.Source.AdaptorID != "native" {
		t.Errorf("source: %+v", got.Source)
	}
}

func TestFromOrchestrationStreamClosesOnInputClose(t *testing.T) {
	in := make(chan core.OrchestrationEvent, 2)
	in <- core.OrchestrationEvent{ID: "1", Kind: "session_transition", At: time.Now()}
	in <- core.OrchestrationEvent{ID: "2", Kind: "session_state_reported", At: time.Now()}
	close(in)
	out := FromOrchestrationStream("native", in)
	got := []GembaEvent{}
	for ev := range out {
		got = append(got, ev)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 events, got %d", len(got))
	}
	if got[0].Kind != SessionTransition || got[1].Kind != SessionStateReported {
		t.Errorf("kinds: %+v", got)
	}
}

func TestFilterEmpty_MatchesEverything(t *testing.T) {
	ev := GembaEvent{Kind: WorkItemUpdated, Source: Source{Plane: PlaneWorkPlane, AdaptorID: "bd"}}
	if !(Filter{}).Match(ev) {
		t.Error("empty filter should match")
	}
}

func TestFilterByKind_NarrowsCorrectly(t *testing.T) {
	f := Filter{Kinds: []Kind{WorkItemUpdated, SessionTransition}}
	if !f.Match(GembaEvent{Kind: WorkItemUpdated}) {
		t.Error("WorkItemUpdated should match")
	}
	if f.Match(GembaEvent{Kind: EscalationOpened}) {
		t.Error("EscalationOpened should not match")
	}
}

func TestFilterByPlane_NarrowsCorrectly(t *testing.T) {
	f := Filter{Planes: []Plane{PlaneOrchestration}}
	if !f.Match(GembaEvent{Source: Source{Plane: PlaneOrchestration}}) {
		t.Error("orchestration should match")
	}
	if f.Match(GembaEvent{Source: Source{Plane: PlaneWorkPlane}}) {
		t.Error("workplane should not match")
	}
}

func TestFilterByScopeFields_AllANDed(t *testing.T) {
	f := Filter{AssignmentID: "asg-1", SessionID: "sess-1", EpicID: "gm-root"}
	if !f.Match(GembaEvent{AssignmentID: "asg-1", SessionID: "sess-1", EpicID: "gm-root"}) {
		t.Error("exact match should pass")
	}
	if f.Match(GembaEvent{AssignmentID: "asg-1", SessionID: "sess-2", EpicID: "gm-root"}) {
		t.Error("session mismatch should fail")
	}
	if f.Match(GembaEvent{AssignmentID: "asg-1", EpicID: "gm-root"}) {
		t.Error("missing session should fail when filter requires it")
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
