package phase

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/GembaCore/gemba-core/internal/events"
)

// EventPlane is the events.Plane stamped on every phase.* event.
// Distinct from PlaneWorkPlane / PlaneOrchestration so SSE subscribers
// can filter on "show me phase changes" without parsing kinds.
const EventPlane events.Plane = "phase"

// EventAdaptorID identifies the in-process server as the source of
// phase.* events. Stable across restarts so subscribers keyed on
// (id) can dedupe consistently.
const EventAdaptorID = "gemba-server"

// KindPhaseTransitioned is the canonical event kind for a Phase
// transition. Matches the design's §API SSE table token
// `phase.transitioned`.
const KindPhaseTransitioned events.Kind = "phase.transitioned"

// Publisher is the minimal contract Advance + helpers need from the
// events.Hub. Decoupling from *events.Hub keeps the package easily
// testable and lets future test doubles intercept Publish without
// reconstructing a real hub.
type Publisher interface {
	Publish(ev events.GembaEvent)
}

// TransitionEvent renders a Transition as a GembaEvent ready to be
// fanned out through the hub. ctx carries the request's W3C span
// context so events.WithTraceID can stamp the trace id at emission
// time. The state argument is the post-transition state — its Phase
// equals t.To and is included in the payload for subscribers that
// only want the "current phase now" view without walking History.
func TransitionEvent(ctx context.Context, t Transition, state WorkspaceState) events.GembaEvent {
	payload := map[string]any{
		"from":  string(t.From),
		"to":    string(t.To),
		"phase": string(state.Phase),
	}
	if t.Reason != "" {
		payload["reason"] = t.Reason
	}
	if !state.Since.IsZero() {
		payload["since"] = state.Since.UTC().Format(time.RFC3339Nano)
	}
	ev := events.GembaEvent{
		ID:      newEventID(),
		Kind:    KindPhaseTransitioned,
		At:      t.At,
		Source:  events.Source{Plane: EventPlane, AdaptorID: EventAdaptorID},
		AgentID: string(t.By),
		Payload: payload,
	}
	return events.WithTraceID(ctx, ev)
}

// PublishTransition is the convenience helper that builds a
// TransitionEvent and Publishes it. No-op when pub is nil so callers
// can plumb a single optional Publisher through without nil-checks.
func PublishTransition(ctx context.Context, pub Publisher, t Transition, state WorkspaceState) {
	if pub == nil {
		return
	}
	pub.Publish(TransitionEvent(ctx, t, state))
}

// newEventID returns a 16-hex-byte unique id for one phase.* event.
// crypto/rand-backed; falls back to a deterministic timestamp suffix
// on the vanishingly-rare random-source failure so the helper never
// panics.
func newEventID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "phase-evt-" + time.Now().UTC().Format("20060102T150405.000000000")
	}
	return "phase-evt-" + hex.EncodeToString(b)
}
