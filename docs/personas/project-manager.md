# Project Manager persona

> **Beads:** gm-518 (PM persona MVP), gm-twp2 (dispatcher + transport), gm-57b (Personas primitive), gm-2yg (Coach/Manager split)
>
> **Status:** v1 ships exactly one skill — `epic_order`. Additional skills (`add_to_backlog`, `change_this`, `what_remains`, `sprint_retro`, …) land per their own beads under gm-858.

The Project Manager is gemba's default advisor for the Plan view and execution batching. It's a **Coach** (variety = `coach`) — every recommendation carries a `suggested_action`, but the operator (or a Manager-variety persona) is responsible for actually applying it. The PM never files beads, never writes to the WorkPlane, never exceeds its declared budget.

## Persona definition

Lives at `<workspace>/.gemba/personas/project-manager.toml`. The dispatcher loads the registry at boot from `cfg.BeadsDir/.gemba/personas`. Reference fields:

| Field | Value |
|---|---|
| `id` | `project-manager` |
| `name` | PM |
| `role` | Project Manager |
| `variety` | `coach` |
| `scope.kind` | `project` (sees every repository in the workspace, never pinned to one) |
| `skills` | `["epic_order"]` |
| `personality.id` | `calm` (gm-1w7) |
| `perspective.statement` | `coordination, sequencing, scope discipline, escalation triage` (gm-1w7) |
| `perspective.volunteer_mode` | `on_demand` (gm-1w7) |
| `purview` | _intentionally omitted — invariant #31_ (gm-9rv) |
| `model.vendor` | `anthropic` |
| `model.model` | `claude-opus-4-7` |
| `model.max_tokens` | 8192 |
| `model.temperature` | 0.2 |
| `budget_policy.max_per_invocation_dollars` | 0.25 |
| `budget_policy.max_per_day_dollars` | 10.00 |
| `context_providers.reads` | `project_summary`, `project_state`, `decisions_log`, `sprint_context`, `active_escalations` |

## PPPP (Personality + Perspective + Purview)

The PM ships with concrete `[personality]` and `[perspective]` blocks per the [gm-9rv design](../design/persona-pppp.md). Purview is **intentionally omitted** — invariant #31 says the PM coordinates but does not own a gateable domain.

- **Personality (`calm`)** — calm, structured, decision-supportive voice. The operator reads twenty rationales in a row, so the PM's voice is even-keeled and short. Decorative only (invariant #27): swapping personalities never changes correctness, only wording.
- **Perspective (`on_demand`)** — the PM's lens is coordination, sequencing, scope discipline, and escalation triage, but it volunteers commentary only when the operator explicitly asks "any perspectives?". Unsolicited PM chatter on every conversation would drown the surface; the PM is a response-shaped persona, not a perspective-shaped one. Triggers like `epic_state_changed`, `sprint_overrun`, `ready_set_empty`, and `parallel_conflict` are declared so future on-demand prompts can deep-link the relevant signals.
- **Purview** — none. The PM is the orchestrator. Domain gates (Architect for design, Security for auth, QA for testing/validating/shipping, Deployment Engineer for shipping, Documentarian for docs) live on the personas that own those domains; the PM coordinates them.

Full TOML lives in the workspace; this doc captures intent. When the persona file changes restart `gemba serve` — the registry loads once at boot.

## Skill: `epic_order`

Rank a set of candidate Epics for staging. Takes pre-computed signals (deps, file-space, budget cost) and returns a JSONL stream of ranked recommendations with rationales, plus warnings + deferrals.

`epic_order` is the **planning** half of gemba's two-tier ranking surface — sprint composition over an operator-curated candidate set, with confidence scores, narrative rationale, warnings, and deferred items per `RecommendationLine`. It is not the same system as the dispatch-time per-bead `Selection` ranker that powers the `/coach` affinity grid; the two answer different questions at different time horizons. See [Dispatch vs Planning](../concepts/dispatch-vs-planning.md) for the full split.

