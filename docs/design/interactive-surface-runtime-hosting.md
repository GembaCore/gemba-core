---
title: "Interactive surface and runtime hosting model"
decision: gm-5n6o
---

# Interactive Surface and Runtime Hosting Model

Status: accepted; current implementation complete for gm-ddpy

Date: 2026-05-01

## Summary

Gemba should use one product model for interactive operator work:
an **Interaction**. Interactions are stateful exchanges where the
operator reviews, edits, answers, ratifies, applies, or defers a
proposed change.

The Right-Hand Panel should become the default host for scoped
interactions inside an existing workspace. Full-page routes remain for
workspace-level flows that have no natural board/detail context, such
as first-project onboarding. The selected OrchestrationPlane owns any
agent runtime used by the interaction: native uses local tmux/iTerm/
Terminal sessions; Gas Town uses mayor or crew-hosted sessions.

Principle:

> Gemba owns the interaction shell. The active orchestration plane owns
> the runtime. The RHP is the default cockpit for scoped interactive work.

## Implementation Status

The first implementation slice is in place:

- `web/src/interactions/types.ts` defines the frontend
  `InteractionSession` contract, scoped target codec, and runtime-host
  routing helper.
- `web/src/components/interactions/InteractionPanel.tsx` renders the
  shared transcript, draft, suggested-action, evidence, and capability
  regions.
- `web/src/components/interactions/NewProjectDraftPreview.tsx` and
  `web/src/components/interactions/NewProjectRatifyModal.tsx` provide
  the reusable Onboarder preview and ratification controls while
  leaving `/onboard` full-page before a project exists.
- `web/src/components/rhp/details/InteractionDetail.tsx` registers an
  `interaction` RHP detail kind.
- `web/src/components/rhp/HelpTab.tsx` registers the default RHP
  **Help** tab first, with `StatusTab.tsx` registered next. Status
  surfaces the top run metrics, active sessions, open escalations,
  adaptor health, and workspace counts.
- `POST /api/v1/interactions:ensure` creates or returns the
  process-local interaction record for a scoped exchange. The response
  is the normalized InteractionSession shape the SPA renders.
- Work item and epic detail headers expose a **Discuss** action that
  opens the scoped RHP interaction.
- Session rows, escalation cards, and active Gemba Walk items expose
  scoped RHP interaction actions for session supervision, escalation
  triage, and walk review.
- The interaction **Dispatch runtime** action opens the existing
  session launcher for the scoped bead/epic when an orchestration plane
  is bound; unavailable actions explain the missing backend contract.
- Status session and escalation rows open scoped interaction tabs, so
  the passive cockpit can hand off into an active operator exchange
  without changing routes.
- Gas Town runtime-host selection is represented in the frontend:
  project/milestone/epic/escalation/walk interactions route to the
  mayor, while leaf work-item interactions route to crew. The server
  now applies the same rule from the active OrchestrationPlane manifest.
- `/onboard` uses the shared transcript component and gates the
  Onboarder LLM behind deterministic setup collection. It asks for
  new/existing/import, project name, GitHub project, orchestration
  layer, native/Gas Town location, and source-analysis backend before calling
  `/api/v1/newproject/start`.
- `POST /api/v1/onboarding/setup` executes the deterministic setup
  transaction for the selected path. It creates or adopts the worktree,
  initializes local Beads metadata where possible, syncs clean existing
  repos, seeds Claude/Codex setup files with Beads and source-analysis
  MCP guidance, verifies or best-effort installs GitNexus, runs initial
  GitNexus analysis for existing/imported codebases, probes MCP
  commands, and for new projects best-effort creates/verifies the GitHub
  repo, configures `origin`, commits a setup snapshot, pushes `main`,
  and asks Gas Town to add/create the rig and an onboarding polecat when
  Gas Town orchestration is selected.

Remaining follow-on work is primarily hardening rather than product
contract completion: replace the process-local interaction store with
durable persistence, graduate the CLI-backed GitHub/Gas Town setup
calls into adaptor-native APIs where available, and deepen transcript
streaming from runtime hosts.

## Problem

Interactive surfaces have grown independently:

- `/onboard` hosts the Onboarder conversation and plan preview.
- `/coach` hosts dispatch coaching and affinity analysis.
- `/walk` and `/walks/:id` host Gemba Walk review.
- `/escalations` hosts HITL triage.
- RHP detail tabs host work-item detail, status, sessions, and related
  controls.
