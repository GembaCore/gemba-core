# Product, Documentation, and Code Drift Report

Date: 2026-04-30

This report treats the Markdown files in this repository as product and design
documentation, then compares their intended product behavior with the current
codebase and the live Beads project database. It uses the local Dolt-backed
Beads server as the decision/status source of truth.

## Evidence Used

- Repository: current worktree at latest `origin/main`, commit `b6beaed`.
- GitNexus: local analysis repo `mike4`, indexed at commit `b6beaed`.
  - 1195 files
  - 16045 symbols
  - 52890 edges
  - 806 clusters
  - 300 flows
- Beads: local Dolt server.
  - Host: `127.0.0.1`
  - Port: `3307`
  - Database: `gemba`
  - Project id: `59a81ba3-8b76-4956-9fd6-1675a82d5bfe`
  - Query mode: `dolt --host 127.0.0.1 --port 3307 --no-tls --use-db gemba sql`
- Markdown sources:
  - `README.md`
  - `docs/ui-spec.md`
  - `docs/design/*.md`
  - `docs/adaptors/*.md`
  - `docs/personas/*.md`
  - `docs/concepts/*.md`
  - testing and operational READMEs

## Executive Summary

The product documentation describes Gemba as a local-first orchestration
sidecar: a single Go binary with an embedded React SPA that pairs one WorkPlane
adaptor with one OrchestrationPlane adaptor. The product intent is to let an
operator plan work, inspect and refine Beads, dispatch AI agent sessions,
supervise escalations, run Gemba walks, consult personas, and progressively
connect richer orchestration backends without leaking backend-specific concepts
into the main UX.

The current codebase broadly matches that architecture. The WorkPlane,
OrchestrationPlane, native adaptor, mock adaptor, GitNexus code-analysis core,
planning/dispatch packages, persona/consult surfaces, Gemba walk routes,
workflow API surface, bootstrap/onboarding, session pools, and SPA routes are
all visibly present.

The most important drift is not architectural misalignment. It is maturity drift:
the documentation describes full product experiences, while the implementation
often has the API/core in place but not the full UI, policy engine, generator, or
end-to-end acceptance coverage. Beads confirms this pattern: several rich epics
are ratified or partly shipped, while child beads for UI completion, gates,
container parity, workflow attach behavior, code-analysis API exposure, QA, and
token cost management remain open or deferred.

## Intended Product Functions

### 1. Local Sidecar Product Shell

Gemba is intended to run as a local sidecar binary with an embedded React UI,
OpenAPI surface, server-side mutation controls, optional TLS/auth, and adaptor
capability discovery. The user should interact with one coherent product even
when the underlying WorkPlane or OrchestrationPlane backend changes.

Primary documented sources:

- `README.md`
- `docs/ui-spec.md`
- `docs/adaptors/workplane.md`
- `docs/adaptors/orchestration.md`
- Bead `gm-root`

Code alignment:

- Go server/router, embedded SPA, OpenAPI endpoint, capabilities endpoints, auth
  and TLS-related configuration, adaptor registry, and SPA settings surfaces are
  present.

Assessment:

- Strongly aligned. Some configured surfaces remain stubs or partial, but the
  shell architecture is implemented in the intended shape.

### 2. WorkPlane: Work Item and Planning System

The WorkPlane is intended to read and mutate product work items, dependencies,
labels, milestones, notes, acceptance criteria, evidence, and readiness state.
The UI is expected to support board, backlog/refine, grid/list, recent work,
RHP detail tabs, dependency views, and planning actions.

Primary documented sources:

- `README.md`
- `docs/ui-spec.md`
- `docs/adaptors/workplane.md`
- `docs/design/work-planning.md`
- Beads `gm-root`, `gm-s47n`, `gm-e12`

Code alignment:

- Server routes exist for work items, sprints, repositories, work-item notify,
  and compatibility aliases.
- SPA routes exist for board, refine, sessions, sprints, graph, recent, and
  related detail views.
- Planning packages exist for targets, concepts, conflicts, affinity, scoring,
  selection, claims, retro, runway, source analysis scheduling, and
  autodispatch.

Assessment:

- Strong alignment for the core planning model.
- Drift remains around explicit ready-work API exposure: `/api/work-items/ready`
  is currently a 501 route even though readiness is a central WorkPlane concept.

### 3. OrchestrationPlane: Agent Sessions and Escalations

The OrchestrationPlane is intended to manage agents, groups, sessions,
workspaces, lifecycle transitions, escalations, events, session output, and
backend capability negotiation. Native, Gas Town, and mock adaptors are all part
of the intended product direction.

