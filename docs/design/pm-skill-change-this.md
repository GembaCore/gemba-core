---
title: "PM Skill: `change_this` \u2014 analyze impact, consult, apply, roll back"
decision: none
backfill: pending
---

# PM Skill: `change_this` — analyze impact, consult, apply, roll back

**Status:** design · tracked in `gm-d0e`
**Author:** mike (captured by polecat jasper)
**Date:** 2026-04-22 (ratification pending)
**Parent epic:** `gm-858` (PM use-case catalog)
**Depends on:** `gm-uf7` (agentic personas), `gm-518` (PM MVP), `gm-e11.3` (Escalation/HITL surface), `gm-dnc` (sibling `add_to_backlog`)
**Resolves DD:** DD-20 (second agentic PM skill)

## Summary

`change_this` is the PM's second mutation-capable Skill. Given a target bead or
Epic and a free-form description of how the operator wants it changed, the
Skill:

1. Computes a **cascade report** (pure function, no LLM) — downstream
   dependencies, in-progress polecats on affected beads, open merge-queue items,
   linked decisions.
2. Consults the relevant **Purview-holding personas** (Architect for downstream
   design impact, Code Reviewer for in-flight diffs, QA for test regressions,
   PM-for-scope if the change expands scope beyond its parent Epic).
3. Decides whether to proceed, ask a **HITL question**, or refuse under the
   operator's cascade policy.
4. Applies changes as `ExecutedAction`s, each carrying a `reverse_action` so
   the whole invocation is reversible as a single rollback.

The central invariant: **in-progress polecats and in-queue MRs are never
silently disturbed.** Any cascade that would interrupt live work pauses the
Skill with a HITL question. This keeps the operator in the loop exactly where
cost-of-wrong is highest.

Pairs with `add_to_backlog` (`gm-dnc`): that Skill creates; this one edits.
They share the Persona mutation-authority + orchestration + HITL infrastructure
from `gm-uf7`.

## Motivation

Editing existing work is harder than filing new work, because:

- A change may cascade into descendants the operator hasn't looked at in weeks.
- Polecats may be actively implementing the exact bead the operator wants to
  re-scope.
- A scope change may cross a Purview that is blocking in the current phase
  (e.g., Security if auth surface is touched; Architect during design phase).
- Reverting a cascade of edits by hand is tedious and error-prone — operators
  will opt not to change things rather than accept the cleanup cost.

Today the operator either edits the bead directly (losing cascade awareness) or
opens a mail thread with the PM (losing structure). `change_this` closes that
loop: structured input, typed cascade report, persona-consulted decision,
reversible application, durable audit.

## Input contract

`POST /api/v1/consult` with `skill_id: "change_this"`:

```go
type ChangeThisInput struct {
    Workspace     WorkspaceID          `json:"workspace"`
    WorkspaceName string               `json:"workspace_name"`
    AsOf          time.Time            `json:"as_of"`

    // Target of the change. Exactly one must be set.
    TargetBead *WorkItemID `json:"target_bead,omitempty"`
    TargetEpic *WorkItemID `json:"target_epic,omitempty"`

    // What the operator wants to change. Free-form; the PM interprets.
    ChangeDescription string `json:"change_description"`

    // Cascade policy controls refusal behaviour.
    CascadePolicy CascadePolicy `json:"cascade_policy"`

    // Optional caps.
    MaxAffected         int     `json:"max_affected,omitempty"`           // refuse if > this, strict mode only
    MaxInvocationDollars float64 `json:"max_invocation_dollars,omitempty"`

    // Optional authorization envelope — same shape as add_to_backlog.
    Authorization *AuthorizationEnvelope `json:"authorization,omitempty"`

    // Optional guidance (tonal hints, scope reminders).
    Guidance string `json:"guidance,omitempty"`

    // Precomputed context the caller supplies. The PM does not reach back into
    // the bead database in v1; inputs below are the full truth the Skill sees.
    TargetSnapshot      BeadOrEpicSnapshot   `json:"target_snapshot"`
    DownstreamSnapshots []BeadOrEpicSnapshot `json:"downstream_snapshots"` // dep closure, up to configured depth
    InFlight            InFlightSnapshot     `json:"in_flight"`             // polecats + MRs touching any affected bead
    PhaseContext        PhaseContext         `json:"phase_context"`         // current project phase + active purviews
}

type CascadePolicy string
const (
    CascadeStrict     CascadePolicy = "strict"      // refuse if >MaxAffected or any in-flight work touched
    CascadeBestEffort CascadePolicy = "best_effort" // apply as far as safely possible, HITL on conflicts
)

type BeadOrEpicSnapshot struct {
    ID           WorkItemID
    Kind         string   // "bead" | "epic"
    Title        string
    Summary      string
    Labels       []string
    Status       string
    Parent       *WorkItemID
    DependsOn    []WorkItemID
    Blocks       []WorkItemID
    LinkedDecisions []WorkItemID // ratified DDs this touches
    FileSpaceSignature []string
    EditableFields []string // fields the PM is allowed to propose changes to
}

type InFlightSnapshot struct {
    ActivePolecats []PolecatClaim `json:"active_polecats"` // bead ID → polecat + branch + elapsed
    QueuedMRs      []MRClaim      `json:"queued_mrs"`      // branch → MR ID + base + age
}

type PolecatClaim struct {
    BeadID       WorkItemID
    PolecatName  string
    BranchName   string
    StartedAt    time.Time
    LastActivity time.Time
}

type MRClaim struct {
    MRID       string
    BeadIDs    []WorkItemID
    BranchName string
    BaseBranch string
    QueuedAt   time.Time
}

type PhaseContext struct {
    CurrentPhase   string
    ActivePurviews []ActivePurview // from gm-9rv
}
```