- Gas Town owns mayor/crew/polecat sessions outside the native local
  terminal model.

These are all valid product surfaces, but the underlying shape is the
same: a scoped interaction with transcript, draft state, suggested
actions, and apply/ratify/resolve controls. Treating each as a bespoke
page or panel creates inconsistent state handling and makes it unclear
where future persona skills should appear.

## Definitions

### Interaction

An Interaction is a durable or semi-durable operator-facing exchange.

Examples:

- Onboarder project planning.
- PM "change this" / scope-trim consult.
- Escalation triage.
- Gemba Walk agenda item review.
- Evidence review.
- Decision ratification.
- Refinement of an epic or bead.
- Session supervision that needs operator input.

### Interaction Host

The UI location that renders the interaction.

Recommended hosts:

- **RHP tab**: default for scoped interactions inside an existing
  workspace.
- **Full-page route**: allowed when there is no project/board context,
  the interaction needs wide comparison space, or it is a top-level
  operational mode.
- **Modal**: only for short confirmation or small bounded edits; not
  for transcript-bearing interactions.

### Runtime Host

The system that runs the agent or service backing the interaction.

Examples:

- local native terminal pane,
- Codex driver process,
- Gas Town mayor session,
- Gas Town crew/polecat session,
- server-side persona call with no long-running terminal.

The runtime host is not necessarily the UI host. The RHP can display an
interaction whose backing runtime is a Gas Town crew session.

## Current Surface Mapping

| Current surface | Current role | Proposed interaction shape | Default future host |
| --- | --- | --- | --- |
| `/onboard` | New-project conversational planning | `project_onboarding` interaction with transcript + plan preview + ratify | Full page before project exists; shared Interaction components |
| RHP Status tab | Operational status | `status_home` passive interaction shell with active-session launch points | RHP pinned tab after Help |
| RHP detail tabs | Work item/session details | `detail` interaction tabs by scope | RHP |
| `/coach` | Dispatch affinity and "what next" | `dispatch_coach` interaction or status-derived view | RHP for scoped prompts; page may remain for broad grid |
| `/walk` | Structured Gemba Walk | `walk` interaction with agenda, turns, decisions | RHP for scoped walk items; full page for whole-walk mode |
| `/escalations` | HITL triage inbox | `escalation_triage` interactions | RHP tab from card/session; page remains as inbox |
| PM skills | Planning/refinement consults | `persona_consult` interaction with suggested actions | RHP |
| Session peek | Runtime transcript/supervision | `session_supervision` interaction | RHP, backed by native or Gas Town runtime |

## RHP Interactive Mode

The RHP should support two top-level modes:

1. **Help mode**
   - Default route-aware landing pane.
   - Explains the current surface and points operators toward the next
     useful action.

2. **Status mode**
   - Passive cockpit.
   - Shows active sessions, counts, escalations, decisions, auto-created
     beads, token/cost counters, generated LOC, and other run metrics.

3. **Interactive mode**
   - Active operator exchange.
   - Renders transcript, draft/preview, suggested actions, and
     controls.
   - Opens as tabs next to Status and detail tabs.

Interactive tabs should follow the already chosen RHP tab mechanics:

- A return/title area on the left.
- Tabs across the top.
- Expansion control on the right.
- Draggable divider.
- Detail tabs accumulate and can be closed; do not silently replace
  same-kind tabs.
- Breadcrumbs appear where hierarchy helps, e.g.
  `Milestone > Epic > Bead`.
- Long sections use "show more" rather than making the tab jump.
- Clicking a scoped item opens a detail tab.

## Interaction Anatomy

Every interaction should expose a common shape, even if not every field
is present:

| Field | Purpose |
| --- | --- |
| `id` | Stable interaction id. |
| `kind` | `onboarder`, `pm_consult`, `escalation`, `walk`, `session`, etc. |
| `scope` | Project, repository, milestone, epic, bead, session, or escalation. |
| `host` | UI host: RHP, full-page, modal. |
| `runtime` | Runtime host: native, codex, gastown-mayor, gastown-crew, server-persona. |
| `status` | Drafting, waiting, prompting, applying, done, canceled, failed. |
| `transcript` | User/assistant/tool turns when available. |
| `draft` | Proposed plan, patch, decision, answer, or work-item edits. |
| `suggested_actions` | Concrete actions the operator may ratify/apply. |
| `evidence` | Linked logs, files, commits, screenshots, test output. |
| `decision_log` | Accepted/rejected/deferred choices with rationale. |
| `capabilities` | What controls the host can show. |

