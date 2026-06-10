// Package events is the unified event surface that adaptors emit
// through and the SSE hub fans out from. GembaEvent is the discriminated
// union both planes (WorkPlane + OrchestrationPlane) translate into so
// subscribers — the SPA, the conformance harness, future Prometheus
// gauges — see one schema regardless of which adaptor produced the
// signal.
//
// Schema design: stringly-typed Kind + map Payload. Per gm-hjx, the
// union MUST be additive-extensible — a new Kind value lands without a
// schema version bump and old subscribers ignore unknown kinds. This
// is why Kind is a Go string type instead of an iota-backed enum:
// adaptors can ship future kinds (epic.ui_state_changed,
// epic.reordered, epic.launched, …) without recompiling Gemba's core.
//
// gm-e3.6 ships the schema + the Translate adapter for OrchestrationEvent.
// OTEL trace propagation, /metrics endpoint, and the mutation→event
// integration test land in follow-up beads (see gm-e3.6 close note).

package events

import (
	"time"
)

// Kind names the canonical event vocabulary. Adaptors are encouraged
// to map their native event kinds to one of these tokens; unknown
// kinds pass through verbatim with a plane-prefix so subscribers can
// still route by namespace.
type Kind string

const (
	// WorkItem.* — emitted whenever a WorkPlane mutation lands. The
	// payload's `before` / `after` shape is adaptor-specific (gm-root
	// DD-9 — Gemba speaks the WorkPlane interface, not adaptor wire
	// format), but the trio (created / updated / closed) is the
	// universal taxonomy.
	WorkItemCreated  Kind = "workitem.created"
	WorkItemUpdated  Kind = "workitem.updated"
	WorkItemClosed   Kind = "workitem.closed"
	WorkItemEvidence Kind = "workitem.evidence_attached"

	// Session.* — every state-changing OrchestrationPlane method
	// emits one of these on its first call (idempotent replays under
	// the same nonce emit nothing).
	SessionTransition    Kind = "session.transition"
	SessionStateReported Kind = "session.state_reported"
	SessionCostSample    Kind = "session.cost_sample"

	// Escalation.* — opened on first surface, resolved on the first
	// matching ResolveEscalation call.
	EscalationOpened   Kind = "escalation.opened"
	EscalationResolved Kind = "escalation.resolved"

	// Reservation + workspace lifecycle. Per the OrchestrationPlane
	// contract these MUST emit on the first state change.
	ReservationClaimed  Kind = "reservation.claimed"
	ReservationReleased Kind = "reservation.released"
	WorkspaceAcquired   Kind = "workspace.acquired"
	WorkspaceReleased   Kind = "workspace.released"

	// Budget.* — three-tier inform/warn/stop fires on the first
	// threshold crossing per epic/sprint scope (gm-e11.5).
	BudgetInform Kind = "budget.inform"
	BudgetWarn   Kind = "budget.warn"
	BudgetStop   Kind = "budget.stop"

	// Capability.refresh — adaptor's CapabilityManifest was
	// re-evaluated (e.g. degraded → healthy transition). The SPA's
	// AdaptorBanner refetches `/api/capabilities` on receipt.
	CapabilityRefresh Kind = "capability.refresh"

	// Skill.output_emitted — a persona consult's spawned session
	// called the gemba-mcp emit_skill_output tool (gm-twp2). Payload
	// shape:
	//   skill_id   string             — registered persona.Skill.ID()
	//   line_count int                — len(lines), for cheap subscribers
	//   lines      []json.RawMessage  — verbatim model emissions; the
	//                                   persona dispatcher validates each
	//                                   line via Skill.ValidateOutputLine
	//                                   and appends to the consult's
	//                                   ValidatedLines / LineErrors.
	// SessionID on the envelope is the consult ID — the spawn driver
	// sets GEMBA_SESSION_ID = consult.ID so the bridge tail picks it up.
	SkillOutputEmitted Kind = "skill.output_emitted"
)

