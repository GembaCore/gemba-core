package native

import (
	"context"
	"testing"
	"time"

	"github.com/GembaCore/gemba-core/core"
)

func testCtx() context.Context { return context.Background() }

func TestHandleStateEvent_BeadDoneCleanTransitionsToReady(t *testing.T) {
	withCleanWorktree(t, true)
	o := New()
	o.mu.Lock()
	o.sessions["sess-clean"] = &core.Session{
		ID:        "sess-clean",
		Status:    core.SessionWorking,
		StartedAt: time.Now(),
		ProviderMetadata: map[string]any{
			"worktree": "/tmp/fake-worktree",
		},
		ActiveTurnID: "turn-7",
	}
	o.mu.Unlock()

	o.handleStateEvent(core.OrchestrationEvent{
		Kind:      "session_state_reported",
		SessionID: "sess-clean",
		Payload:   map[string]any{"state": "bead-done"},
	})

	o.mu.Lock()
	defer o.mu.Unlock()
	s := o.sessions["sess-clean"]
	if s.Status != core.SessionReady {
		t.Errorf("status: got %q want Ready", s.Status)
	}
	if s.ActiveTurnID != "" {
		t.Errorf("ActiveTurnID should be cleared on bead-done; got %q", s.ActiveTurnID)
	}
	if got, _ := s.ProviderMetadata["bead_done_clean"].(bool); !got {
		t.Error("bead_done_clean metadata flag not set")
	}
}

func TestHandleStateEvent_BeadDoneDirtyRefusesAndKeepsWorking(t *testing.T) {
	withCleanWorktree(t, false)
	o := New()
	// Capture escalation events on a subscription.
	sub, err := o.Subscribe(testCtx(), core.SubscribeFilter{})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	o.mu.Lock()
	o.sessions["sess-dirty"] = &core.Session{
		ID:        "sess-dirty",
		Status:    core.SessionWorking,
		StartedAt: time.Now(),
		ProviderMetadata: map[string]any{
			"worktree": "/tmp/fake-worktree",
		},
	}
	o.mu.Unlock()

	o.handleStateEvent(core.OrchestrationEvent{
		Kind:      "session_state_reported",
		SessionID: "sess-dirty",
		Payload:   map[string]any{"state": "bead-done"},
	})

	o.mu.Lock()
	s := o.sessions["sess-dirty"]
	o.mu.Unlock()
	if s.Status != core.SessionWorking {
		t.Errorf("dirty bead-done should keep Working; got %q", s.Status)
	}
	if _, set := s.ProviderMetadata["bead_done_clean"]; set {
		t.Error("bead_done_clean must not be set on dirty refusal")
	}
	// An escalation_opened event should have fired.
	timeout := time.After(500 * time.Millisecond)
	gotEscalation := false
	for !gotEscalation {
		select {
		case ev := <-sub:
			if ev.Kind == "escalation_opened" {
				gotEscalation = true
			}
		case <-timeout:
			t.Fatal("expected escalation_opened event, got none")
		}
	}
}
