package native

import (
	"time"

	"github.com/MikeBengtson/gemba/internal/core"
)

// EventKindSessionParallelChanged is emitted whenever a session's
// in-flight bead count changes (assignment or completion). Consumers
// (SPA pills, global counter, audit log) MUST subscribe to this kind
// to track parallelism without polling.
//
// See docs/design/parallelism-boundary.md and gm-root.16 for the
// design contract; testdata/session_parallel_changed.json holds the
// canonical payload fixture.
const EventKindSessionParallelChanged = "session_parallel_changed"

// parallelChanged builds a session_parallel_changed event. delta is +1
// on assignment and -1 on completion; callers SHOULD skip zero (no
// observable transition) rather than emit a no-op event.
func parallelChanged(sessionID, agentType string, inFlight, maxParallel, delta int) core.OrchestrationEvent {
	return core.OrchestrationEvent{
		Kind:      EventKindSessionParallelChanged,
		At:        time.Now(),
		SessionID: sessionID,
		Payload: map[string]any{
			"session_id":   sessionID,
			"agent_type":   agentType,
			"in_flight":    inFlight,
			"max_parallel": maxParallel,
			"delta":        delta,
		},
	}
}