Primary documented sources:

- `README.md`
- `docs/adaptors/orchestration.md`
- `docs/design/session-pool.md`
- `docs/design/mock-orchestration.md`
- Beads `gm-native`, `gm-e7`, `gm-root.15`, `gm-root.28`

Code alignment:

- Routes exist for agents, agent groups, sessions, session peek, escalations,
  pool config, personas, orchestration state, scopes, polecats, and scheduler
  config.
- Native, Gas Town, Docker/container-related, and mock adaptor packages exist.
- The mock adaptor can execute templated dry-run tasks and close Beads through
  the WorkPlane in some flows.

Assessment:

- Strong architectural alignment.
- Runtime parity is uneven. Native and container backends still expose
  unsupported operations in places, and Beads keeps container parity work open
  for MCP compatibility, orphan reaping, preamble injection, bridge support, and
  SPA backend badges.
- Beads marks mock orchestration as open while code already contains a mock
  adaptor, suggesting status drift or remaining acceptance work rather than a
  missing implementation.

### 4. Planning, Dispatch, and Session Pools

The product intends to select work using a two-axis planning model:
product/release targets and code/source concepts. It should account for
conflicts, affinity, leverage, session health, dirty worktrees, persona fit,
parallelism, pool keys, recycling, and deterministic lifecycle decisions.

Primary documented sources:

- `docs/design/work-planning.md`
- `docs/design/session-pool.md`
- `docs/design/pool-editor.md`
- `docs/concepts/dispatch-vs-planning.md`
- Beads `gm-s47n`, `gm-s47n.13`, `gm-s47n.15`, `gm-root.16`

Code alignment:

- Planner and dispatch packages are present for the documented concepts.
- Pool editor endpoints are present.
- Per-agent parallelism was marked closed in Beads and corresponding config/UI
  concepts are visible in code.

Assessment:

- Strong alignment in backend/core.
- The product surface still appears split between fully shipped controls and
  evolving UX. The docs are slightly ahead of the visible navigation state.

### 5. Personas, Skills, Consults, and Manager Assistance

Gemba is intended to include configurable assistant personas, a Project Manager
skill catalog, consult flows, PM panel guidance, and auditable advice. Personas
should help with decomposition, priority, escalation triage, scope trimming,
velocity, dependency hygiene, and similar operator decisions.

Primary documented sources:

- `README.md`
- `docs/ui-spec.md`
- `docs/personas/*.md`
- Beads `gm-57b`, `gm-858`

Code alignment:

- Routes exist for personas, skills, consults, consult creation, and consult
  apply behavior.
- SPA routes include coach and insights/personas surfaces.

Assessment:

- Good alignment for infrastructure and early UX.
- The PM skill catalog is still open in Beads, so documentation describing the
  full catalog should be read as target product behavior, not fully complete
  shipped behavior.

### 6. Gemba Walks and Decisioning

The product intends to provide guided, auditable Gemba walks: multi-topic
operator conversations that produce decisions, summaries, and follow-up work.
Walks should eventually integrate personas, checkpoints, source context, and
decision artifacts.

Primary documented sources:

- `docs/design/gemba-walk.md`
- `docs/ui-spec.md`
- Beads related to walks, checkpoints, and QA

Code alignment:

- Routes exist for starting, listing, reading, advancing, deciding, pausing,
  resuming, and ending walks.
- The SPA includes `/walk`, `/recent`, and walk detail routes.

Assessment:

- Partially aligned.
- Code comments and behavior show known incompleteness: agenda sources are empty
  until a future aggregator is wired, persona response generation is future
  work, `/consult` is a 501 stub, and summary artifact generation is not the
  default route behavior.

### 7. Workflows

The documentation intends workflows to become first-class Beads molecules:
library formulas, active workflow runs, manual attach, session auto-attach,
policy, and epic gates.

Primary documented sources:

- `docs/design/workflows.md`
- `docs/ui-spec.md`
- Bead `gm-e12.22`

Code alignment:

- Server routes exist for workflow formulas, formula detail, runs, run detail,
  run creation, and burn/progress behavior.

Assessment:

- API surface is present.
- Beads shows the richer product layer still open: Settings -> Workflows pane,
  RHP workflow section, workflow policy/session auto-attach, and epic
  gates are not complete.

### 8. Project Creation, Import, Bootstrap, and Code Analysis

The product intends to support creating/adopting projects, importing existing
work, bootstrapping repositories, analyzing source code, and using GitNexus as
the reference CodeAnalysisProvider.

Primary documented sources:

- `README.md`
- `docs/design/code-analysis.md`
- `docs/design/acceptance-temperature-spa.md`
- Beads around bootstrap, code analysis, and acceptance

Code alignment:

- SPA routes exist for `/new`, `/onboard`, and `/bootstrap/:step`.
- Server routes exist for bootstrap start/progress/plan/commit.
- Code-analysis packages and GitNexus adapter packages exist.
- Local GitNexus analysis is current for this worktree.

Assessment:

- Bootstrap/onboarding is substantially present.
- Code-analysis drift remains: the design specifies `/api/v1/code-analysis/*`
  routes, but those routes are not mounted in the current router. Core/provider
  code exists, but the public product API described in the design is not fully
  exposed.

### 9. Observability, Operations, QA, and Release Safety

The docs intend health, capabilities, drift, insights, QA health/gates,
acceptance tests, Prometheus/metrics-adjacent observability, release support,
and deployment-engineering persona guidance.

Primary documented sources:

- `README.md`
- `docs/ui-spec.md`
- `docs/design/acceptance-temperature-spa.md`
- `docs/personas/deployment-engineer.md`
- Beads `gm-v6f`, `gm-root.27`

Code alignment:

- Health, version, capabilities, adaptors, drift, insights, and acceptance/mock
  related code paths exist.
- SPA routes include `/health`, `/drift`, `/insights`, and related surfaces.

Assessment:

- Operational basics are aligned.
- QA gates/checkpoints are still design-forward. UI routes such as `/qa/health`,
  `/qa/gates`, and `/checkpoints` are documented but not present in the current
  SPA route table.

## Usage Patterns Elicited from the Documentation

1. Project setup: an operator creates, adopts, or imports a project, then lets
   Gemba detect capabilities, Beads state, source repositories, and orchestration
   options.

2. Planning and refinement: the operator reviews the board/refine surfaces,
   cleans up work items, checks dependencies and acceptance criteria, and uses
   PM/persona assistance to decide what should happen next.

3. Dispatch: the operator or autodispatch mechanism chooses ready work based on
   product targets, source concepts, conflicts, persona fit, workspace health,
   and pool capacity.

4. Supervision: the operator monitors active sessions, inspects peeks/logs,
   resolves escalations, and recycles or ends sessions according to pool policy.

5. Retrospective and evidence: the system records session outcomes, bead-done
   events, notes, decisions, walk output, and follow-up issues.

6. Product navigation: the operator moves between Plan (including Recent
   Board views), Refine, Review, Triage, Sessions, Settings, Insights,
   Capabilities, Walks, and project config without needing to understand
   backend-specific vocabulary.

7. Advanced governance: the operator configures pools, workflows, personas,
   packs, channels, cost budgets, checkpoints, and QA gates as these areas
   mature.

## Key Drift and Gaps

### High Priority

1. Code-analysis API is behind the design.

The design describes `/api/v1/code-analysis/*` endpoints and GitNexus as the
reference provider. The code contains GitNexus and code-analysis packages, and
the local GitNexus index is current, but the documented API routes are not
mounted in the router.

Risk: planning and prompt-context features may rely on code analysis internally,
but the product cannot yet expose or debug the capability as documented.

Recommended action: either mount the public code-analysis API or revise the
design to describe the current internal-provider-only state.

2. Workflow product UX is still incomplete.

The workflow API surface exists, but Beads keeps the Settings pane, RHP bead
workflow section, session auto-attach policy, and epic gates open.

Risk: docs imply first-class workflow governance, while users may only find API
level behavior.

Recommended action: finish `gm-e12.22` children or label workflows as API/beta
in product docs.

3. Gemba Walks are route-complete but behavior-light.

Walk lifecycle routes and UI entry points exist, but agenda sourcing, persona
turn generation, consult integration, and default summary artifact generation
are not complete.

Risk: the feature can look shippable from navigation alone but may not yet
deliver the documented decisioning loop.

Recommended action: make walk capability status explicit and prioritize agenda
source aggregation plus consult integration.

4. Navigation and UI spec are ahead of the SPA.

The UI spec names several surfaces that are absent or partial in the current
route table, including QA health, QA gates, checkpoints, Settings -> Workflows,
and some setup/packs concepts. Sidebar comments also refer to a six-pane
ratification while the visible navigation currently exposes a different set.

Risk: product direction is sound, but docs, Beads epics, and UI labels will
confuse acceptance testing unless the intended IA is re-ratified.

Recommended action: create a route/capability matrix and update `docs/ui-spec.md`
to mark each surface as shipped, beta, hidden, or planned.

5. Mock orchestration status appears stale or acceptance-gated.