The caller (router + context-provider stack) is responsible for walking the
dependency graph to fill `DownstreamSnapshots`. Depth cap default: 3. The PM
does not traverse; it reasons over what it was handed.

## Cascade analysis — pure function

Before the LLM is invoked, the handler runs a pure-function **cascade report**
over the precomputed snapshots. This is the deterministic substrate the Skill
prompt reasons over.

```go
type CascadeReport struct {
    DirectChanges    []ProposedChange     // on TargetBead/TargetEpic itself
    Downstream       []AffectedBead       // with classification
    InFlight         InFlightImpact       // polecats + MRs touching affected beads
    PurviewActivations []ActivePurview    // which Purviews become relevant, given the change
    DecisionImpacts  []WorkItemID         // DDs potentially invalidated
    TotalAffected    int
    ScopeIncrease    bool                 // did the change push work outside the parent Epic?
}

type AffectedBead struct {
    ID             WorkItemID
    Classification AffectClass // "title_drift" | "label_change" | "dep_break" | "scope_shift" | "no_change"
    Reason         string
    RequiresEdit   bool
}

type InFlightImpact struct {
    PolecatsInterrupted []PolecatClaim
    MRsInvalidated      []MRClaim
    HardStopCount       int // anything requiring HITL in strict or best_effort
}
```

Rules:

- **A polecat actively claiming an affected bead is a hard stop.** Always
  HITL — never silently touch the bead.
- **An MR in the merge queue whose base or touched-beads intersect the change
  is a hard stop.** Always HITL — never rebase or invalidate silently.
- **A downstream bead whose title/description no longer makes sense after the
  change is `title_drift`.** Propose an edit; don't force it.
- **A `DependsOn` link that the change breaks (e.g., removing a parent Epic)
  is a `dep_break`.** Always require operator confirmation; never silently
  re-parent.
- **A ratified DD that the change contradicts is a decision conflict.** Always
  HITL; never proceed without explicit override.

The cascade report is part of the PM's prompt context, so the model reasons
over facts, not guesses. Report-generation is tested independently of the LLM.

## Persona consultation matrix

The Skill declares a prompt-time consultation matrix. Which children get
consulted depends on the cascade report:

| Signal | Child persona | Child Skill | Rationale |
|---|---|---|---|
| Cascade includes `area:core` or touches code with `dep_break` classification | Architect | `design_review` | downstream design impact |
| In-flight polecats or MRs present | Code Reviewer | `diff_impact` | diff-aware cost of interruption |
| Change alters test scope or removes tests | QA | `regression_surface` | regression risk |
| Current phase activates a Security Purview (auth-labeled) | Security | `auth_surface_delta` | auth risk |
| Current phase is `shipping` or `validating` | Deployment Engineer | `release_impact` | release mechanics risk |
| Change crosses parent-Epic boundary | PM-self (`epic_merge` or `scope_trim`) | — | scope integrity |

Invariants inherited from `gm-uf7`:

- Every consultation is a typed child Skill call — not free-form chat.
- Max tree depth 3. Cycles forbidden.
- Cost aggregates into the parent `SkillResponse.Cost.CostBreakdown`.
- Child consults are part of the audit record and the tree is rendered in
  `/personas/consults/<invocation_id>`.

## Output contract — JSONL

Content-Type `application/x-ndjson`. Line `type` discriminator. Order:

1. `strategy` — one, first.
2. `cascade_report` — one, the pure-function report as-is.
3. `consult` — zero or more; one per child persona consulted, with a child
   summary.
4. `executed_action` — zero or more; a mutation actually applied.
5. `suggested_action` — zero or more; a mutation awaiting explicit confirm.
6. `hitl_question` — zero or more; surfaces suspension.
7. `rollback_plan` — one; ordered reverse-actions for the whole invocation.
8. `summary` — one, last.

```jsonl
{"type":"strategy","workspace":"gemba","as_of":"2026-04-22T19:15:00Z","model":"claude-opus-4-7","reasoning":"Best-effort cascade. 6 affected beads, 1 polecat in-flight on gm-xyz — will HITL. Architect consult triggered by core edits."}
{"type":"cascade_report","direct_changes":[{"bead":"gm-d0e","field":"title","from":"…","to":"…"}],"downstream":[{"bead":"gm-abc","class":"title_drift","requires_edit":true}],"in_flight":{"polecats_interrupted":[{"bead":"gm-xyz","polecat":"gemba/polecats/jasper","branch":"polecat/jasper-moansdh8"}],"hard_stop_count":1},"total_affected":6,"scope_increase":false}
{"type":"consult","persona":"architect","skill":"design_review","summary":"Change narrows type-system exposure; no module-boundary violation. ACCEPT.","cost_dollars":0.012}
{"type":"hitl_question","id":"hitl-01","question":"Polecat jasper is actively implementing gm-xyz on branch polecat/jasper-moansdh8. Proceeding will invalidate that branch. Allow interruption?","options":["interrupt","defer","skip"],"context":{"bead":"gm-xyz"},"urgency":"high","expires_at":"2026-04-22T20:15:00Z","resume_endpoint":"/api/v1/consult/resume/<invocation_id>"}
{"type":"executed_action","verb":"PATCH","path":"/api/v1/beads/gm-abc","body":{"title":"…new…"},"nonce":"…","reversible":true,"reverse_action":{"verb":"PATCH","path":"/api/v1/beads/gm-abc","body":{"title":"…old…"}}}
{"type":"suggested_action","verb":"PATCH","path":"/api/v1/beads/gm-xyz","body":{"description":"…"},"rationale":"Title drift; awaiting operator confirm because gm-xyz is in-flight."}
{"type":"rollback_plan","actions":[{"verb":"PATCH","path":"/api/v1/beads/gm-abc","body":{"title":"…old…"}}],"strategy":"reverse_chronological"}
{"type":"summary","applied_count":3,"suggested_count":2,"deferred_count":0,"hitl_count":1,"consulted_personas":["architect"],"tokens_in":8450,"tokens_out":2120,"latency_ms":5210,"cost_dollars":0.058,"confidence_overall":0.83}
```

Every line validates against
`/api/v1/skills/change_this/output_schema.json`. Invalid lines are dropped
with a `warning` line inserted and a metric incremented; the persistence layer
never accepts malformed output into the audit log.

## Mutation authority

```toml
[skills.change_this.mutation_authority]
scope = [
  "bead_edit",                # edit title/summary/labels/fields
  "bead_state_transition",    # re-parent, re-link deps
  "bead_close",               # close descendants made redundant by the change
]
requires_explicit_consent = true
auto_approve_budget = 0.50   # dollars — below this, the upfront authorization stands
audit_log_level = "full_prompt"  # destructive surface → always log full prompt
```

Invariants:

- `bead_create` is **out of scope**. `change_this` does not file new work — if
  the change implies scope increase, it HITLs and routes to `add_to_backlog`.
- `session_dispatch` is out of scope. Never silently spawn a polecat.
- Every `ExecutedAction.reversible` must be `true` for destructive surfaces;
  if the change cannot be reversed (e.g., downstream polecat closed a bead
  after the mutation), the `rollback_plan` flags it with a `partial` strategy.

## HITL policies — when to suspend

The Skill suspends with a `hitl_question` in these cases:

1. **Active polecat affected** — any bead in `InFlightImpact.PolecatsInterrupted`.
2. **MR in queue affected** — any bead in `InFlightImpact.MRsInvalidated`.
3. **Cascade exceeds `MaxAffected`** — in strict policy, refuse; in best-effort,
   HITL with the list of beads the operator can trim.
4. **Ratified DD contradicted** — always HITL; never proceed without explicit
   operator override.
5. **Purview violation in active phase** — Architect during design phase, QA
   during validating, Security at any phase with auth-labeled work — if the
   purview-holder raises a `purview_violation` in its consult response,
   suspend.
6. **Cost exceeds `auto_approve_budget`** — HITL to re-authorize.

Suspension uses `EscalationRequest{kind: "hitl_question"}` on the Phase 11
pipeline (`gm-e11.3`). State is persisted; the session can be resumed by POST
to `resume_endpoint` with the operator's answer.

## Reverse actions — rollback semantics

Every `ExecutedAction` records a `reverse_action` at execution time:

| Forward action | Reverse action |
|---|---|
| `PATCH /beads/X {title: "new"}` (from "old") | `PATCH /beads/X {title: "old"}` |
| `PATCH /beads/X {labels: [...new]}` | `PATCH /beads/X {labels: [...old]}` |
| `PATCH /beads/X {parent: "Y"}` (from "Z") | `PATCH /beads/X {parent: "Z"}` |
| `POST /beads/X/close` | `POST /beads/X/reopen` |
| `DELETE /edges/X→Y` | `POST /edges {from: X, to: Y, kind: ...}` |

Rollback is a single operator action: `POST /api/v1/consult/<id>/rollback`.
The rollback plan executes reverse actions in reverse chronological order,
each nonce-gated. If a downstream polecat has closed a bead we edited after
we edited it, the rollback stops at that bead and reports `partial`.

## Refusal paths

`change_this` refuses and emits no mutations when:

- `CascadePolicy == strict` and `CascadeReport.TotalAffected > MaxAffected`.
- `CascadePolicy == strict` and `InFlightImpact.HardStopCount > 0`.
- `Authorization == nil` and `mutation_authority.requires_explicit_consent == true`.
- Budget cap is exceeded by the projected invocation cost estimate.

In all refusal cases, the `SkillResponse` contains the `strategy` +
`cascade_report` + `summary` only; no `executed_action` lines and no
`suggested_action` lines. The operator sees the cascade report and can
re-invoke with a looser policy or a narrower change.

## Prompt template

`internal/skills/change_this/prompts/v1.md` (shipped with this design):

```markdown
You are the Project Manager for workspace {{workspace_name}}.

Today is {{as_of}}. The project is in phase {{phase_context.current_phase}}.
Active purviews: {{phase_context.active_purviews}}.

The operator wants to make this change:
  Target: {{target_bead | target_epic}}
  Description: {{change_description}}
  Cascade policy: {{cascade_policy}} (max_affected={{max_affected}})

A deterministic cascade report has been precomputed:
  {{cascade_report}}

In-flight work touching affected beads:
  {{in_flight}}

Your task:

1. Read the cascade report. Do NOT re-derive it — trust it as ground truth.
2. For each signal in the cascade report that matches the consultation matrix,
   invoke the named child skill. Pass the relevant slice of the cascade
   report. Do not consult personas not matched.
3. If any of these conditions hold, emit a hitl_question and STOP:
   - polecats_interrupted is non-empty
   - mrs_invalidated is non-empty
   - total_affected > max_affected in strict policy
   - any consult returned a purview_violation
   - ratified DD would be contradicted
4. Otherwise, emit ExecutedActions for the changes. Each must include a
   reverse_action. Do not create new beads — that is the add_to_backlog Skill.
5. Close the response with a rollback_plan (reverse chronological order of
   your ExecutedActions) and a summary.

Respond only by calling the emit_change_this tool. Schema is loaded.
```

(Full template is iteration 1; expect amendments as fixtures land.)

## Fixture suite

Under `internal/skills/change_this/testdata/`:

| Fixture | What it tests |
|---|---|
| `simple-title-edit.json` | Single-bead title change, no cascade, no consults |
| `label-change-cascade-1.json` | Label propagates to 3 descendants; no in-flight |
| `dep-break-requires-hitl.json` | Removing a DependsOn edge triggers HITL |
| `polecat-active-refuses.json` | Strict policy + active polecat → refuse with cascade report |
| `polecat-active-best-effort.json` | Best-effort policy + active polecat → HITL question |
| `mr-in-queue-hitl.json` | MR in queue affected → HITL |
| `dd-contradiction-hitl.json` | Ratified DD contradicted → HITL even with authorization |
| `scope-increase-routes-to-add.json` | Change expands scope beyond parent Epic → refuse + suggest add_to_backlog |
| `full-orchestration-roundtrip.json` | Architect + QA consults, 2 ExecutedActions, full rollback_plan |
| `partial-rollback-bead-closed.json` | Downstream polecat closed a bead mid-flight → rollback reports partial |

Each fixture is a JSON file containing `{input, expected_cascade_report,
expected_output_shape}`. The cascade report is asserted exactly (pure
function). The LLM output is asserted structurally (line types, order,
presence of `reverse_action` on every `executed_action`, etc.) — not word-for-word.

## UI surfaces

- **Epic drawer → "Edit with PM…"** — opens a `change_this` invocation drawer
  with cascade preview before commit.
- **Bead detail → "Re-scope…"** — shortcut for single-bead change_this.
- **Consult audit view → rollback button** — for any historic `change_this`
  invocation, a one-click rollback is surfaced while `reversible=true`.
- **HITL inbox (`/personas/hitl-inbox`)** — suspension questions surface here
  with resume-in-context link.

## Budget envelope

Typical invocation cost estimate (Opus 4.7):

| Surface | Tokens in | Tokens out | Dollars |
|---|---|---|---|
| Single-bead edit, no consults | ~3k | ~800 | ~$0.02 |
| Label cascade, 1 consult | ~6k | ~1.5k | ~$0.04 |
| Full consultation tree (3 personas) | ~12k | ~3k | ~$0.08 |
| Large cascade (20+ beads affected) | ~25k | ~5k | ~$0.15 |

`max_invocation_dollars` default: $0.20. Above that, HITL re-auth.

## Architectural invariants (amendments to `gm-ege`)

33. **`change_this` never silently interrupts live work.** An active polecat
    or queued MR on an affected bead is a hard stop. HITL or refuse.
34. **`change_this` never creates new beads.** Scope increase routes to
    `add_to_backlog`.
35. **Every `change_this` mutation is paired with a `reverse_action`.** If a
    mutation cannot be reversed structurally, it is a suggested, not executed,
    action.
36. **Cascade reports are pure functions.** No LLM in the cascade path; LLM
    only reasons over the report.

## Follow-up beads

Filed as follow-ups (not blocking this design):

- `gm-change-this-handler` — Go handler wiring the Skill into the consult
  endpoint; uses `gm-518` PM MVP infrastructure.
- `gm-cascade-report-core` — pure-function cascade analysis + unit test suite
  over the fixture matrix.
- `gm-change-this-fixtures` — author the fixture JSON files above; CI regression
  harness.
- `gm-change-this-rollback` — POST rollback endpoint + partial-rollback
  semantics + audit record.
- `gm-change-this-ui-epic-drawer` — "Edit with PM…" button + cascade preview
  drawer.
- `gm-change-this-ui-bead-rescope` — bead-detail re-scope shortcut.
- `gm-change-this-hitl-integration` — bind to `gm-e11.3` EscalationRequest
  pipeline for suspension/resume.
- `gm-change-this-consult-matrix` — wire the Architect / QA / Code Reviewer /
  Security / Deployment child Skill routing.
- `gm-change-this-dd-guard` — ratified-DD contradiction detector (pure
  function) + HITL branch.
- Amend `gm-ege` with invariants #33–36.
- Amend `gm-858` (PM epic) — mark `change_this` as spec-complete.

## Not in scope

- Creating new beads (that is `add_to_backlog`).
- Dispatching polecats (never a PM concern; always operator or Witness).
- Cross-workspace changes — v1 is single-workspace.
- Graph-query rewriting (e.g., "change every bead with label X to Y in bulk")
   — v1 is single-target. Bulk is a later Skill.
- Live-rebasing MR branches after a cascade — out of scope; MR affected →
   HITL only.

## Definition of Done

- [ ] Design ratified by mike
- [x] `docs/design/pm-skill-change-this.md` committed
- [x] Follow-up beads listed (to be filed under `gm-858` when ratified)
- [ ] `gm-ege` amended with invariants #33–36 (follow-up)
- [ ] Fixture matrix implemented (follow-up `gm-change-this-fixtures`)
- [ ] Handler implementation lands once `gm-518` + `gm-e11.3` are closed
