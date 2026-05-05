# Product Capability Matrix

Date: 2026-04-30

This matrix is the recommended living source of truth for what Gemba currently
delivers, what is beta, what exists only as backend/API/internal capability, and
what is still planned. It is based on the Markdown product docs, the live
Dolt-backed Beads project, current code inspection, and the local GitNexus
analysis for commit `b6beaed`.

## Status Legend

| Status | Meaning |
| --- | --- |
| Shipped | Usable in the product today through visible UI, supported API, and current code paths. |
| Beta | Implemented enough to use, but still has known gaps, backend limitations, or acceptance work open. |
| API-only / Internal | Core code or server API exists, but the full user-facing product experience is not present. |
| Planned | Ratified or documented product direction with little or no current product implementation. |
| Needs reconciliation | Code, docs, and Beads status do not yet tell the same story. |

## Capability Matrix

| Capability | Status | Product intent | Current evidence | Drift / gap | Next action |
| --- | --- | --- | --- | --- | --- |
| Local sidecar shell | Shipped | Run Gemba as a local Go binary with embedded React SPA, OpenAPI, config, capabilities, and adaptor discovery. | `README.md`, `docs/ui-spec.md`, server routes for health/version/config/OpenAPI/capabilities, embedded SPA. | Some future nouns are exposed as stubs. | Keep capability discovery strict so unsupported future surfaces are not implied as available. |
| WorkPlane Beads integration | Shipped | Read and mutate work items, labels, dependencies, notes, status, and acceptance data through a WorkPlane adaptor. | Work item routes, Beads/Dolt adaptor packages, live Dolt server project data. | Ready-work route is still stubbed. | Implement or hide `/api/work-items/ready`. |
| Beads-only mode | Shipped | Run Gemba as a Beads viewer/manager without project or orchestration setup. | `--beads-only`, `--beads-read-only`, `--beads-url`, `--beads-history`, Board status columns from Beads state, Refine table / hierarchy / swimlane views, shared order selector, Status Beads health, RHP Beads history, Graph availability with milestone/epic/search/state/kind filters, create/edit/delete routes, writable Dolt URL mode, a self-contained Docker quickstart image with `bd` + seeded sample data, and a standard unseeded Docker image for mounted real work. | Source selection from Help is still deterministic setup rather than LLM coaching. | Keep Beads-only docs and hidden-route gating aligned as new Beads surfaces ship. |
| Board surface | Shipped | Let operators inspect and act on project work from the primary status board. | SPA routes for `/board`, board subroutes, work-item APIs, Ready / In Progress / Done status columns, shared order selector, milestone/epic focus selectors, card status/attention pills, and legacy deep-link layouts for old bookmarks. | Board exists, with legacy layout aliases still present but no longer exposed as primary controls. | Keep route names and card-state vocabulary aligned with `docs/ui-spec.md`. |
| Refine / Backlog surface | Shipped | Let operators clean up backlog, improve work items, and refine planning state outside the execution kanban. | `/refine`, `/backlog` redirect, work-item patch routes, dense table, milestone -> epic -> bead hierarchy, and planning swimlanes. | Backlog remains a supported state and legacy route alias, but not a default Board lane. | Keep `/refine` as the primary backlog and planning-view UX. |
| Recent work view | Shipped | Let operators catch up on recently created or recently completed work from Plan without adding another primary pane. | Board named views `recent` and `done-recent`, legacy `/recent` deep link, `workItemViews` registry. | Broader activity belongs in walks, sessions, or Insights rather than the Recent filter. | Keep Recent as a Board filter/list view, not a separate inbox. |
| Sessions | Shipped | Start, inspect, delete, and supervise orchestration sessions. | `/api/sessions`, `/api/sessions/{id}/peek`, `/sessions` SPA route. | Backend support varies by adaptor and mode. | Ensure UI gates controls by adaptor capabilities. |
| Escalations / HITL | Shipped | Surface agent questions and let operators respond. | `/api/escalations`, `/api/escalations/{id}/respond`, `/escalations` route. | Escalation semantics vary by backend. | Keep escalation event schema consistent across adaptors. |
| Capabilities and adaptor health | Shipped | Show what the selected backend supports and shape UI accordingly. | `/api/capabilities`, `/api/adaptors`, `/api/adaptors/stream`, `/capabilities`. | Some unsupported operations still need clearer UI treatment. | Audit all visible controls against capability manifests. |
| Agent and group registry | Shipped | List agents/personas/groups available to orchestration. | `/api/agents`, `/api/agent-groups`, SPA agent routes. | Mock adaptor returns empty agent/group lists. | Define expected mock/default fixtures for acceptance. |
| Sprints | Shipped | Provide planning grouping and release/sprint context. | `/api/sprints`, `/sprints`, `/sprints/:id`. | Cost/budget behavior is not complete. | Keep budget language separate from sprint display until cost controls ship. |
| Project config | Shipped | Let operators inspect or change project-level configuration. | `/project/config`, settings routes, config API. | Config model spans product, adaptor, and orchestration concerns. | Keep settings grouped by operator task, not backend implementation. |
| Bootstrap / import | Shipped | Start or adopt projects, create/import work, and commit bootstrap plans. | `/api/bootstrap/start`, `/progress`, `/plan`, `/commit`, `/bootstrap/:step`, `/new`, `/onboard`. | Old import/new/onboard vocabulary is still being consolidated. | Mark `/new` and `/bootstrap` roles explicitly in docs. |
| Native orchestration adaptor | Shipped | Run local shell-backed sessions without Gas Town. | Bead `gm-native` closed, native adaptor package, session routes. | Some lifecycle/workspace operations remain unsupported in specific modes. | Continue capability-gated UX rather than assuming full contract support. |
| Per-agent parallelism | Shipped | Limit and communicate agent concurrency by configured capacity. | Bead `gm-root.16` closed, config/UI evidence. | Needs continued acceptance coverage when new backends are added. | Add matrix entry to backend conformance checks. |
| Pool config editor | Shipped | Configure pool scope, agents, polecats, and scheduler behavior. | Bead `gm-s47n.15` closed, `/api/pool-config`, `/api/personas`, `/api/orchestration/*`, `/settings/pools`. | Policy language is still dense and backend-sensitive. | Keep UI labels adaptor-neutral and expose validation clearly. |
| Personas | Shipped | Treat personas as configurable assistants with roles and skills. | Bead `gm-57b` closed, `/api/personas`, persona docs, consult APIs. | PM skill catalog remains broader than current product. | Separate persona registry from skill catalog maturity in docs. |
| Consults | Shipped | Ask personas for advice and apply selected suggestions with audit trail. | `/api/consults`, create/apply routes, coach/persona surfaces. | Advice quality and coverage depend on open PM skill work. | Add acceptance scenarios for consult apply behavior. |
| Health page | Shipped | Show local runtime health. | `/api/health`, `/health` SPA route. | QA health is distinct and not shipped. | Avoid conflating runtime health with QA gates. |
| Graph / dependency view | Beta | Show dependencies, relationships, and work graph context. | `/graph` route, dependency data in Beads, RHP Graph help, milestone/epic/search/state/kind filters, Items/Epics/Auto density controls, cycle and critical-path highlighting. | Scope of graph versus insight surfaces is still evolving. | Document which graph relationships are authoritative. |
| Insights | Beta | Provide higher-level project/persona/work insights. | `/insights`, `/insights/personas` routes. | Some docs mention Prometheus or richer observability that is not fully present. | Mark insight widgets by source and confidence. |
| Drift surface | Beta | Show mismatch between desired/current state or adaptor/project reality. | `/drift` route and API surface. | Some drift/desired-state routes are placeholders. | Define minimum drift checks that are actually enforced. |
| Autodispatch | Beta | Automatically select ready work and dispatch sessions using planning signals. | Bead `gm-s47n` closed, planner/autodispatch packages. | Operator-facing controls and acceptance criteria still need clarity. | Document manual, suggested, and automatic dispatch modes separately. |
| Sticky session pools | Beta | Reuse warm sessions by `(rig, persona)` or equivalent scope while preserving cleanliness. | `docs/design/session-pool.md`, Bead `gm-s47n.13` open/ratified, pool APIs. | Bead is open even though design says ratified; lifecycle details still need acceptance. | Convert remaining ratified decisions into precise shipped/open checks. |
| Container orchestration | Beta | Run sessions in containers with secure defaults and backend parity. | Bead `gm-root.15` in progress, container/Docker code paths. | MCP compatibility, orphan reaper, preamble injection, bridge, and SPA badges remain open. | Label container mode beta until open children close. |
| Mock orchestration adaptor | Beta | Provide dry-run/acceptance-safe orchestration without external backends. | `docs/design/mock-orchestration.md`, `internal/adapter/mock`, temperature-spa default native wrapper. | Acceptance remains active, and template coverage is intentionally narrow. | Keep extending templates only when they support real acceptance scenarios. |
| Acceptance temperature SPA tests | Beta | Validate native, Gas Town, and mock flows through strict product oracles. | `docs/design/acceptance-temperature-spa.md`, Bead `gm-root.27` in progress. | Acceptance is intentionally still active, not complete. | Keep failing or missing scenarios as first-class Beads. |
| Gemba Walk lifecycle | Beta | Guide multi-topic operator walks with agenda, turns, decisions, pause/resume, and end. | `/api/v1/walks:*`, `/walk`, `/walks/:id`, `docs/design/gemba-walk.md`. | Agenda sources, persona replies, consult integration, and summary artifacts are incomplete. | Prioritize agenda source aggregation and consult integration. |
| Workflow API | API-only / Internal | Manage workflow formulas and active runs. | `/api/workflows/formulas`, `/api/workflows/runs`, run creation/burn routes. | Product UI and policy layers are not complete. | Keep API available but document as beta/internal until UI arrives. |
| Workflow UI and gates | Planned | Provide Settings workflow management, RHP bead workflow state, auto-attach, and epic gates. | `docs/design/workflows.md`, Bead `gm-e12.22` children open. | Settings pane, RHP workflow section, policy/session attach, and gates remain open. | Complete or explicitly downscope `gm-e12.22`. |
| CodeAnalysisProvider core | API-only / Internal | Provide source/code context via GitNexus-backed provider. | `docs/design/code-analysis.md`, `internal/core/codeanalysis`, GitNexus analysis current. | Public design routes are missing from router. | Decide whether code analysis is public API or internal prompt context. |
| Code-analysis public API | Needs reconciliation | Expose `/api/v1/code-analysis/*` for capabilities, query, reindex, coverage, and context. | Design doc specifies routes; GitNexus index exists. | Router does not mount documented route family. | Mount routes or update design doc. |
| Source-analysis visibility | Beta | Show whether the scope's source-analysis graph is current enough to trust. | Operational-context `scope_status`, GitNexus `meta.json` comparison, agent context card pills. | Re-index triggering remains separate from the visible freshness check. | Add explicit reindex action/scheduling once the public code-analysis API is settled. |
| Project Manager skill catalog | Planned | Offer PM skills for what remains, epic ordering, escalation triage, priority, dependency hygiene, velocity, decomposition, and scope trimming. | Bead `gm-858` open, persona docs. | Skill catalog is broader than shipped consult behavior. | Ship skills incrementally with fixtures and budget envelope. |
| QA health and gates | Planned | Provide QA persona/gates and explicit readiness checks. | `docs/ui-spec.md`, Bead `gm-v6f` open. | `/qa/health` and `/qa/gates` routes are not present. | Keep QA gates roadmap-labeled until routes and checks exist. |
| Checkpoints | Planned | Persist checkpoints and recovery/decision points across work. | Bead `gm-tfy`, UI spec references. | No full product surface visible. | Define checkpoint MVP and relationship to walks/sessions. |
| Packs / role packs | Planned | Configure project/org context, roles, packs, and reusable operating modes. | Bead `gm-kfq`, UI spec references. | Some API stubs exist, but no full product surface. | Keep packs as future unless needed for near-term onboarding. |
| Subscriptions / channels | Planned | Send email or channel updates for project events. | Bead `gm-hvs` open. | No product implementation found. | Treat as roadmap, not core acceptance scope. |
| Token and cost management | Planned | Track and enforce token/cost budgets by sprint/epic/session and inform routing. | Bead `gm-root.14` deferred. | Deferred; no enforceable product controls. | Keep all budget enforcement language future-tense. |
| Desired-state / city / rigs nouns | Needs reconciliation | Represent higher-level system topology and desired state. | API routes exist as stubs/placeholders. | Product meaning is not yet implemented enough for user-facing confidence. | Hide or clearly mark as experimental until backed by real behavior. |

