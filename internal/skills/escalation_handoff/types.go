package escalation_handoff

// LineType is the discriminator for emit_handoff_response output
// lines. The dispatcher reads it to enforce ordering invariants
// (response first, summary last). Mirrors the pattern used by
// epic_order.LineType so downstream tooling (audit log, /insights)
// can render hand-off consults with the same machinery.
type LineType string

const (
	// LineResponse — the persona's narrative reply. Free-form text.
	// At least one MUST appear before the summary.
	LineResponse LineType = "response"
	// LineSummary — terminal artifact: a short operator-facing note
	// the SPA can render in the consult drawer ("PM recommends
	// approving with caveat X"). Exactly one MUST appear last.
	LineSummary LineType = "summary"
)

// HandoffInput is the request body for an escalation_handoff consult.
// The SPA's HandoffModal assembles it from the originating
// EscalationRequest plus the operator-picked persona id.
type HandoffInput struct {
	// EscalationID is the originating escalation's id. Echoed back
	// in the summary line so the audit log can correlate the
	// hand-off consult to the escalation that prompted it.
	EscalationID string `json:"escalation_id"`

	// Title is the escalation's operator-facing title — the same
	// string the EscalationCard renders in the inbox.
	Title string `json:"title"`

	// Prompt is the agent's question / pending decision text.
	// Optional (some escalations carry only a title — e.g. a
	// blocker_filed event).
	Prompt string `json:"prompt,omitempty"`

	// AssignmentID is the originating Claude Code session id, when
	// known. Lets the persona reference the upstream session in its
	// reply ("I checked sess-XY and...").
	AssignmentID string `json:"assignment_id,omitempty"`

	// TargetPersona is the persona id the operator picked. Carried
	// for the audit log; the dispatcher already routes the consult
	// to the persona via createConsultRequest.persona_id, so the
	// skill itself does not re-resolve it.
	TargetPersona string `json:"target_persona,omitempty"`
}

// ResponseLine is one narrative reply turn from the persona. The
// dispatcher concatenates these into the consult's user-visible
// transcript; the SPA's consult drawer renders them in order.
type ResponseLine struct {
	Type    LineType `json:"type"`
	Message string   `json:"message"`
}

// SummaryLine is the terminal artifact: a one-line operator-facing
// summary the SPA can splice into the inbox card without expanding
// the consult drawer.
type SummaryLine struct {
	Type LineType `json:"type"`
	// EscalationID echoes the input so a stale consult can be
	// correlated post-hoc.
	EscalationID string `json:"escalation_id"`
	// Disposition is the persona's high-level recommendation —
	// free-form, but conventionally one of "approve" / "deny" /
	// "modify" / "defer" / "needs_more_info". Not enforced; the
	// operator decides whether to act on it.
	Disposition string `json:"disposition,omitempty"`
	// Note is a short prose summary (one or two sentences). Always
	// required.
	Note string `json:"note"`
}
