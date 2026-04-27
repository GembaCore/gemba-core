package native

import (
	"testing"
	"time"

	"github.com/MikeBengtson/gemba/core"
)

// helper — builds a fresh adaptor with a known session record we can
// mutate through handleStateEvent, without spinning up a backend or
// a real bridge.
func newWithSession(t *testing.T, id string, start core.SessionStatus) *OrchestrationPlane {
	t.Helper()
	o := New()
	o.mu.Lock()
	o.sessions[id] = &core.Session{
		ID:        id,
		Status:    start,
		StartedAt: time.Now(),
	}
	o.mu.Unlock()
	return o
}

func TestHandleStateEvent_transitionsObservableStates(t *testing.T) {
	cases := []struct {
		token string
		want  core.SessionStatus
	}{
		{"initializing", core.SessionInitializing},
		{"ready", core.SessionReady},
		{"working", core.SessionWorking},
		{"prompting", core.SessionPrompting},
		{"stalled", core.SessionStalled},
	}
	for _, tc := range cases {
		t.Run(tc.token, func(t *testing.T) {
			o := newWithSession(t, "sess-x", core.SessionInitializing)
			o.handleStateEvent(core.OrchestrationEvent{
				Kind:      "session_state_reported",
				SessionID: "sess-x",
				Payload:   map[string]any{"state": tc.token},
			})
			if got := o.sessions["sess-x"].Status; got != tc.want {
				t.Errorf("status after %q: got %q, want %q", tc.token, got, tc.want)
			}
		})
	}
}

func TestHandleStateEvent_ignoresWrongKind(t *testing.T) {
	o := newWithSession(t, "sess-y", core.SessionWorking)
	o.handleStateEvent(core.OrchestrationEvent{
		Kind:      "something_else",
		SessionID: "sess-y",
		Payload:   map[string]any{"state": "ready"},
	})
	if o.sessions["sess-y"].Status != core.SessionWorking {
		t.Errorf("non-state event mutated status")
	}
}

func TestHandleStateEvent_ignoresUnknownToken(t *testing.T) {
	o := newWithSession(t, "sess-z", core.SessionWorking)
	o.handleStateEvent(core.OrchestrationEvent{
		Kind:      "session_state_reported",
		SessionID: "sess-z",
		Payload:   map[string]any{"state": "bogus"},
	})
	if o.sessions["sess-z"].Status != core.SessionWorking {
		t.Errorf("unknown token mutated status")
	}
}

func TestHandleStateEvent_absorbsTerminal(t *testing.T) {
	o := newWithSession(t, "sess-t", core.SessionCompleted)
	o.handleStateEvent(core.OrchestrationEvent{
		Kind:      "session_state_reported",
		SessionID: "sess-t",
		Payload:   map[string]any{"state": "working"},
	})
	if o.sessions["sess-t"].Status != core.SessionCompleted {
		t.Errorf("state event bled past terminal status")
	}
}

func TestHandleStateEvent_ignoresUnknownSession(t *testing.T) {
	o := New()
	o.handleStateEvent(core.OrchestrationEvent{
		Kind:      "session_state_reported",
		SessionID: "sess-missing",
		Payload:   map[string]any{"state": "ready"},
	})
	// No panic, no entry inserted.
	if _, ok := o.sessions["sess-missing"]; ok {
		t.Errorf("handler created a session record for an unknown id")
	}
}