The skill surfaces in the SPA at `/sprints` (post-`gm-szz0.1`) and as the PM panel's "Recommend order" quick action; mid-walk the dispatcher may also invoke it as a sub-skill when an agenda item calls for sprint composition. Every emitted `recommendation` line carries a `suggested_action` and an Apply button — the persona never auto-applies. The operator clicks Apply on the rows they want; until `gm-twp2.1` lands, the operator (or their tooling) executes the recorded `verb`+`path`+`body` themselves. Coach variety means consent at every step.

### Input shape — `EpicOrderInput`

Authoritative source: `internal/skills/epic_order/types.go`. Wire shape:

```jsonc
{
  "workspace": "gemba",                  // workspace id; required
  "workspace_name": "Gemba",             // human-readable label spliced into {{workspace_name}}
  "as_of": "2026-04-26T12:00:00Z",       // freeze-point timestamp; required
  "candidate_epics": [                   // required, non-empty
    {
      "epic_id": "gm-e3",
      "title": "Phase 3: Core contracts",
      "summary": "domain types, adaptor interfaces, conformance harness",
      "ui_state": "on_deck",             // current planner column
      "current_rank": null,
      "parent_epic": "gm-root",
      "derived": { /* arbitrary signals the planner pre-computed */ }
    }
  ],
  "constraints": {
    "max_dollars": 0.25,
    "max_latency_seconds": 30,
    "min_confidence": 0.6
  },
  "sprint_context": null,                // optional; presence triggers budget-aware ranking
  "recent_completions": [],              // optional; calibrates velocity estimates
  "open_escalations": [],                // optional; escalated Epics jump in priority
  "guidance": "focus on unblocking UI work",  // optional operator intent
  "max_recommendations": 0               // 0 = all candidates eligible
}
```

The dispatcher's `Skill.ValidateInput` rejects unknown fields with a 400 — the SPA's typed shape stays in lockstep with the Go struct.

### Output stream — `emit_ordering` MCP tool

The PM emits structured output through the `gemba-mcp` `emit_skill_output` tool in a single tool call. The dispatcher routes the call into `Skill.ValidateOutputLine` per element. Order rules the model is told to honor:

1. Exactly one `strategy` line first
2. Zero or more `recommendation` lines in rank order
3. Zero or more `warning` / `deferred` lines
4. Exactly one `summary` line last

#### Line types

```jsonc
// Type discriminator on every line.
// "strategy" | "recommendation" | "warning" | "deferred" | "summary"

// strategy — once per consult; framing + freeze-point echo
{ "type": "strategy", "workspace": "gemba", "as_of": "2026-04-26T12:00:00Z",
  "model": "claude-opus-4-7", "reasoning": "top-down by blocker depth then sprint cost",
  "total_considered": 12, "total_ranked": 8, "filtered": 4,
  "filter_reason": "trimmed for budget" }

// recommendation — one per ranked Epic
{ "type": "recommendation", "rank": 1, "epic_id": "gm-e3",
  "parallel_group": "A", "confidence": 0.85,
  "rationale": "Unblocks every Phase 4+ epic; smallest cost in the ready set.",
  "estimated_tokens": 12000,
  "suggested_action": {
    "verb": "PATCH", "path": "/api/work-items/gm-e3",
    "body": { "user_order": 1 }
  } }

// warning — flag an issue (parallel_conflict / dep_cycle / budget_overrun / missing_signal)
{ "type": "warning", "kind": "parallel_conflict", "epic_ids": ["gm-e6","gm-e7"],
  "detail": "Both touch internal/adapter/native; split into separate groups." }

// deferred — actively can't act on it (distinct from filtered)
{ "type": "deferred", "epic_id": "gm-r5vz",
  "reason": "blocked on external decision per gm-twp2" }

// summary — once per consult; closing rollup
{ "type": "summary", "ranked": 8, "warnings": 1, "deferred": 1,
  "estimated_total_tokens": 84000, "notes": "Recommend executing batch A then B sequentially." }
```

