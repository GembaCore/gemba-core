# Persona PPPP: Personality + Perspective + Purview (phase-gated)

**Status:** design · tracked in `gm-9rv`
**Author:** mike (captured by polecat quartz)
**Date:** 2026-04-21 (ratification pending)
**Parent:** `gm-57b` (Personas as first-class primitive)
**Resolves DD:** DD-34

## Summary

Extend the Persona primitive (`gm-57b`) with three orthogonal, separately
configurable axes, and introduce project **Phase** as first-class state so a
persona's gate authority can be phase-conditional.

- **Personality** — tonal flavor; decorative only.
- **Perspective** — a lens the persona always applies; produces rich non-binding
  commentary. Many personas may volunteer perspectives on the same item at the
  same time.
- **Purview** — the domain in which the persona has gate authority.
  **Phase-activated**: dormant outside active phases, binding (up to blocking)
  during them.

This composes cleanly with the Coach/Manager split (`gm-2yg`) and with the QA
gate architecture (`gm-v6f`): a Coach with a Purview produces strong advisory
opinions; a Manager with a Purview in an active phase can actually block.

## Motivation

Today's Persona + Skill model (`gm-57b`) treats every persona as equally
weighted at every moment. In practice:

- The Architect's opinion during **design** matters more than during shipping.
- The Deployment Engineer's opinion during **shipping** matters more than
  during design.
- Security's opinion flips from advisory to blocking the moment auth-labeled
  work enters the picture.

We need a native way to encode that asymmetry without bolting ad-hoc gates onto
individual flows.

## The three axes

### 1. Personality

Tonal flavoring only. Injected into the persona's system prompt as a
voice/tone qualifier.

Examples:

- "Laconic and precise. Says what it means; never padding."
- "Warm, mentor-ish. Encourages the operator, praises well-reasoned judgment calls."
- "Wry, mildly skeptical. Reads between the lines. Not sarcastic."

```go
type Personality struct {
    ID          string   // "laconic" | "warm" | "wry" | custom
    Description string   // free-form; appended to system prompt as "Voice: {description}"
    Examples    []string // optional; few-shot style anchors
}
```

Personality is **never** load-bearing for correctness. Two personas with
different personalities but identical perspective + purview produce the same
factual content, different wording. Safe to experiment with.

### 2. Perspective

A deterministic lens the persona always applies when consulted. Every persona
has exactly one perspective, declared at config time. Perspective manifests in
two ways:

1. **When explicitly consulted** — the persona's full skill surface applies,
   filtered through the perspective.
2. **When the persona volunteers a comment** — any active persona may optionally
   contribute an inline "Perspective from Documentarian: the project summary
   you're editing has been stale for 3 weeks" note on any active conversation
   or Gemba walk agenda, without being asked.

Examples:

- **Architect** — design integrity, module boundaries, type-system cleanness, coupling cost
- **UX Expert** — vocabulary consistency, user-flow clarity, accessibility, keyboard ergonomics
- **QA** — testability, regression surface, evidence requirements, flake risk
- **Security** — auth surface, data handling, injection risk, attack surface, secret exposure
- **Deployment Engineer** — release mechanics, semver discipline, reproducibility, rollback path
- **Documentarian** — artifact freshness, decision log completeness, vocabulary stability, reader comprehension

```go
type Perspective struct {
    Statement     string        // e.g. "design integrity, module boundaries, coupling cost"
    Triggers      []string      // label or signal patterns that should surface a volunteered comment
    VolunteerMode VolunteerMode // never | on_demand | on_trigger | always
    CostTier      string        // "haiku" (default for volunteered) | "sonnet" | "opus" (explicit consult)
}

type VolunteerMode string
const (
    VolunteerNever     VolunteerMode = "never"       // only speaks when consulted
    VolunteerOnDemand  VolunteerMode = "on_demand"   // speaks when the operator asks "any perspectives?"
    VolunteerOnTrigger VolunteerMode = "on_trigger"  // speaks when Triggers match
    VolunteerAlways    VolunteerMode = "always"      // speaks every turn; use sparingly
)
```

**Multiple perspectives stack.** A conversation editing `internal/core/session.go`
with `auth` labels in play would trigger volunteered perspectives from Architect
(core edits), Security (auth label), and UX Expert (if user-facing copy is
involved). Each gets its own inline note; the operator reads and proceeds.
**Perspectives never block.**

#### Perspective economics

Volunteered perspectives use a **cheaper model tier by default**
(Claude Haiku 4.5) with short response caps. A perspective is a 1–2 sentence
commentary, not an essay. Cost-per-perspective is bounded (target <$0.01/call)
so many perspectives can surface without budget impact. Explicitly consulted
full-skill invocations use the persona's configured tier (typically Opus).

#### Perspective hooks — Epic lifecycle

Perspectives hook into Epic transitions:

- `epic.create` — personas with matching triggers may offer a perspective on
  the new Epic (e.g., Architect: "touches core; flag for design review";
  Documentarian: "link this to decisions-log entry #14").
- `epic.start` (→ InProgress) — readiness perspectives (QA: "no test plan yet";
  Security: "auth surface touched — my purview activates").
