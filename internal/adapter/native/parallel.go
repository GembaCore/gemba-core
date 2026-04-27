package native

import (
	"time"

	"github.com/MikeBengtson/gemba/internal/core"
)

// EventKindSessionParallelChanged is emitted whenever a pane's
// in-flight bead count changes (assignment or completion). Consumers
// (SPA pills, global counter, audit log) MUST subscribe to this kind
// to track parallelism without polling.
//
// See docs/design/parallelism-boundary.md and gm-root.16 for the
// design contract; testdata/session_parallel_changed.json holds the
// canonical payload fixture.
const EventKindSessionParallelChanged = "session_parallel_changed"

// extKeyReusePaneID lets a caller hand StartSession a pane_id from a
// running same-agent-type session and have the new bead co-locate in
// that pane instead of spawning a new one. Subject to MaxParallel cap.
// See docs/design/parallelism-boundary.md.
const extKeyReusePaneID = "gemba:reuse_pane_id"

// parallelChanged builds a session_parallel_changed event. delta is +1
// on assignment and -1 on completion; callers SHOULD skip zero (no
// observable transition) rather than emit a no-op event. The pill on
// session/agent cards keys off pane_id — multiple Session records can
// share one pane under intra-parallelism, so the pane is the natural
// aggregation unit for the operator-facing count.
func parallelChanged(paneID, sessionID, agentType string, inFlight, maxParallel, delta int) core.OrchestrationEvent {
	return core.OrchestrationEvent{
		Kind:      EventKindSessionParallelChanged,
		At:        time.Now(),
		SessionID: sessionID,
		Payload: map[string]any{
			"pane_id":      paneID,
			"session_id":   sessionID,
			"agent_type":   agentType,
			"in_flight":    inFlight,
			"max_parallel": maxParallel,
			"delta":        delta,
		},
	}
}

// PaneInFlight returns the current concurrent bead count on a pane.
// Used by the dispatcher to decide whether an existing pane has
// capacity for another bead, and by the SPA's pill consumer when
// it bootstraps before any session_parallel_changed event has fired.
// Returns 0 for unknown panes.
func (o *OrchestrationPlane) PaneInFlight(paneID string) int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.paneSessions[paneID])
}

// paneInFlightLocked is the lock-held variant for callers that already
// hold o.mu.
func (o *OrchestrationPlane) paneInFlightLocked(paneID string) int {
	return len(o.paneSessions[paneID])
}

// addSessionToPaneLocked appends a session id to the pane's session
// list. Caller MUST hold o.mu.
func (o *OrchestrationPlane) addSessionToPaneLocked(paneID, sessionID string) {
	o.paneSessions[paneID] = append(o.paneSessions[paneID], sessionID)
}

// removeSessionFromPaneLocked removes a session id from the pane's
// session list and returns the remaining count. When the count drops
// to zero the pane entry is deleted entirely. Caller MUST hold o.mu.
func (o *OrchestrationPlane) removeSessionFromPaneLocked(paneID, sessionID string) int {
	cur := o.paneSessions[paneID]
	out := cur[:0]
	for _, s := range cur {
		if s != sessionID {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		delete(o.paneSessions, paneID)
		return 0
	}
	o.paneSessions[paneID] = out
	return len(out)
}