// Plane identifies which adaptor surface produced an event.
// Subscribers can capability-filter by plane (e.g. drop all
// orchestration-plane traffic when the deployment runs WorkPlane only).
type Plane string

const (
	PlaneWorkPlane     Plane = "workplane"
	PlaneOrchestration Plane = "orchestration"
)

// Source carries adaptor identity so a multi-adaptor deployment can
// disambiguate two simultaneously-bound planes (e.g. bd + dolt
// double-binding for migration). AdaptorID matches the manifest's
// AdaptorID field.
type Source struct {
	Plane     Plane  `json:"plane"`
	AdaptorID string `json:"adaptor_id"`
}

// GembaEvent is the unified envelope the SSE hub fans out and the
// /metrics endpoint aggregates from.
//
// The Payload field is a free-form map by design — adaptors carry
// their native shape inside it (e.g. a `before` / `after` WorkItem
// snapshot for workitem.updated). Subscribers MUST tolerate unknown
// keys and unknown kinds; old subscribers reading a future event
// shape ignore the extra fields without erroring.
//
// Empty optional fields serialise out of the JSON wire form
// (omitempty) so the SSE bandwidth stays low for the common case.
type GembaEvent struct {
	// ID is the adaptor-issued unique id for this event. Re-emission
	// of the same logical event MUST reuse this id so subscribers
	// keyed on (id) can dedupe.
	ID string `json:"id"`
	// Kind is the canonical event type. See the Kind constants.
	Kind Kind `json:"kind"`
	// At is the event's wall-clock timestamp from the adaptor's POV.
	At time.Time `json:"at"`
	// Source identifies the adaptor that emitted the event.
	Source Source `json:"source"`

	// Scope hooks — every field optional. Subscribers filter on
	// these to narrow what they consume (e.g. an EpicCard only
	// invalidates when EpicID matches).
	AssignmentID string `json:"assignment_id,omitempty"`
	SessionID    string `json:"session_id,omitempty"`
	WorkItemID   string `json:"work_item_id,omitempty"`
	AgentID      string `json:"agent_id,omitempty"`
	EpicID       string `json:"epic_id,omitempty"`
	SprintID     string `json:"sprint_id,omitempty"`

	// TraceID is the W3C trace-context id propagated from the caller's
	// CallCtx. Empty until the OTEL pipeline lands (gm-e3.6 follow-up).
	// Defined now so the wire shape doesn't need to grow once OTEL
	// arrives.
	TraceID string `json:"trace_id,omitempty"`

	// Payload carries the kind-specific body. See per-kind doc above
	// for the conventional shape; adaptors are free to add keys.
	Payload map[string]any `json:"payload,omitempty"`
}

// Filter narrows what a subscriber receives from the hub. Empty
// filter matches every event. When multiple fields are set, they
// AND together (Kinds within Kinds OR'd, then AND'd against the
// rest).
type Filter struct {
	Kinds        []Kind  `json:"kinds,omitempty"`
	Planes       []Plane `json:"planes,omitempty"`
	AssignmentID string  `json:"assignment_id,omitempty"`
	SessionID    string  `json:"session_id,omitempty"`
	WorkItemID   string  `json:"work_item_id,omitempty"`
	EpicID       string  `json:"epic_id,omitempty"`
}

// Match reports whether an event satisfies a filter. Used by the SSE
// hub before fanning to a subscriber.
func (f Filter) Match(e GembaEvent) bool {
	if len(f.Kinds) > 0 {
		ok := false
		for _, k := range f.Kinds {
			if k == e.Kind {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	if len(f.Planes) > 0 {
		ok := false
		for _, p := range f.Planes {
			if p == e.Source.Plane {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	if f.AssignmentID != "" && f.AssignmentID != e.AssignmentID {
		return false
	}
	if f.SessionID != "" && f.SessionID != e.SessionID {
		return false
	}
	if f.WorkItemID != "" && f.WorkItemID != e.WorkItemID {
		return false
	}
	if f.EpicID != "" && f.EpicID != e.EpicID {
		return false
	}
	return true
}