- `epic.end` (→ Completed) — retro perspectives (Architect: "design held up
  well"; Documentarian: "decisions-log updated").

#### Perspective hooks — Gemba walks

During a Gemba walk, for each agenda item, all active personas (not just those
PM consulted) may contribute an inline perspective pane. The operator sees the
PM's framing plus perspective notes from others.

Example — escalation about an auth-surface change:

- PM frames + proposes actions.
- Security perspective: "blocking my purview".
- Architect perspective: "changing the type — need DD amendment".
- Documentarian perspective: "no release-notes entry yet".

#### Noise control

The operator must be able to:

- Filter perspectives by persona (mute Code Reviewer on this walk).
- Filter perspectives by category (show only design + security; hide copy + docs).
- Set a per-workspace max-perspectives-per-item cap (default 3; avoids 11
  personas talking at once).
- Snooze volunteered perspectives for a session ("not relevant right now").

Muted personas render as faded avatars in a collapsed "muted" bar; one click
reinstates.

```go
type PerspectiveComment struct {
    Persona      PersonaID
    Content      string
    TriggeredBy  PerspectiveTrigger // epic_event | walk_agenda_item | on_demand
    CostDollars  float64
    RenderedAt   time.Time
}

type PerspectiveFilter struct {
    WorkspaceID      string
    MutedPersonas    []PersonaID
    HiddenCategories []string
    MaxPerItem       int // default 3
}
```

### 3. Purview

A domain in which the persona has **gate authority**, but only when the project
is in an active phase that includes that Purview. The core change: gate
authority becomes **phase-conditional**.

#### Phases

A **Phase** is a project-level mode that determines which Purviews are active.
Phases are transitionable states; the project has exactly one phase at a time.
Initial enum:

- **ideation** — research, prior-art analysis, concept shaping
- **design** — architectural decisions, type-system design, UI spec, API contract locked
- **building** — implementation, test authoring, iterative work
- **validating** — QA pass, security review, release-readiness checks
- **shipping** — final release mechanics, deploy, post-release monitoring
- **commercialization** — product-strategy work, go-to-market, pricing (future CEO pack)

Phase transitions are nonce-gated workspace actions and emit events
(`phase.changed`). Workspace modes (`gm-e8o`) compose: in managed mode, every
phase transition requires explicit user approval.

#### Purview mapping

Each persona declares its Purview and the phases it's active in:

```go
type Purview struct {
    Domain            string           // "design" | "testing" | "auth" | "release" | "copy" | custom
    ActivePhases      []Phase          // Phases where this Purview is binding
    BlockingAuthority PurviewAuthority // advisory | strong | blocking
}

type PurviewAuthority string
const (
    PurviewAdvisory PurviewAuthority = "advisory" // always a Coach; strong opinions, never blocks
    PurviewStrong   PurviewAuthority = "strong"   // can block with persona consensus + user override
    PurviewBlocking PurviewAuthority = "blocking" // hard-block in active phases; override requires nonce+justification
)
```

#### Example roster

| Persona | Perspective | Purview | Active Phases | Authority |
|---|---|---|---|---|
| Architect | design integrity | **design** | ideation, design, building | strong |
| UX Expert | UX consistency | **copy + UI vocabulary** | design, building | strong |
| Code Reviewer | diff quality | **code** | building | strong |
| QA | testability, regression | **testing + release readiness** | validating, shipping | blocking |
| Security | attack surface | **auth + data handling** | (auto on auth-labeled work, any phase) | blocking |
| Deployment Engineer | release mechanics | **release** | shipping | blocking |
| Documentarian | artifact curation | **docs + decisions log** | (any phase; advisory) | advisory |
| Project Manager | coordination | (none — PM is orchestrator, not domain-owner) | — | — |
| Technical Writer | copy quality | copy (advisory) | building, shipping | advisory |
| Onboarder | onboarding | — (bootstrapped only) | — | — |

**Project Manager has no Purview.** PM coordinates; PM doesn't own a domain.
This preserves the "personas answer, polecats execute" layering — PM
orchestrates, Purview-holders block.

#### How a Purview blocks

When a persona with `blocking` authority is in an active phase and detects a
violation of its Perspective, it files an `EscalationRequest{kind: "purview_violation"}`
attached to the Epic or bead being transitioned. The state transition is gated:
can't move without one of:

- Fixing the violation (Perspective satisfied → Purview releases).
- Overriding via nonce-confirmed user justification (logged prominently in audit).
- Consensus from two other Manager Purviews that this is an acceptable exception.

This composes cleanly with QA gates (`gm-v6f`): a QA gate is already a Purview
in testing/validating/shipping phases. The generalization is that Architect,
Security, and Deployment Engineer get equivalent gates in their respective
phases.

## Updated Persona config (TOML)

```toml
id = "architect"
name = "Arch"
role = "Architect"
variety = "coach"  # Coaches cap at PurviewStrong
icon = "📐"

[personality]
id = "laconic"
description = "Precise and economical. Doesn't over-explain; assumes the operator is sharp."

[perspective]
statement = "design integrity, module boundaries, type-system cleanness, coupling cost"
triggers = ["area:core", "area:contracts", "area:api", "new type introduced", "edges internal/core/*"]
volunteer_mode = "on_trigger"
cost_tier = "haiku"

[purview]
domain = "design"
active_phases = ["ideation", "design", "building"]
blocking_authority = "strong"  # Coaches cap here

[model]
vendor = "anthropic"
model = "claude-opus-4-7"
temperature = 0.2

[skills]
opt_in = ["design_review", "dep_hygiene", "epic_decompose", "api_shape_review", "merge_conflict_forecast"]
```

## Architectural invariants (amendments to `gm-ege`)

27. **Personality is decorative.** Two personas with the same Perspective +
    Purview but different Personalities produce equivalent correctness-affecting
    output. Personality never gates anything.
28. **Perspective never blocks.** Perspectives produce comments, volunteered
    or on-demand. To block, a persona needs a Purview with blocking authority
    in an active phase. Without it, opinions are advisory-only.
29. **Purview is phase-gated.** A Purview is dormant outside its active phases
    and binding during them. The phase is a project-level first-class state.
30. **Phase transitions are nonce-gated and event-emitting.** `phase.changed`
    is a first-class event; managed mode requires user approval on every
    transition.
31. **PM has no Purview.** PM coordinates; PM does not own a domain. This
    preserves the answer-vs-execute separation.
32. **Security's Purview auto-activates regardless of phase** when auth-labeled
    work is in play. Security is the only Purview that ignores phase state —
    the risk profile demands it.

## UI integration

- **Phase indicator in chrome** — always-visible, color-coded badge. Click →
  phase detail with active Purviews listed.
- **Purview palette** — `/project/purviews` view showing every persona's
  Purview + active phases + authority; operator sees at a glance which personas
  can block right now.
- **Perspective sidebar** — when volunteer-on-trigger personas have comments
  on the current work, they surface as a small sidebar indicator; click reveals
  the full comment set. Not a modal; not blocking.
- **Phase transition UX** — managed mode: summary + approve-each-active-Purview;
  supervised: transition + active-Purviews listed; unsupervised: silent.
- **Purview violation inline** — when a Manager's Purview blocks a transition,
  the blocking reason surfaces in the relevant drawer + on the `/escalations`
  inbox under `purview_violation`.

## API

| Path | Verb | Purpose |
|---|---|---|
| `/api/v1/phases` | GET | List valid phases |
| `/api/v1/project/phase` | GET | Current phase |
| `/api/v1/project/phase` | PATCH | Transition (nonce-gated) |
| `/api/v1/purviews` | GET | All personas' purviews + active status |
| `/api/v1/perspectives:recent` | GET | Recently-volunteered perspectives |

SSE events: `phase.transitioned`, `purview.activated`, `purview.deactivated`,
`perspective.offered`, `purview.violation_raised`, `purview.violation_resolved`.

## Implementation follow-ups

Filed as follow-up beads (not blocking this design):

- `gm-persona-pppp-core` — add Personality + Perspective + Purview fields to
  Persona type + TOML config schema
- `gm-phase-core` — Phase enum + project-level phase state + transition
  handlers + events
- `gm-purview-gate-core` — Purview-as-gate binding integrated with Manager
  mutation path (extends `gm-uf7` + `gm-v6f`)
- `gm-perspective-volunteer` — trigger-based volunteered-comment mechanism
  (SSE + UI sidebar)
- `gm-perspective-cost-tier` — model-tier routing for perspective vs skill
  calls
- `gm-perspective-epic-hooks` — bind to `epic.create` / `epic.start` /
  `epic.end` events
- `gm-perspective-walk-integration` — agenda-item-level perspective panel in
  Gemba walks
- `gm-perspective-noise-control-ui` — filter + mute + snooze + cap controls
- `gm-ui-phase-indicator` — always-visible chrome badge + palette view
- `gm-ui-purview-browser` — `/project/purviews` view
- `gm-ui-perspective-sidebar` — collapsed sidebar surfacing volunteered
  perspectives
- Update all seed persona TOML files with Personality + Perspective + Purview
  blocks
- Amend `gm-v6f` — QA's gate reframed as a Purview in testing + validating +
  shipping phases with `blocking` authority
- Amend `gm-57b` — formalize the three new config blocks
- Amend `gm-ege` — invariants #27–32

## Not in scope

- Implementation (tracked as follow-ups above)
- Cross-project Purview inheritance (v1 is project-scoped)
- Multi-phase Purviews with different authority per phase (v1 is single
  authority per Purview; upgrade later)
- Phase auto-detection (v1 transitions are manual-or-persona-proposed; never
  auto)

## Definition of Done

- [ ] Design ratified by mike
- [x] `docs/design/persona-pppp.md` committed
- [x] Follow-up beads filed
- [x] `gm-57b` amended with the three new axes
- [x] `gm-v6f` amended — QA's gate authority redescribed in Purview terms
- [x] `gm-ege` amended with invariants #27–32
- [ ] Seed persona roster updated with the new config blocks (follow-up, not
      blocking on this design)
