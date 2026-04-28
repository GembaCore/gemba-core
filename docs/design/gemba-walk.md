# Gemba walk — interactive multi-topic decisioning conversation

**Status:** design · tracked in `gm-3nk`
**Author:** mike (captured by polecat onyx)
**Date:** 2026-04-22 (ratified 2026-04-21 via bead notes)
**Parent:** `gm-57b` (Personas as first-class primitive)
**Resolves DD:** DD-32 (Gemba walk as a first-class interactive decisioning surface)

## Naming

- **Gemba** (noun, the view) — the main dashboard / home screen. Epic-granular
  rendering + escalation dots + budget gauge + PM panel + active-guardrail
  banner, all at once. "Open the Gemba", "on your Gemba".
- **Gemba walk** (noun, the action) — the interactive multi-topic decisioning
  conversation between operator and PM; aggregates escalations from all workers
  into a single agenda; resumable; auditable. "Start a Gemba walk", "wrap up
  the walk".

The name replaces the prior working term "Jam Session" (decision 2026-04-21 by
mike). All prior "Jam Session" references in `gist.md`,
`product-description.md`, `RFC.md`, `discord-post.md`, `ui-spec-template.md`
are to be updated to "Gemba walk".

## Summary

Formalize the **Gemba walk** — an interactive, multi-topic, decisioning
conversation between the operator and the Project Manager persona that
captures escalated decision requests from all workers and produces a stream
of ratified / modified / rejected / deferred decisions. This is exactly what
the operator-and-assistant do today in ad-hoc conversations; making it
first-class means it can be resumable, auditable, and batch-pullable against
workspace state.

## The pattern being codified

The operator and PM open a working conversation. The PM pulls together:

- Open `EscalationRequest`s from every worker (polecat HITL questions, Manager
  suspensions, gate failures, budget-stop events, adaptor-degraded escalations,
  Witness findings, Refinery rejections)
- Recently-filed beads needing ratification
- Recently-closed work needing retro / summary
- Recently-failed sessions needing triage
- User-added agenda items

They work through the list. For each item: the PM frames it, summarizes
context, proposes actions. The operator ratifies / modifies / rejects / defers.
Decisions apply as `ExecutedAction`s (nonce-gated); deferred items stay in the
agenda or get bounced to backlog. The Gemba walk is auditable, resumable (can
pause and pick up later), and produces a permanent record in the audit log +
a summary artifact (Documentarian-written).

## Primitives

```go
type WalkID string

type GembaWalk struct {
    ID                 WalkID
    Workspace          WorkspaceID
    Label              string        // optional user-provided ("pre-release review")
    InitiatedBy        AgentID
    PrimaryPersona     PersonaID     // default: project-manager
    AssistingPersonas  []PersonaID   // personas the PM may consult during the walk
    StartedAt          time.Time
    EndedAt            *time.Time
    Status             string        // "active" | "paused" | "completed" | "abandoned"

    Agenda             []AgendaItem
    Transcript         []WalkTurn
    Decisions          []WalkDecision
    BeadsTouched       []WorkItemID  // every bead the walk created, closed, edited
    ActionsApplied     []ExecutedAction
    Cost               AdviceCost    // cumulative

    ResumeContext      string        // LLM-condensed summary for resume
}

type AgendaItem struct {
    ID         string
    Topic      string
    Source     AgendaSource   // user | escalation | hitl | gate_failure | bead_filed | bead_closed | suggested
    SourceRef  string
    Priority   int            // high-urgency escalations float up
    Status     string         // "queued" | "active" | "decided" | "deferred" | "dismissed"
    AddedAt    time.Time
    AddedBy    AgentID
    Decision   *WalkDecision
}

type AgendaSource struct {
    Kind   string
    Ref    string    // bead ID, escalation ID, HITL question ID, gate ID
    Worker *AgentID  // which worker surfaced it
}

type WalkTurn struct {
    At            time.Time
    Speaker       string     // "user" | PersonaID
    Content       string
    Citations     []Citation
    AgendaItemID  *string    // which agenda item this turn concerns (may be null for meta-conversation)
}

type WalkDecision struct {
    AgendaItemID    string
    Kind            string       // "ratify" | "modify" | "reject" | "defer" | "handoff"
    Rationale       string
    AppliedActions  []ExecutedAction
    Citations       []Citation
    DecidedAt       time.Time
    DecidedBy       AgentID      // typically the human
}
```

## Starting a walk

Two ways:

1. **Manual** — user clicks "Start walk" in the UI (or `gemba walk start`)
2. **PM-suggested** — PM detects a condition worth convening a walk (e.g., ≥3
   critical escalations open, a batch of newly-filed beads needs ratification,
   a persona-HITL question has aged beyond threshold) and offers: "Want to
   walk the Gemba?"

On start, the PM auto-populates the agenda from five sources:

| Source         | Query |
|----------------|-------|
| `escalation`   | all open `EscalationRequest`s in the workspace, sorted by urgency (`critical` → `high` → `medium` → `low`) |
| `hitl`         | all suspended Manager persona sessions awaiting HITL answers (per `gm-uf7`) |
| `gate_failure` | all open QA or Security gate failures |
| `bead_filed`   | beads filed in the last 24h (configurable) not yet ratified |
| `bead_closed`  | beads closed in the last 24h needing retro / summary (if Documentarian flagged them) |

User adds / removes / reorders agenda items freely.

## During a walk

The PM walks the agenda one item at a time (or user jumps around):

1. PM frames the item: "Agenda #3: Escalation from witness — polecat obsidian
   has dirty worktree. Full context: ..."
2. PM proposes actions (e.g., "Recommended: restart obsidian's session, mark
   gm-foo as open, file gm-bar as a follow-up").
3. User responds (ratify / modify / reject / defer / handoff to another
   persona).
4. PM applies the ratified action as `ExecutedAction` (nonce-gated via
   existing mutation path).
5. Turn moves to next agenda item.

The PM may consult other personas mid-walk (multi-persona orchestration per
`gm-uf7`): "Before I file this bead, let me check with the Architect on
design impact." Brief consultation runs inline; response feeds the current
agenda item's decision.

## Ending a walk

Two exit paths:

- **Pause** — preserves state; user resumes later via `gemba walk resume <id>`.
  Transcript + remaining agenda + cost carried over. Resumption uses
  `ResumeContext` (LLM-condensed summary) as the starting context injection.
- **End** — produces a **walk summary artifact** (markdown; Documentarian
  writes it) and an `AdviceRecord`-grade audit entry. Summary includes:
  label, duration, decisions by kind (N ratified, M modified, K deferred),
  beads touched, actions applied, cost. Summary lands in
  `docs/walks/<date>-<label>.md` (or a workspace-configurable path).

Ending also creates an Epic-stop Checkpoint (per `gm-tfy`'s `epic_stop`
trigger) if the walk closed out significant work.

## UI

### `/walk` (new top-level surface)

- **Left pane** — agenda list. Queued / active / decided / deferred swimlanes.
  Drag-reorder. Badge per item (source kind, priority, who surfaced it).
- **Center pane** — chat-style conversation with PM. Each turn shows: speaker,
  content, citations, action buttons for PM-proposed `SuggestedAction`s.
- **Right pane** — "what's happening elsewhere": open escalations not yet in
  agenda (with "add to walk" button), in-flight polecat peek thumbnails,
  budget gauge.
- **Bottom bar** — running summary:
  `N decisions · M beads touched · $X spent · {duration}`.

### Keyboard (integrates with `gm-7hj` global hotkey system)

- `w` — toggle walk panel (always-available; does not lose current context)
- `n` — next agenda item
- `d` — defer current item
- `Enter` on a `SuggestedAction` — applies it (with nonce confirm in managed
  mode)

### Integration with PM panel

The always-available PM panel (`gm-96l`) has a "Start walk" shortcut. An
active walk hijacks the PM panel for continuity — the chat is the walk
transcript when a walk is active.

## API

| Path                             | Verb  | Purpose |
|----------------------------------|-------|---------|
| `/api/v1/walks:start`            | POST  | Start a walk (optional label, persona pin) |
| `/api/v1/walks/:id`              | GET   | Full state |
| `/api/v1/walks/:id/agenda`       | POST  | Add an item |
| `/api/v1/walks/:id/agenda/:item` | PATCH | Reorder / update item |
| `/api/v1/walks/:id/turn`         | POST  | Add a user turn (triggers PM response) |
| `/api/v1/walks/:id/decide/:item` | POST  | Record a decision; apply `AppliedActions` |
| `/api/v1/walks/:id/consult`      | POST  | Invoke a secondary persona from inside the walk |
| `/api/v1/walks/:id/pause`        | POST  | Pause; compute `ResumeContext` |
| `/api/v1/walks/:id/resume`       | POST  | Resume; re-hydrate |
| `/api/v1/walks/:id/end`          | POST  | End; generate summary artifact |
| `/api/v1/walks:recent`           | GET   | List recent walks for audit |

SSE events: `walk.started`, `walk.agenda_added`, `walk.turn`, `walk.decided`,
`walk.consulted`, `walk.paused`, `walk.resumed`, `walk.ended`.

## Escalation-source aggregation

Key design point: **Gemba walks capture escalated decision requests from ALL
workers**, not just Manager persona HITLs. Concretely, the agenda pulls from:

- `OrchestrationEvent{kind: "escalation.raised"}` — polecat / crew / mayor
  escalations via the `EscalationRequest` pipeline
- Gas Town's `gt escalate` output (already an `EscalationRequest` kind)
- Witness findings (via mail or events)
- Refinery merge rejections with blocker notes
- Beads daemon degraded states (`gm-b1`)
- HITL questions from Manager persona sessions
- QA + Security gate failures
- Sprint `TokenBudget` threshold crossings (`budget.warn`, `budget.stop`)
- Adaptor-degraded events

No filter is applied by default; user filters down in the UI. This is
intentional: walks are exhaustive ("let's clear the inbox"); focused sessions
are for specific skills.

## Resumability + audit

Walk state is persisted like Manager HITL sessions (per `gm-uf7` invariant
#13). A walk can:

- Pause mid-item and resume hours/days later
- Survive Gemba process restarts
- Be reviewed by the user via `/walks/:id` (closed walk view is read-only)
- Show up in `/insights/personas` under a "walks" filter with aggregate
  metrics

Every `ExecutedAction` inside a walk gets the `walk_id` as provenance metadata
— retros can answer "which decisions came out of walk XYZ?"

## Relationship to other concepts

- **PM `what_remains` Skill** (`gm-m38`) — an individual Skill invocation;
  produces structured-markdown. A walk is multi-skill + conversational — may
  INVOKE `what_remains` as its opening agenda pull.
- **PM `add_to_backlog` + `change_this`** (`gm-dnc`, `gm-d0e`) — agentic
  mutation-capable skills. A walk may invoke these mid-session when the
  operator says "go ahead and file those three beads."
- **HITL suspension** (`gm-uf7`) — a Manager persona suspending with a HITL
  question automatically surfaces as an agenda item in any active walk. If
  no walk is active, the HITL landing surfaces as an inbox escalation.
- **Workspace modes** (`gm-e8o`) — in managed mode, every `ExecutedAction`
  inside a walk still requires an explicit nonce confirm per-mutation; in
  supervised mode, the walk's authorization envelope covers a batch of
  related mutations; in unsupervised mode, the walk can blaze through.
- **Checkpoints** (`gm-tfy`) — starting a walk creates a pre-walk checkpoint
  (`epic_start` trigger variant); ending creates a post-walk checkpoint.
- **Persona PPPP** (`gm-9rv`) — any persona may volunteer a `Perspective`
  comment on the active agenda item (same surface used for inline
  `epic_event` / `walk_agenda_item` triggers).

## Follow-up beads

| Bead       | Scope |
|------------|-------|
| `gm-rfy`   | Core — types + persistence + agenda aggregator |
| `gm-wgv`   | HTTP API + SSE events |
| `gm-i65`   | `/walk` UI surface |
| `gm-4p6`   | PM persona walk-specific system prompt extension |
| `gm-77u`   | Documentarian walk-summary artifact skill |
| `gm-3jv`   | Cross-worker escalation aggregator |
| `gm-0tl`   | Keyboard shortcuts (`w` / `n` / `d` / `Enter`) |

Dependency chain: `gm-rfy` → `gm-wgv` → `gm-i65` → `gm-0tl`.
`gm-4p6`, `gm-77u`, `gm-3jv` all depend on `gm-rfy`.

## Definition of Done

- [x] Design ratified by mike (naming + structure confirmed 2026-04-21)
- [x] `docs/design/gemba-walk.md` committed
- [x] Name chosen and consistent (Gemba walk)
- [x] Follow-up beads filed
- [x] `gm-p27` updated to require `/walk` route + `w` keyboard entry in the UI
      spec

## Not in scope

- Implementation (lives in the follow-up beads)
- Multi-participant walks (more than one human in the room; v2+)
- Voice / audio transcripts (v2+)
- Automatic walk scheduling (user-triggered only in v1; PM may *suggest* but
  not auto-start)