Lines that fail `Skill.ValidateOutputLine` (missing `type`, unknown discriminator, schema mismatch on the typed shape) drop into `consult.LineErrors` rather than into `ValidatedLines`. The audit log preserves the rejected raw JSON so a hallucinated tool call is visible post-hoc.

JSON Schema for the wire shape: GET `/api/skills/epic_order/output_schema.json` (Content-Type `application/schema+json`).

## Lifecycle — start to apply

```
operator: clicks "Recommend order" on /coach
  ↓
SPA: POST /api/consults
  body: { persona_id: "project-manager", skill_id: "epic_order",
          workspace, raw_input: <EpicOrderInput>, spawn: true }
  header: X-GEMBA-Confirm: <uuid>
  ↓
server: persona.Dispatcher.Begin
  - PersonaCanInvoke check (PM must list epic_order in skills)
  - Skill.ValidateInput (rejects malformed input as 400)
  - Compose prompt envelope (gm-3d6 layers: project values + workspace
    values + persona system_prompt + skill prompt + user message)
  - Register consult with status=running
  ↓
server: persona.NativeSpawn (post-Begin hook)
  - Write composed prompt to <workspace>/.gemba CLAUDE.md sentinel block
  - op.StartSession with gemba:session_id_override = consult.ID
  - tmux pane spawns Claude Code with GEMBA_SESSION_ID = consult.ID
  ↓
agent (Claude Code in spawned pane):
  - Reads CLAUDE.md preamble (system + user)
  - Calls emit_skill_output MCP tool with ordered line array
  ↓
gemba-mcp: writes GembaSkillOutput frame to session log
  ↓
bridge: tail picks up the frame, translateGembaSkillOutput → OrchestrationEvent
  ↓
events.Hub: publishes events.SkillOutputEmitted
  ↓
persona.FanFromHub: pulls event, calls Dispatcher.Receive(consult.ID, lines)
  - Skill.ValidateOutputLine per line
  - Append to consult.ValidatedLines / LineErrors
  ↓
SPA: drawer polls /api/consults/{id} (1.5s) — lines stream in
  ↓
operator: clicks "Apply" on recommendation row #2
  ↓
SPA: POST /api/consults/{id}/apply/2
  header: X-GEMBA-Confirm: <fresh uuid>
  ↓
server: Dispatcher.Apply
  - Validates idx in range, not already-applied
  - Appends to consult.AppliedIdx
  - Returns the recommendation line (so SPA shows the suggested_action)
  ↓
operator: executes the suggested_action (manually until gm-twp2.1 lands)
```

## API surface