The mock adaptor exists in code and implements dry-run templates and some bead
closing behavior. Beads still marks `gm-root.28` open and `gm-root.27`
in-progress.

Risk: future work may reimplement or mis-scope mock behavior because Beads does
not reflect what is already merged.

Recommended action: reconcile `gm-root.28` against the actual mock adaptor and
move remaining work into precise bug/acceptance beads.

6. Token/cost management remains deferred.

The product docs and Beads describe cost meters, token budgets, sprint/epic
budgeting, gauges, enforcement, and cost-aware routing. Bead `gm-root.14` is
deferred.

Risk: autonomous dispatch and multi-agent workflows can outgrow the current
operator-control model before spending safeguards arrive.

Recommended action: keep token/cost language clearly future-tense until budget
visibility exists.

### Medium Priority

7. Containerized orchestration is partial.

Beads show containerized orchestration in progress with open children for MCP
compatibility, orphan container reaping, preamble injection, bridge events, and
SPA backend badges.

Risk: container support may work for some direct paths but not yet satisfy the
native-session parity implied by product docs.

Recommended action: present container mode as beta until the open child beads
close.

8. Orchestration capability negotiation needs user-visible clarity.

The adaptor contracts define broad lifecycle/workspace/reservation behavior, but
some concrete adaptors return unsupported for methods such as reservation,
workspace, pause/resume, or peek in specific modes.

Risk: users may see controls that cannot work for the selected backend.

Recommended action: audit UI gating against adaptor capability manifests.

9. Ready-work endpoint is a stub.

`/api/work-items/ready` currently returns 501 even though ready-work selection is
central to dispatch.

Risk: integrations or UI paths that use the public route will fail even if
internal planning code can compute candidates elsewhere.

Recommended action: either implement the endpoint or remove it from public
surface until dispatch owns the full path.

10. Stubbed product nouns remain visible in the API.

Routes for city, rigs, packs, desired state, drift, and related future surfaces
exist, but some are placeholder/stub behavior.

Risk: capability discovery may imply broader backend support than is actually
available.

Recommended action: keep stubs behind explicit capability labels or remove them
from user-facing affordances.

11. Subscriptions, channels, checkpoints, packs, and QA gates are primarily
design-roadmap items.

Beads and docs contain useful product direction for email updates, multi-org
role packs, checkpoints, and QA gates. The current code does not yet provide the
full product experiences.

Risk: roadmap items can blur with committed shipped scope.

Recommended action: split docs into "current product", "ratified next", and
"design backlog" sections.

## Recommended Product Capability Matrix

The next documentation pass should publish a single matrix with these statuses:

- Shipped: local sidecar shell, basic WorkPlane board/refine flows, sessions,
  escalations, capabilities, pool config, native orchestration, consults,
  bootstrap/onboarding, mock adaptor core.
- Beta: autodispatch, session pools/recycle policy, container orchestration,
  Gemba walks, workflow API, code-analysis prompt context.
- API-only or internal: workflow formulas/runs, code-analysis provider,
  planning score components, source-analysis scheduling.
- Planned: Settings -> Workflows, workflow gates, checkpoint UX, QA gates, cost
  management, subscriptions, channel updates, role packs.
- Needs reconciliation: mock orchestration Bead status, SPA navigation IA,
  ready-work endpoint, code-analysis public routes.

## Suggested Next Decisions

1. Decide whether code analysis is a public API feature in the next release or
   an internal prompt-context capability. Update either router or docs
   accordingly.

2. Close the loop on `gm-root.28` by comparing each mock-orchestration child
   acceptance criterion against the current `internal/adapter/mock` code.

3. Finish or explicitly downscope `gm-e12.22` workflows so product docs stop
   implying complete workflow governance.

4. Ratify the SPA information architecture again against the route table and
   `docs/ui-spec.md`.

5. Promote Gemba Walks only after agenda sourcing, persona response generation,
   consult integration, and summary artifact creation are coherent.

6. Keep cost controls and QA gates visibly marked as future or beta until Beads
   and code show enforceable behavior.

## Bottom Line

Gemba's implementation and documentation agree on the product shape: an
adaptor-agnostic, local-first orchestration sidecar for planning, dispatching,
and supervising AI-assisted software work. The main gap is that several product
experiences are documented at their final shape while the code currently exposes
backend primitives, partial UI, or beta-level behavior.

The highest-leverage cleanup is to introduce a living capability matrix and then
reconcile four areas first: code-analysis API exposure, workflow UX completion,
Gemba Walk behavior, and mock/acceptance Beads status. That will make the docs,
Beads project state, and implementation tell the same story before deeper code
analysis work begins.
