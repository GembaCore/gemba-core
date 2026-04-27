// Core types for Gemba walks (gm-rfy, design §Primitives).

package walk

import (
	"time"

	"github.com/MikeBengtson/gemba/internal/core"
)

// ID is the walk identifier. Stable for the life of the walk;
// every persistence row + every ExecutedAction stamped with
// walk_id provenance carries this value.
type ID string

// Status names the four lifecycle phases of a walk. The
// transition graph: active → paused (resumable) → active;
// active → completed (terminal); active → abandoned (terminal,
// rare — used when a walk is dropped without an end summary).
type Status string

const (
	StatusActive    Status = "active"
	StatusPaused    Status = "paused"
	StatusCompleted Status = "completed"
	StatusAbandoned Status = "abandoned"
)

// IsTerminal reports whether the walk has reached an end state.
// Terminal walks never accept more turns or decisions.
func (s Status) IsTerminal() bool {
	return s == StatusCompleted || s == StatusAbandoned
}

// AgendaSourceKind names where an agenda item came from. The
// five non-"user" kinds correspond to the aggregator's five
// source queries (design §Starting a walk).
type AgendaSourceKind string

const (
	SourceUser        AgendaSourceKind = "user"
	SourceEscalation  AgendaSourceKind = "escalation"
	SourceHITL        AgendaSourceKind = "hitl"
	SourceGateFailure AgendaSourceKind = "gate_failure"
	SourceBeadFiled   AgendaSourceKind = "bead_filed"
	SourceBeadClosed  AgendaSourceKind = "bead_closed"
	SourceSuggested   AgendaSourceKind = "suggested"
)

// AgendaItemStatus tracks one item's progress through the walk.
// Items move queued → active → (decided | deferred | dismissed).
// "deferred" stays on the agenda for the next walk; "dismissed"
// drops without a decision row.
type AgendaItemStatus string

const (
	AgendaQueued    AgendaItemStatus = "queued"
	AgendaActive    AgendaItemStatus = "active"
	AgendaDecided   AgendaItemStatus = "decided"
	AgendaDeferred  AgendaItemStatus = "deferred"
	AgendaDismissed AgendaItemStatus = "dismissed"
)

// DecisionKind names what the operator did with one agenda
// item. Five canonical kinds, mirroring the design's five-way
// split. "handoff" routes the item to another persona / queue
// without resolving it on the walk.
type DecisionKind string

const (
	DecisionRatify  DecisionKind = "ratify"
	DecisionModify  DecisionKind = "modify"
	DecisionReject  DecisionKind = "reject"
	DecisionDefer   DecisionKind = "defer"
	DecisionHandoff DecisionKind = "handoff"
)

// AgendaSource is the per-item provenance bundle. Kind picks
// the source query; Ref carries the source's own id (escalation
// id, work-item id, gate id) for cross-reference; Worker
// records which agent surfaced the item (when known) so the UI
// can render "from polecat onyx".
type AgendaSource struct {
	Kind   AgendaSourceKind `json:"kind"`
	Ref    string           `json:"ref,omitempty"`
	Worker core.AgentID     `json:"worker,omitempty"`
}

// AgendaItem is one row on the walk's agenda. Items keep their
// AddedAt + AddedBy so an audit can reconstruct who pushed an
// item onto the walk and when.
type AgendaItem struct {
	ID       string           `json:"id"`
	Topic    string           `json:"topic"`
	Source   AgendaSource     `json:"source"`
	Priority int              `json:"priority"`
	Status   AgendaItemStatus `json:"status"`
	AddedAt  time.Time        `json:"added_at"`
	AddedBy  core.AgentID     `json:"added_by,omitempty"`
	// Decision is non-nil once the operator has resolved this
	// item. JSON-omitted while queued so the UI's render path
	// reads "no decision yet" without a nil check.
	Decision *WalkDecision `json:"decision,omitempty"`
}

// IsQueued reports whether the item is awaiting attention. The
// aggregator skips items already decided / dismissed when
// re-running over a paused walk.
func (a AgendaItem) IsQueued() bool {
	return a.Status == AgendaQueued
}

// Speaker tags a turn as user, persona, or system.
type Speaker string

const (
	SpeakerUser    Speaker = "user"
	SpeakerPersona Speaker = "persona"
	SpeakerSystem  Speaker = "system"
)

