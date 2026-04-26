package events

import (
	"github.com/MikeBengtson/gemba/internal/core"
)

// FromOrchestrationEvent converts a core.OrchestrationEvent (the
// existing per-adaptor envelope) into a unified GembaEvent. The
// adaptorID is required so the SSE hub can route by Source.AdaptorID
// even when two orchestration planes are bound (rare, but possible
// during cutover from one to another).
//
// Mapping table — adaptor-emitted Kind → canonical events.Kind:
//
//	session_state_reported → session.state_reported
//	session_transition     → session.transition
//	escalation_opened      → escalation.opened
//	escalation_resolved    → escalation.resolved
//	reservation_claimed    → reservation.claimed
//	reservation_released   → reservation.released
//	workspace_acquired     → workspace.acquired
//	workspace_released     → workspace.released
//
// Anything else (user_message, tool_use, unknown_hook, …) keeps its
// original Kind verbatim under the "orchestration." namespace so a
// subscriber that doesn't know the kind still receives a typed event
// it can ignore. This is the additive-extensible property gm-hjx
// requires: new Kinds land without code changes anywhere except the
// adaptor that introduced them.
func FromOrchestrationEvent(adaptorID string, ev core.OrchestrationEvent) GembaEvent {
	out := GembaEvent{
		ID:           ev.ID,
		At:           ev.At,
		Source:       Source{Plane: PlaneOrchestration, AdaptorID: adaptorID},
		AssignmentID: ev.AssignmentID,
		SessionID:    ev.SessionID,
		AgentID:      string(ev.AgentID),
		Payload:      ev.Payload,
	}
	out.Kind = canonicalOrchestrationKind(ev.Kind)
	return out
}

// canonicalOrchestrationKind maps the OrchestrationEvent.Kind string
// vocabulary to the canonical events.Kind set. Unknown kinds get a
// stable namespaced fallback ("orchestration.<original>") so
// subscribers can still filter.
func canonicalOrchestrationKind(k string) Kind {
	switch k {
	case "session_state_reported":
		return SessionStateReported
	case "session_transition":
		return SessionTransition
	case "escalation_opened":
		return EscalationOpened
	case "escalation_resolved":
		return EscalationResolved
	case "reservation_claimed":
		return ReservationClaimed
	case "reservation_released":
		return ReservationReleased
	case "workspace_acquired":
		return WorkspaceAcquired
	case "workspace_released":
		return WorkspaceReleased
	case "skill_output_emitted":
		// gm-twp2: persona consult emit_skill_output frames. Promoted
		// to a canonical Kind so the persona dispatcher can subscribe
		// with a single typed filter rather than the
		// "orchestration.<raw>" namespace fallback.
		return SkillOutputEmitted
	}
	return Kind("orchestration." + k)
}

// FromOrchestrationStream wraps a channel of OrchestrationEvents in a
// channel of GembaEvents. The returned channel closes when the input
// closes; a goroutine ferries events with a one-deep buffer so a
// closed input doesn't strand the wrapper.
//
// This is the seam the SSE hub uses to consume an adaptor's
// Subscribe() output without each adaptor having to learn the
// GembaEvent envelope. Future plane-specific translators (WorkPlane,
// budget) follow the same shape.
func FromOrchestrationStream(adaptorID string, in <-chan core.OrchestrationEvent) <-chan GembaEvent {
	out := make(chan GembaEvent, 1)
	go func() {
		defer close(out)
		for ev := range in {
			out <- FromOrchestrationEvent(adaptorID, ev)
		}
	}()
	return out
}

// FromWorkPlaneEvent converts a core.WorkPlaneEvent into a unified
// GembaEvent. gm-e4.3.1.
//
// Mapping table — adaptor-emitted Kind → canonical events.Kind:
//
//	workitem_created           → workitem.created
//	workitem_updated           → workitem.updated
//	workitem_closed            → workitem.closed
//	workitem_evidence_attached → workitem.evidence_attached
//
// Any other Kind is passed through verbatim under the "workplane."
// namespace so a subscriber that doesn't recognise the token still
// receives a typed event it can ignore.
func FromWorkPlaneEvent(adaptorID string, ev core.WorkPlaneEvent) GembaEvent {
	return GembaEvent{
		ID:         ev.ID,
		Kind:       canonicalWorkPlaneKind(ev.Kind),
		At:         ev.At,
		Source:     Source{Plane: PlaneWorkPlane, AdaptorID: adaptorID},
		WorkItemID: string(ev.WorkItemID),
		Payload:    ev.Payload,
	}
}

func canonicalWorkPlaneKind(k string) Kind {
	switch k {
	case core.WorkItemEventCreated:
		return WorkItemCreated
	case core.WorkItemEventUpdated:
		return WorkItemUpdated
	case core.WorkItemEventClosed:
		return WorkItemClosed
	case core.WorkItemEventEvidenceAttached:
		return WorkItemEvidence
	}
	return Kind("workplane." + k)
}
