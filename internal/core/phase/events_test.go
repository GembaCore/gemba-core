package phase

import (
	"context"
	"testing"
	"time"

	"github.com/MikeBengtson/gemba/internal/events"
)

// recPublisher captures every Publish call so tests can assert on
// the emitted event without standing up a real hub.
type recPublisher struct {
	got []events.GembaEvent
}

func (r *recPublisher) Publish(ev events.GembaEvent) { r.got = append(r.got, ev) }

func TestTransitionEvent_PayloadShape(t *testing.T) {
	now := time.Date(2026, 4, 27, 10, 0, 0, 0, time.UTC)
	state := WorkspaceState{Phase: PhaseBuilding, Since: now}
	tr := Transition{
		From: PhaseDesign, To: PhaseBuilding,
		At: now, By: "operator", Reason: "ADR ratified",
	}
	ev := TransitionEvent(context.Background(), tr, state)

	if ev.Kind != KindPhaseTransitioned {
		t.Errorf("Kind = %s, want %s", ev.Kind, KindPhaseTransitioned)
	}
	if ev.Source.Plane != EventPlane {
		t.Errorf("Source.Plane = %s, want %s", ev.Source.Plane, EventPlane)
	}
	if ev.Source.AdaptorID != EventAdaptorID {
		t.Errorf("Source.AdaptorID = %s, want %s", ev.Source.AdaptorID, EventAdaptorID)
	}
	if ev.AgentID != "operator" {
		t.Errorf("AgentID = %q, want operator", ev.AgentID)
	}
	if !ev.At.Equal(now) {
		t.Errorf("At = %v, want %v", ev.At, now)
	}

	if got, _ := ev.Payload["from"].(string); got != "design" {
		t.Errorf("payload.from = %q, want design", got)
	}
	if got, _ := ev.Payload["to"].(string); got != "building" {
		t.Errorf("payload.to = %q, want building", got)
	}
	if got, _ := ev.Payload["phase"].(string); got != "building" {
		t.Errorf("payload.phase = %q, want building", got)
	}
	if got, _ := ev.Payload["reason"].(string); got != "ADR ratified" {
		t.Errorf("payload.reason = %q, want 'ADR ratified'", got)
	}

	if ev.ID == "" {
		t.Error("event ID empty; expected a unique stamp")
	}
}

func TestTransitionEvent_OmitsEmptyReason(t *testing.T) {
	tr := Transition{From: PhaseDesign, To: PhaseBuilding, At: time.Now(), By: "op"}
	state := WorkspaceState{Phase: PhaseBuilding}
	ev := TransitionEvent(context.Background(), tr, state)
	if _, ok := ev.Payload["reason"]; ok {
		t.Errorf("payload.reason set unexpectedly: %v", ev.Payload["reason"])
	}
}

func TestPublishTransition_NilPublisherIsNoop(t *testing.T) {
	// Must not panic.
	tr := Transition{From: PhaseDesign, To: PhaseBuilding, At: time.Now()}
	PublishTransition(context.Background(), nil, tr, WorkspaceState{Phase: PhaseBuilding})
}

func TestPublishTransition_FansToPublisher(t *testing.T) {
	pub := &recPublisher{}
	tr := Transition{From: PhaseDesign, To: PhaseBuilding, At: time.Now(), By: "op", Reason: "go"}
	state := WorkspaceState{Phase: PhaseBuilding}
	PublishTransition(context.Background(), pub, tr, state)
	if len(pub.got) != 1 {
		t.Fatalf("Publish calls = %d, want 1", len(pub.got))
	}
	if pub.got[0].Kind != KindPhaseTransitioned {
		t.Errorf("Kind = %s", pub.got[0].Kind)
	}
}

// Sanity check: the Hub satisfies the Publisher contract. Compile-
// time assertion via type assertion at run time.
func TestEventsHubSatisfiesPublisher(t *testing.T) {
	hub := events.NewHub(events.Config{})
	t.Cleanup(hub.Close)
	var _ Publisher = hub
}