## Recommended Navigation Status

| Route / surface | Status | Notes |
| --- | --- | --- |
| `/board` | Shipped | Primary status board. |
| `/refine` | Shipped | Backlog/refinement surface. |
| `/backlog` | Shipped | Redirect/alias to refine. |
| `/grid` | Shipped | Redirect/alias to refine. |
| `/sessions` | Shipped | Session supervision. |
| `/escalations` | Shipped | Human-in-the-loop triage. |
| `/settings` | Shipped | General settings. |
| `/settings/pools` | Shipped | Pool configuration. |
| `/capabilities` | Shipped | Adaptor/capability visibility. |
| `/project/config` | Shipped | Project configuration. |
| `/bootstrap/:step` | Shipped | Bootstrap/import flow. |
| `/new` | Shipped | New project entry, but naming is being consolidated. |
| `/onboard` | Shipped | Onboarding entry. |
| `/health` | Shipped | Runtime health, not QA gates. |
| `/graph` | Beta | Dependency/relationship visualization. |
| `/insights` | Beta | Insight surface. |
| `/insights/personas` | Beta | Persona insight surface. |
| `/drift` | Beta | Some backing routes are still placeholder-like. |
| `/coach` | Beta | PM/coach behavior depends on open skill catalog work. |
| `/walk` | Beta | Walk lifecycle exists, behavior is incomplete. |
| `/walks/:id` | Beta | Walk detail exists. |
| `/board?view=recent` | Shipped | Recent Board named view; `/recent` remains a legacy deep link/bookmark path. |
| `/settings/workflows` | Planned | Referenced by workflow roadmap, not present in current SPA route table. |
| `/qa/health` | Planned | Referenced by UI spec, not present. |
| `/qa/gates` | Planned | Referenced by UI spec, not present. |
| `/checkpoints` | Planned | Referenced by UI spec/design direction, not present. |
| `/packs` | Planned | Concept exists, full route/product surface not present. |

## Immediate Reconciliation Queue

1. Reconcile `gm-root.28` with the current mock adaptor implementation.
2. Decide whether the documented code-analysis API should be mounted now.
3. Mark workflow capability as API-only until Settings, RHP workflow state, attach policy,
   and gates ship.
4. Update `docs/ui-spec.md` to classify absent routes as planned instead of
   implicitly shipped.
5. Split Gemba Walk documentation into current lifecycle behavior and future
   decisioning behavior.
6. Hide, remove, or explicitly label placeholder API nouns such as city, rigs,
   packs, and desired state.

## Maintenance Rule

Every new product-facing Bead should update this matrix when it changes one of
these dimensions:

- A planned capability becomes beta.
- A beta capability becomes shipped.
- A route is added, removed, renamed, or hidden.
- An API-only capability gains a visible product surface.
- A documented feature is deferred or descoped.
- A Beads epic closes while code or docs still imply unfinished work.