## Runtime Hosting Rules

### Native OrchestrationPlane

Use local terminal-backed sessions:

- Claude Code: native pane with Claude hooks and `gemba-mcp`.
- Codex: `gemba-codex-driver` wrapping `codex exec`, with
  session-scoped `gemba-mcp`.
- Shell/manual: local shell pane plus prompt-command/sentinel support.

RHP owns the interaction UI. Native owns pane spawn, worktree, lifecycle
events, and bridge frames.

### Gas Town OrchestrationPlane

Use Gas Town to host interactive agent sessions.

Recommended split:

- **Mayor-hosted interactions**
  - project-level planning,
  - cross-epic decisions,
  - escalation triage,
  - routing and staffing decisions,
  - broad Gemba Walk synthesis.

- **Crew-hosted interactions**
  - implementation work,
  - bead execution,
  - repo-local debugging,
  - code generation,
  - evidence production.

Gemba should not silently create local native panes while Gas Town is
the active orchestration plane. It should request a Gas Town session
through the adaptor and render the resulting interaction in the RHP or
appropriate page.

### Server-Side Persona Calls

Some interactions do not need a long-running runtime session:

- quick PM recommendation,
- deterministic plan transform,
- lightweight documentarian advice,
- validation/explanation.

These should use a server-side persona/skill invocation and still land
in the same Interaction model. The UI should not care whether the
runtime was a terminal session or a server-side call.

## Full-Page Exceptions

Full-page interactions are allowed when:

1. No workspace exists yet.
2. The interaction is a broad operational mode rather than a scoped
   detail, such as a full Gemba Walk agenda.
3. The interaction needs wide comparison space that would be cramped in
   the RHP.
4. The route is a deep-linkable inbox or dashboard, such as all
   escalations or all sessions.

Even when a full page remains, it should use the same Interaction
components and API concepts as the RHP.

`/onboard` is the key exception today: before project ratification,
there is no board context for an RHP. It remains full-page, while its
conversation pane, plan preview, transcript model, ratify controls, and
state machine use reusable Interaction components.

The Onboarder full-page exception still has an important boundary:
branching questions that can be answered deterministically must be
asked before the LLM launches. Gemba asks whether the user is creating
a new project, adopting an existing project, or importing a project;
then collects the project name, GitHub project identity, orchestration
layer, native worktree or Gas Town location, and source-analysis
backend. Existing/imported codebases default to GitNexus. Only after
that setup is explicit and required setup checks have completed should
the Onboarder LLM coach the user through project intent, milestones,
epics, beads, acceptance criteria, risks, and next actions.

Ownership split:

| Setup concern | Owner |
| --- | --- |
| New / existing / import branch | Gemba deterministic setup UI |
| Beads database create/adopt/sync | Gemba server setup transaction |
| GitHub repo verify/create/push | Gemba server setup transaction |
| Native worktree path and sync | Gemba server setup transaction |
| Gas Town location, boot, project init | Gas Town adaptor through Gemba setup transaction |
| Mayor vs crew mounting | Active OrchestrationPlane policy |
| GitNexus install/analyze for existing code | Gemba server setup transaction |
| Beads and source-analysis MCP connection test | Gemba server setup transaction |
| LLM setup-file updates for Beads and analysis | Gemba server setup transaction |
| Product intent and plan synthesis | Onboarder LLM |
| Milestone/epic/bead proposal | Onboarder LLM, validated by Gemba |

## Gas Town Mayor and Crew Session Hosting

When Gemba runs with the Gas Town adaptor:

1. Starting an interaction asks Gas Town for a host:
   - mayor for project-level coordination,
   - crew for implementation/work-item execution.
2. Gas Town returns a session id, transcript/peek capability, and
   supported controls.
3. Gemba opens or updates an RHP interaction tab bound to that session.
4. Escalations, mail, prompts, and status updates flow through the Gas
   Town adaptor, not local hook files.
5. If the interaction produces work changes, the WorkPlane records the
   resulting beads/evidence/decisions.

This preserves the authority boundary: Gas Town owns its polecats,
crews, mayor coordination, and tmux/session layout; Gemba owns the
operator cockpit and normalized product state.

