# Prior Art: Foolery

> Spike output for **gm-mth** (2026-04-20). Evaluates Foolery as a host or consumer
> of Gemba's UI layer under Path B / Path D of the UI-strategy decision (gm-9h6).
>
> Code examined: local clone at `/Users/mikebengtson/gt/foolery/mayor/rig`,
> upstream `github.com/acartine/foolery`, version 0.10.0 (2026-04-20).

## 1. License & source access

- **License:** MIT (`LICENSE`, copyright 2026 Andrew Cartine).
- **Upstream:** https://github.com/acartine/foolery — public, single repo.
- **Verdict:** No license blockers. Fork, embed, contribute, and redistribute are all permitted.

## 2. Architecture

- Next.js 16 App Router + React 19 + TypeScript + Tailwind 4. State via Zustand.
  Server state via TanStack Query. Terminal via xterm.js.
- Runtime model: Node.js server (via `src/instrumentation.ts`) alongside the Next
  client. The server hosts API routes under `src/app/api/*`.
- Delivered as a `foolery` launcher CLI (`foolery start` / `stop` / `doctor` /
  `setup`) plus a downloaded runtime artefact. Not Electron, not VS Code.
  "Local server + browser" is the shipping model.
- Backend communication is **direct CLI shell-out** via `Bun.spawn()` /
  `execFile()` against `kno` (Knots) or `bd` (Beads). No HTTP or daemon protocol
  with the backend.

## 3. Extensibility — `BackendPort`

This is the load-bearing finding.

- `src/lib/backend-port.ts` defines a typed interface every backend implements:
  `list / listReady / search / query / get / listWorkflows / create / update /
  delete / close / listDependencies / addDependency / removeDependency /
  buildTakePrompt / buildPollPrompt`, plus a `BackendResult<T>` envelope and a
  structured `BackendError` taxonomy.
- Backends are wired by `src/lib/backend-factory.ts` + `backend-instance.ts`,
  selected by a marker-dir probe (`.knots` → Knots, `.beads` → Beads) and
  overridable via `FOOLERY_BACKEND`.
- `src/lib/backend-capabilities.ts` carries per-backend capability flags
  (`canDelete`, `canSync`, `maxConcurrency`, …) that UI call sites check with
  `hasCapability()` / `assertCapability()` — the same pattern Gemba's
  CapabilityManifest uses.
- **Contract tests** exist at `src/lib/__tests__/backend-contract.test.ts`, and
  there is an authored `docs/backend-extension-guide.md` walking through
  "add a new backend". This is mature.
- Limit: adding a new backend today means a PR (or fork) that edits
  `backend-factory.ts` and `schemas.ts` to extend the `BackendType` union. There
  is no dynamic plugin loader.

## 4. Backend coupling to Beads-specific schema

- The UI does **not** import Beads types directly. Everything flows through
  `Beat`, `BeatDependency`, and `MemoryWorkflowDescriptor` in `src/lib/types.ts`.
- Beads-specific status mapping (`open / in_progress / closed` ↔ canonical
  workflow state) is isolated to `src/lib/backends/beads-compat-status.ts`.
  `docs/adr-knots-compatibility.md` documents the decision to pass workflow-native
  state through unchanged and keep translation at the edge.
- Beat has a superset of fields — `aliases`, `nextActionState`,
  `requiresHumanAction`, `isAgentClaimable`, `invariants`, `metadata` — that map
  cleanly onto Gemba's `WorkItem` plus a few Gemba-native fields that would need
  capability-flagged UI.

## 5. Active development

- Single maintainer (Andrew Cartine). `git log --pretty='%an' | sort -u` returns
  one name.
- Active: latest commit `d8c8270` on 2026-04-20 (today). Conventional commits
  with scope (`fix(agents): …`). Changesets-driven releases. Current version
  0.10.0.
- Discord community (`thecartine`), Substack launch post, Mintlify community-tools
  listing. Public + visible.
- Test coverage: Vitest unit + Storybook + contract tests + smoke tests
  (`test:smoke:beats:latency`, `test:smoke:beats:pulldowns`). Quality bar is
  substantial for a solo project.

## 6. Feature gap vs Phase 12 — see fit-gap matrix

Summary (full matrix in `crew/mike/foolery-fit-gap.md`):

- **Green (already shipped, ~7/17):** App shell, WorkItem grid, WorkItem detail,
  AgentGroup board, creation flow, cmdk command palette, bulk actions.
- **Yellow (partial, ~4/17):** Data layer (TanStack Query yes; SSE-driven
  invalidation for workitems no — SSE exists only for terminal streams, Beat
  reads are polled), Agents dashboard, backlog board, agent detail.
- **Red (not shipped, ~6/17):** Adaptor capability browser, dependency graph,
  drift dashboard, insights/OTEL panel, YOLO mode toggle, AgentGroup mode switch
  (static / pool / graph).

## 7. Outreach feasibility

Reasonable. Andrew is active, has a Discord, authored a backend-extension guide,
and the codebase is structured for third-party backends. The viable shape of an
RFC is "Gemba as a third BackendPort implementation" rather than "host Gemba's
SPA inside Foolery". The latter is a much bigger ask that competes with Foolery's
own UI direction; the former slots into an existing seam.

**Recommended outreach (pending human go-ahead):** GitHub issue on
`acartine/foolery` titled "RFC: Gemba BackendPort — proposal to list Gemba as a
third memory-manager backend", linking this spike and the `foolery-extension-plan.md`.

## Recommendation

**CONDITIONAL viable**, with a clear preference for **Path D**.

- **Path B (Foolery is Gemba's UI):** Viable only if upstream accepts a Gemba
  backend and we add SSE push for beat-state changes (today's SSE is
  terminal-only) plus three red-zone views (capability browser, dependency
  graph, drift). This is a multi-quarter collaboration with a single maintainer.
  Risk: our roadmap becomes coupled to Andrew's bandwidth.
- **Path D (Foolery as optional consumer; Gemba ships its own thin SPA):** Low
  risk, high optionality. We implement a `gemba-backend.ts` `BackendPort`
  against Gemba's WorkPlaneAdaptor, upstream or fork, and users who want
  Foolery's keyboard UX get it. Gemba still ships `web/` for its own views
  (capability browser, dependency graph, drift). Foolery's strong features
  (Queues, Active, Retakes, History, hotkeys) become a bonus consumer surface,
  not a dependency.

Full architectural integration spec for both paths: see
`crew/mike/foolery-extension-plan.md`.

## Evidence index

| Claim | File |
| ----- | ---- |
| MIT license | `foolery/mayor/rig/LICENSE` |
| Backend contract | `src/lib/backend-port.ts` |
| Backend factory + routing | `src/lib/backend-factory.ts`, `src/lib/backend-instance.ts` |
| Capability flags | `src/lib/backend-capabilities.ts` |
| Beat schema | `src/lib/types.ts` (`Beat`, `BeatDependency`, `MemoryWorkflowDescriptor`) |
| Beads status isolation | `src/lib/backends/beads-compat-status.ts` |
| Contract tests | `src/lib/__tests__/backend-contract.test.ts` |
| Extension guide | `docs/backend-extension-guide.md` |
| Agent dialect switch | `src/lib/agent-adapter.ts` (`resolveDialect`, arg builders, normalizers) |
| Terminal SSE (scope limit) | `src/app/api/terminal/[sessionId]/route.ts` |
| Activity signal | `git log --oneline` — latest `d8c8270` 2026-04-20 |