// Citation is a lightweight reference attached to a turn or
// decision rationale. Kept narrow on purpose — gm-rfy ships
// the field, the UI / persona layers fill it in. Defining it
// here (rather than in core) keeps the walk package self-
// contained while a richer shape lands in a follow-up.
type Citation struct {
	// Kind names the reference flavour: "bead", "escalation",
	// "doc", "url". Empty kind means "free text".
	Kind string `json:"kind,omitempty"`
	// Ref is the kind-specific identifier (bead id, URL, etc).
	Ref string `json:"ref"`
	// Snippet is an optional human-readable excerpt for UI
	// rendering — e.g. the bead title for a bead citation.
	Snippet string `json:"snippet,omitempty"`
}

// WalkTurn is one transcript entry. AgendaItemID is non-empty
// when the turn concerns a specific item; empty for meta-
// conversation (start banner, "let's pause" exchange, etc.).
type WalkTurn struct {
	ID           string       `json:"id"`
	At           time.Time    `json:"at"`
	Speaker      Speaker      `json:"speaker"`
	PersonaID    string       `json:"persona_id,omitempty"`
	UserID       core.AgentID `json:"user_id,omitempty"`
	Content      string       `json:"content"`
	Citations    []Citation   `json:"citations,omitempty"`
	AgendaItemID string       `json:"agenda_item_id,omitempty"`
}

// WalkDecision is the operator's resolution of one agenda item.
// AppliedActionIDs lists ExecutedAction ids the persona applier
// recorded against this decision; the walk package stores the
// ids only — the persona / applier layer owns the full record.
type WalkDecision struct {
	AgendaItemID     string       `json:"agenda_item_id"`
	Kind             DecisionKind `json:"kind"`
	Rationale        string       `json:"rationale,omitempty"`
	AppliedActionIDs []string     `json:"applied_action_ids,omitempty"`
	Citations        []Citation   `json:"citations,omitempty"`
	DecidedAt        time.Time    `json:"decided_at"`
	DecidedBy        core.AgentID `json:"decided_by"`
}

// Cost is the running token / dollar tally for a walk. Mirrors
// the shape persona consults already use (TokenUsage + Dollars)
// so the walk's summary card can be rendered without a unit
// conversion. Persona / dispatcher writes update Cost via
// Store.AddCost.
type Cost struct {
	TokensIn  int     `json:"tokens_in"`
	TokensOut int     `json:"tokens_out"`
	Dollars   float64 `json:"dollars"`
}

// Add returns the elementwise sum. Pure; the Store handles
// persistence.
func (c Cost) Add(d Cost) Cost {
	return Cost{
		TokensIn:  c.TokensIn + d.TokensIn,
		TokensOut: c.TokensOut + d.TokensOut,
		Dollars:   c.Dollars + d.Dollars,
	}
}

// Walk is the persisted view of one Gemba walk. The Agenda /
// Transcript / Decisions slices are append-mostly: Status moves
// items between buckets via Store.UpdateAgendaItem, but the
// transcript and decisions are append-only.
type Walk struct {
	ID                ID             `json:"id"`
	Workspace         string         `json:"workspace"`
	Label             string         `json:"label,omitempty"`
	InitiatedBy       core.AgentID   `json:"initiated_by"`
	PrimaryPersona    string         `json:"primary_persona"`
	AssistingPersonas []string       `json:"assisting_personas,omitempty"`
	StartedAt         time.Time      `json:"started_at"`
	EndedAt           *time.Time     `json:"ended_at,omitempty"`
	Status            Status         `json:"status"`
	Agenda            []AgendaItem   `json:"agenda"`
	Transcript        []WalkTurn     `json:"transcript,omitempty"`
	Decisions         []WalkDecision `json:"decisions,omitempty"`
	// BeadsTouched is the union of every work item the walk
	// created, closed, or edited. Sourced from applied
	// ExecutedAction provenance; the walk package keeps the
	// list as ids only.
	BeadsTouched []core.WorkItemID `json:"beads_touched,omitempty"`
	Cost         Cost              `json:"cost"`
	// ResumeContext is the LLM-condensed summary the resume
	// turn injects. Empty until BuildResumeContext is called
	// (typically at pause time); rebuilt on each pause.
	ResumeContext string `json:"resume_context,omitempty"`
}

// IsActive reports whether the walk is currently accepting
// turns. Paused walks do not (they require Resume); terminal
// walks never do.
func (w Walk) IsActive() bool {
	return w.Status == StatusActive
}

// QueuedAgenda returns the slice of agenda items still awaiting
// attention. Helper for the agenda aggregator's "what's left to
// merge into?" path on a resumed walk.
func (w Walk) QueuedAgenda() []AgendaItem {
	out := make([]AgendaItem, 0, len(w.Agenda))
	for _, a := range w.Agenda {
		if a.IsQueued() {
			out = append(out, a)
		}
	}
	return out
}