All routes are nonce-gated where they mutate; the `X-GEMBA-Confirm` middleware caches replays per nonce so a SPA double-click can't fork or double-apply. The full path list:

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/skills` | List registered skills (id / name / description / output_tool_name / has_output_schema) |
| `GET` | `/api/skills/{id}` | One skill's metadata + output tool description |
| `GET` | `/api/skills/{id}/output_schema.json` | JSON Schema for the skill's structured-output array |
| `POST` | `/api/consults` | Begin a consult (X-GEMBA-Confirm). Returns `consultSummary`. `body.spawn=false` for prompt-only dry-run. |
| `GET` | `/api/consults` | List in-flight + recent consults |
| `GET` | `/api/consults/{id}` | Consult detail. Live shape (composed prompt + validated_lines) when in-memory; archived shape (composed_persisted=false) when audit-log-only. |
| `POST` | `/api/consults/{id}/apply/{idx}` | Record an applied recommendation (X-GEMBA-Confirm). Idempotent: 409 on duplicate idx, 404 if consult finished. |

## Observability

- **`/insights/personas`** — one row per persona seen in `/api/consults`, with consult count, last activity, status badges (running / failed), and token totals. 5s polling.
- **`/coach`** — "Recommend order" button on the header opens the consult drawer. Polls `/api/consults/{id}` every 1.5s; renders ValidatedLines as they arrive; per-recommendation Apply buttons update the applied_idx without waiting for the next poll.
- **`~/.gemba/persona/consults/<YYYY-MM-DD>/<consult-id>.json`** — append-only audit log, partitioned by day. `Dispatcher.Finish` writes it; the GET fall-through reads it when an archived consult is queried.

## Configuration boundaries

- The persona's TOML is the authoritative shape (id / name / role / variety / scope / skills / system_prompt / model / budget_policy / context_providers). Restart `gemba serve` to pick up edits — the registry is loaded once at boot.
- The skill's input/output shape is authoritative in `internal/skills/epic_order/types.go`. Wire-shape changes require updating the JSON schema in the same package and regenerating any TypeScript codegen the SPA depends on.
- Spawn binding: `cmd/gemba serve` installs `persona.NativeSpawn(op, "claude")` when an OrchestrationPlane is bound — every persona dispatches as agent type `"claude"` for v1. Per-persona agent-type selection is a follow-up (file under gm-858 when the next persona introduces the need).

## Known limits

- **`suggested_action` is recorded, not executed.** The operator (or their tooling) reads the response's `line.suggested_action` and dispatches the verb+path+body manually. The applier-executor that turns this into a real WorkPlane mutation is **gm-twp2.1**.
- **Audit-log archived consults are reduced.** `composed_persisted=false`, `validated_lines` empty. The drawer renders an "archived" badge so the operator knows the live drawer state isn't preserved.
- **No SSE push yet.** The drawer polls `/api/consults/{id}` every 1.5s; `/insights/personas` polls `/api/consults` every 5s. SSE upgrade for `events.SkillOutputEmitted` is a follow-up — `events.FanFromHub` already subscribes server-side so the SPA upgrade is the remaining work.
- **Model auth is Claude Code's problem.** The operator authenticates `claude` once; gemba never sees `ANTHROPIC_API_KEY`. The dispatcher's audit-log row carries the model + token usage Claude Code reports; if the model changes between sessions the row shows the change.

## Gemba walk

When the PM is invoked inside an active **Gemba walk** (`gm-rfy` core, `gm-wgv` HTTP, `gm-4p6` prompt extension), the dispatcher appends a walk-mode framing fragment to the persona's base system prompt. The fragment teaches the model the per-item rhythm: **frame** the topic, **summarize** the source context, **propose** 1-3 concrete actions as `SuggestedAction`s, then **yield** to the operator. The operator responds with ratify / modify / reject / defer / handoff before the persona moves to the next agenda item. Brevity is load-bearing — the operator may have twenty items to walk in a single session.

Wiring: the HTTP `/walks/:id/turn` handler detects an active walk via `walk.IDFromContext` on the request context and passes the walk id through `BeginRequest.WalkID`; `Compose` then appends the extension via `WalkPromptExtension`. Non-walk consults leave `WalkID` empty and the PM persona behaves exactly as documented above. On resume (`POST /walks/:id/resume`), the handler calls `persona.RenderResumePrompt(w, userMsg)` once to prepend a deterministic synopsis (`walk.BuildResumeContext`) to the operator's first post-resume message so the persona has lossless state without depending on an LLM-condensed summary. v1 ships SuggestedActions only — the agentic mutation-authority path (`gm-uf7`) lands later and will extend this contract without breaking it.

## Related design

- `docs/design/persona-pppp.md` — Persona axes (Personality / Perspective / Purview / Phases) — gm-9rv
- `docs/design/pm-skill-change-this.md` — sketches `change_this` (post-MVP skill)
- `docs/design/work-planning.md` §5 — Coach mode + auto-dispatch — calls the PM as the recommend-order driver
- `internal/skills/epic_order/doc.go` — skill-internal contract (lookup conventions, vocabulary)
