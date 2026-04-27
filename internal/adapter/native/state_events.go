package native

import (
	"github.com/MikeBengtson/gemba/core"
)

// handleStateEvent reacts to the bridge's session_state_reported
// events (written by the gemba-state CLI, see cmd/gemba-state/main.go
// and internal/adapter/native/bridge/translate.go) and mutates the
// session's Status field. gm-cdph.
//
// The agent calls `gemba-state ready|working|prompting|...` at every
// state boundary; this handler is what makes those signals observable
// on Session.Status.
//
// Invariants:
//   - Terminal statuses absorb: a gemba-state call against an already-
//     completed/failed session is a silent no-op (the agent may still
//     be flushing frames when the operator ended the pane).
//   - Unknown state tokens are dropped silently (already validated
//     server-side by gemba-state; this is a defence-in-depth gate).
func (o *OrchestrationPlane) handleStateEvent(ev core.OrchestrationEvent) {
	if ev.Kind != "session_state_reported" {
		return
	}
	raw, _ := ev.Payload["state"].(string)
	status, ok := stateToken(raw)
	if !ok {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	sess, known := o.sessions[ev.SessionID]
	if !known {
		return
	}
	if isTerminalStatus(sess.Status) {
		return
	}
	sess.Status = status
}

// stateToken maps the wire token ("ready", "working", …) to a
// core.SessionStatus. Returns (0, false) for unknowns so callers can
// drop the event.
func stateToken(token string) (core.SessionStatus, bool) {
	switch token {
	case "initializing":
		return core.SessionInitializing, true
	case "ready":
		return core.SessionReady, true
	case "working":
		return core.SessionWorking, true
	case "prompting":
		return core.SessionPrompting, true
	case "stalled":
		return core.SessionStalled, true
	}
	return "", false
}