The current onboarding setup implementation uses the installed `gt`
CLI as a conservative handoff boundary: it verifies or creates the
rig from the selected GitHub remote when possible and creates an
onboarding polecat for the project. Failures are returned as setup
warnings so the operator sees an honest boundary instead of the LLM
silently taking over infrastructure work.

## Capability Model

Interactions should render controls based on runtime capabilities.

Examples:

| Capability | Meaning |
| --- | --- |
| `transcript.peek` | Host can show live or recent transcript. |
| `input.send` | Operator can send input to the runtime. |
| `pause.resume` | Runtime supports pause/resume. |
| `suggested_actions.apply` | Interaction can apply structured actions. |
| `ratify` | Interaction can commit a draft plan or decision. |
| `evidence.attach` | Runtime can attach structured evidence. |
| `escalation.respond` | Operator can answer blocker/question in place. |
| `runtime.rehost` | Interaction can move between mayor/crew/native hosts. |

The RHP should hide unavailable controls, not render dead buttons.

## Implementation Direction

### Phase 1: Contract and Shared Components

- Add an `InteractionSession` API/type.
- Extract shared transcript, suggested-action, draft-preview, and
  ratify/apply controls from `/onboard`, walks, escalations, and PM
  panels.
- Make the RHP able to open an interaction tab.

### Phase 2: RHP Interactive Mode

- Add Status/Interactive mode mechanics to the RHP.
- Let work-item, session, escalation, and walk clicks open interaction
  tabs.
- Preserve full-page routes as deep links and broad dashboards.

### Phase 3: Runtime Routing

- Add a runtime-host selector:
  - native local,
  - Codex driver,
  - Claude native,
  - Gas Town mayor,
  - Gas Town crew,
  - server persona.
- Route new interactions through the active OrchestrationPlane.
- Prevent accidental native-pane launches when Gas Town is active.

### Phase 4: Migration

- Move Onboarder internals to shared Interaction components while
  leaving `/onboard` full-page.
- Rehome scoped PM and escalation interactions into the RHP.
- Reconcile `/coach` and `/walk` with the Interaction model without
  removing useful full-page overview modes.

## Integration Checklist

New interactive surfaces should pass this checklist before shipping:

| Check | Requirement |
| --- | --- |
| Scope | The interaction has a concrete scope: project, milestone, epic, bead, session, escalation, or walk. |
| Determinism | Factual branching, local paths, repo identity, runtime selection, and setup side effects are handled by Gemba before any LLM is launched. |
| Beads awareness | Runtime setup files tell the LLM that Beads is authoritative for milestones, epics, beads, decisions, dependencies, and evidence. |
| Source analysis | Existing codebases prompt for analysis backend, default to GitNexus, run initial analysis, and test the source-analysis MCP before launch. |
| New project setup | Fresh projects still seed Beads and source-analysis MCP guidance so agents know where to look once code exists. |
| LLM boundary | The LLM is limited to coaching, synthesis, recommendations, and structured proposal generation. |
| Host | Scoped interactions default to RHP tabs; full-page routes are reserved for pre-project, broad dashboard, or wide comparison modes. |
| Runtime | The active OrchestrationPlane selects the runtime host; Gas Town work must not silently spawn native panes. |
| Capabilities | Controls render from capabilities and unavailable actions explain the missing contract. |
| Transcript | Transcript-bearing UI uses shared Interaction components. |
| Actions | Suggested actions are typed, nonce-gated when destructive, and persist evidence/decisions when applied. |
| Tests | Unit or integration tests assert the host, runtime routing, and no-premature-LLM-launch rules. |

## Design Decision

Adopt the Interaction model and RHP interactive mode as the default for
new scoped operator exchanges. Keep full pages for broad or pre-project
flows. When Gas Town is active, runtime-backed interactions must be
hosted by Gas Town mayor/crew sessions rather than local native panes.

## Open Questions

1. Should every Interaction persist to the WorkPlane, or should some
   remain session-local until ratified?
2. Should Gas Town decide mayor vs crew automatically, or should Gemba
   pass a requested host and let Gas Town override?
3. Should `/coach` become mostly an RHP/status surface, or remain a
   broad page for dispatch-grid inspection?
4. Should interaction transcripts share one store with session
   transcripts, or stay separate with links between them?
5. How much of `/onboard` should be migrated before we build new PM
   interaction tabs?
