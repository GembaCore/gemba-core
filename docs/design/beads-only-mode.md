---
title: "Beads-only operating mode"
decision: gm-etq2
d: D22
---

# Beads-only operating mode

> Status: **implemented baseline** - 2026-05-03
> Owner: gemba mayor
> Scope: let Gemba run as a lightweight Beads viewer and manager
> without requiring a project, worktree orchestration, or agent
> runtime.

## 1. Decision

Add a first-class **Beads-only** operating mode. The baseline shipped
in `gm-4u4l`: boot-time mode selection, capability/status projection,
UI gating, JSONL history, RHP history, numbering/tag authoring support,
and fake-backed E2E coverage.

In this mode Gemba can boot with `--beads-only` and a Beads source, or
enter the mode from the help/coaching panel by selecting a Beads URL or
local worktree. No project metadata, GitHub repository, native agent
setup, or Gas Town orchestration setup is required.

Beads-only mode preserves all Beads management surfaces:

- Board status columns backed directly by Beads state
- Refine table, hierarchy, and swimlane planning views
- shared sort order by modified, created, edited, or ID
- backlog and refinement
- milestone, epic, bead, and decision detail tabs
- create, edit, and hard-delete flows
- dependency graph
- Beads health, remote check/setup affordances, and history

It hides functionality that depends on live work execution:

- sessions
- dispatch
- agent status and session supervision
- Gemba Walk
- review and triage
- orchestration setup
- project-required onboarding steps

When a user performs an action that would normally trigger orchestration
or represent project-state intent, Gemba appends an informational JSONL
entry to a **session manifest**. The manifest is not used for dispatch
in this decision. It is displayed to the operator in plain English in a
new **Beads history** tab in the RHP.

The future goal is a Docker image that can run Gemba in this mode solely
for Beads viewing and management.

## 2. Why This Exists

Full Gemba assumes three things:

1. a project or scope,
2. a Beads work plane,
3. an orchestration plane that can start and supervise work.

That is right for autonomous development, but too heavy for users who
only want to inspect, edit, organize, or share Beads. A Beads-only mode
creates a lower-friction adoption path:

- teams can review and manage Beads without adopting agents;
- a planning database can be hosted by URL and browsed in a container;
- product managers can shape milestones, epics, and work items before
  any code workspace exists;
- Beads data remains portable because every user action can be captured
  as an append-only manifest.

This mode should feel intentionally smaller, not degraded. The status
panel should say clearly that Gemba is operating in Beads-only mode and
that execution features are unavailable by design.

## 3. Entry Points

### CLI boot

Proposed CLI shape:

```bash
gemba serve --beads-only --beads-url dolt://example/gemba
gemba serve --beads-only --beads-dir /workspace/.beads
gemba serve --beads-only --worktree /workspace/project
```

Rules:

- `--beads-only` disables project and orchestration requirements.
- Exactly one Beads source should be selected by boot completion.
- If no source is provided, the SPA opens in a source-selection state.
- A local worktree is accepted only as a way to discover or initialize
  the Beads database; it does not imply native project onboarding.

Implemented boot-time controls:

```bash
gemba serve --beads-only --beads-url mysql://root@127.0.0.1:3307/gemba
gemba serve --beads-only --beads-dir /workspace/project
gemba serve --beads-only --beads-history /data/session-manifest.jsonl
gemba serve --beads-read-only --beads-url mysql://reader@127.0.0.1:3307/gemba
gemba serve --beads-read-only --beads-dir /workspace/project --restart
```

Container-style environment variables are also honored:

```bash
GEMBA_MODE=beads_only
GEMBA_BEADS_URL=mysql://root@127.0.0.1:3307/gemba
GEMBA_BEADS_DIR=/workspace/project
GEMBA_BEADS_READ_ONLY=true
GEMBA_BEADS_ONLY_MANIFEST=/data/session-manifest.jsonl
```

When `--beads-only` uses a Dolt URL, the `bd` CLI is not required at
startup. A local `--beads-dir` source still uses the bd adaptor and
therefore still requires `bd`.

### Help/coaching panel source selection

The help/coaching panel should expose a deterministic source selector:

- Beads URL
- local worktree
- local Beads directory, if supported by the active adaptor

This panel does not launch an LLM. It performs deterministic setup:

1. validate the source,
2. connect to or initialize the Beads database if allowed,
3. load Beads capabilities,
4. switch runtime mode to `beads_only`,
5. open the board or backlog with Help as the default RHP tab and
   Status next in the rail for health and runtime checks.

## 4. Runtime Mode Model

Add a runtime mode value:

```ts
type RuntimeMode = "full" | "beads_only";
```

The server should expose this through the existing status/capability
surface so the SPA does not infer mode from missing data.

Suggested status fields:

```json
{
  "mode": "beads_only",
  "source": {
    "kind": "url",
    "display": "dolt://example/gemba",
    "readonly": false
  },
  "project_required": false,
  "orchestration_required": false,
  "manifest_path": ".gemba/session-manifest.jsonl",
  "beads_read_only": false
}
```

The Status panel should include a prominent pill:

```text
Beads-only
```

When `--beads-read-only` is set, the pill changes to:

```text
Beads-read-only
```

It should also show source health, read/write capability, last sync, and
manifest write status. A Dolt URL source is not intrinsically read-only:
it is writable when the configured Dolt user can write. Read-only is an
explicit Gemba runtime posture (`--beads-read-only`) or a lower-layer
credential/server policy.

## 5. Capability Gating

Beads-only mode is a capability profile. The UI should hide unavailable
features rather than render dead controls.

| Surface or action | Beads-only behavior |
|---|---|
| Board | Visible; defaults to Ready / In Progress / Done status columns from Beads state |
| Refine table | Visible; dense inventory and Deferred grooming surface |
| Refine hierarchy | Visible; hierarchy-first milestone -> epic -> bead view |
| Refine swimlanes | Visible; grouped planning view over the same Beads records |
| Sort order | Visible; modified, created, edited, and ID |
| Backlog/refinement | Visible |
| Milestone detail | Visible |
| Epic detail | Visible |
| Work-item detail | Visible |
| Decision detail | Visible |
| Graph view | Visible |
| Create/edit milestone | Visible |
| Create/edit epic | Visible |
| Create/edit bead | Visible |
| Create/edit decision | Visible |
| Delete bead | Visible when the source is writable; hard-deletes through the Beads public API/CLI |
| Card drag/state change | Visible; emits manifest event; does not dispatch |
| Status / Beads health | Visible; shows current database, source status, remote check/setup actions, and manifest health |
| Sessions | Hidden |
| Agent status | Hidden |
| Dispatch controls | Hidden |
| Gemba Walk | Hidden |
| Review/triage | Hidden |
| Escalation inbox | Hidden |
| Orchestration setup | Hidden |
| Project onboarding | Hidden except source selection |

Deep links to hidden routes should redirect to Status or render a clear
"unavailable in Beads-only mode" page with a path back to Beads views.

### Board and Refine presentation

Beads-only mode uses the same default Board as full Gemba: Ready, In
Progress, and Done columns derived directly from Beads state. This keeps
state terminology and drag behavior consistent across modes. Dragging a
card patches Beads state when the source is writable, but it does not
dispatch an agent or imply that an orchestration session is working the
item.

Milestone and epic beads are wrappers: they collect related child beads
so users can read the project as broad goals, coherent feature areas,
and concrete work. Milestones are best for releases or phases; epics
are best for feature areas or larger threads of work. Individual beads
remain the concrete units to inspect, edit, and later dispatch from
full Gemba.

Refine hosts the alternate planning views. The **Hierarchy** view is a
hierarchy-first reading surface that renders:

```text
Milestone
  Epic
    Bead
```

The **Table** view gives users a dense ordered inventory and the primary
Deferred grooming workspace. The **Swimlanes** view keeps grouped
planning available without making it the Board's default mode.

Board, Refine table, Refine hierarchy, and Refine swimlanes share the
order selector:

- Modified
- Created
- Edited
- ID

The current WorkItem model exposes `created_at` and `updated_at`;
`Edited` is therefore an alias of the latest persisted update until the
Beads source exposes a distinct edit timestamp. In full mode the same
order selector is available on the regular Board because ordering by
created, modified, edited, or ID is useful outside Beads-only mode too.

### Beads health and remote setup

The Status RHP tab exposes Beads health as the primary operating signal
in this mode:

- current database/source display;
- read/write status;
- remote configured/reachable status when the source can report it;
- retry health-check action;
- source-specific repair/setup action for Dolt URL or local Beads
  remotes when available;
- manifest write status.

The top status pills collapse to the mode signal: **Beads-only** is the
active WorkPlane pill in writable mode, while **Beads-read-only** is the
active pill when explicit read-only mode is enabled. Orchestration is
shown as not applicable instead of degraded.

## 6. Session Manifest

The session manifest is an append-only JSONL file. It records what the
operator did in Beads-only mode. It is informational in this decision.

Default location:

```text
.gemba/session-manifest.jsonl
```

Container mode may override this with:

```text
GEMBA_BEADS_ONLY_MANIFEST=/data/session-manifest.jsonl
```

### Event schema

Each line should be self-contained:

```json
{
  "event_id": "evt_01hv...",
  "occurred_at": "2026-05-03T11:00:00Z",
  "actor": "mike",
  "mode": "beads_only",
  "source": {
    "kind": "url",
    "display": "dolt://example/gemba"
  },
  "action": "work_item.state_changed",
  "entity": {
    "type": "bead",
    "id": "gm-4u4l.3",
    "title": "BOM-3: Session manifest JSONL ledger"
  },
  "before": {
    "state_category": "staged"
  },
  "after": {
    "state_category": "started"
  },
  "summary": "Moved BOM-3 from Staged to In Progress."
}
```

### Required event types

| Event | Trigger |
|---|---|
| `work_item.state_changed` | Dragging a bead, epic, or milestone between board states |
| `work_item.created` | Creating a bead/work item |
| `epic.created` | Creating an epic |
| `milestone.created` | Creating a milestone |
| `decision.created` | Creating a decision |
| `work_item.edited` | Editing title, description, labels, tags, status, or metadata |
| `work_item.deleted` | Hard-deleting a bead through Gemba |
| `dependency.changed` | Adding or removing graph/dependency edges |
| `source.connected` | Selecting or changing the Beads source |

The manifest writer should be best-effort but visible. If appending
fails, the Status panel and Beads history tab should show the failure.
Beads writes should not silently pretend a manifest event was recorded.

## 7. RHP Beads History

Add a **Beads history** tab to the RHP in Beads-only mode.

The tab reads the JSONL manifest and renders entries as plain English:

```text
11:00 AM - You moved "BOM-3: Session manifest JSONL ledger" from
Staged to In Progress.
```

Behavior:

- empty state explains that Beads history begins after the current
  Beads-only session starts;
- entries append live when possible;
- reload reparses the manifest;
- malformed lines are shown as recoverable warnings;
- clicking an entry opens the related milestone, epic, bead, or decision
  detail tab.

The tab is not a session log. It is a Beads action ledger.

## 8. Authoring and Numbering

Beads-only creation and editing should preserve the structured numbering
style users expect from Gas Town-derived plans.

Rules:

- Milestone creation suggests the next `M#:` title prefix when the
  existing Beads set follows that convention.
- Epic creation under a milestone suggests a scoped prefix when the
  surrounding plan uses one.
- Work-item creation under an epic suggests the next child number or
  bead-style suffix supported by the Beads database.
- Existing prefixes are preserved on edit unless the user explicitly
  changes them.
- If the database does not follow a recognizable convention, Gemba does
  not invent one silently; it offers a suggested prefix that the user can
  accept or clear.

Create/edit forms should expose decision and milestone tags as selectable
pills:

- milestone tags come from existing milestone ids/titles and labels;
- decision tags come from decision beads and `d:#` labels;
- selected pills persist into the same label/metadata structure used by
  full Gemba mode;
- pills should be searchable once the list grows.

## 9. Board State Hooks

Dragging a card in Beads-only mode still changes Beads state when the
active work plane allows it. It must not start sessions. In
Beads-read-only mode, the same attempted drag is rejected with
`read_only`, the card remains in its original state, and no manifest
entry is appended.

Flow:

1. User drags a card from Staged to In Progress.
2. SPA sends the normal Beads state update.
3. Server updates the Beads work item.
4. Server appends `work_item.state_changed` to the manifest.
5. SPA updates the card and Beads history tab.

If the Beads update succeeds but manifest append fails, the state change
stands and the manifest error is surfaced. If the Beads update fails,
no manifest event is appended.

## 10. Docker Readiness

This mode now has a self-contained Docker quickstart image, a standard
Docker server image, and the minimal production `ko` image.

Quickstart model:

```bash
docker compose -f docker-compose.quickstart.yml up --build
```

The quickstart image:

- includes the `gemba` binary and `bd` CLI;
- seeds `examples/my-project/seed.json` into `/data/example-project`
  when neither `GEMBA_BEADS_DIR` nor `GEMBA_BEADS_URL` is supplied;
- persists the Beads database, auth token, and history manifest in
  `/data`;
- starts `gemba serve --beads-only` against that source;
- accepts `GEMBA_BEADS_READ_ONLY=true` to start the same container in
  Beads-read-only mode;
- accepts `GEMBA_BEADS_DIR=/work` for a mounted local Beads worktree or
  `GEMBA_BEADS_URL=mysql://...` for direct Dolt URL mode.

Standard server model:

```bash
docker compose up --build
```

The standard `Dockerfile` image:

- includes the `gemba` binary, sentinel CLIs, `bd`, git, ssh, bash, and
  `tini`;
- does not seed demo data;
- defaults to
  `gemba serve --listen 0.0.0.0:7666 --auth token --orchestration none`;
- accepts `GEMBA_BEADS_DIR=/work` for mounted local Beads worktrees and
  `GEMBA_BEADS_URL=mysql://...` for direct Dolt URL mode;
- accepts `GEMBA_BEADS_ONLY=true` and `GEMBA_BEADS_READ_ONLY=true` for
  the Beads-only and Beads-read-only variants;
- can pass through workspace/orchestration configuration via
  `GEMBA_CITY`, `GEMBA_TOWN`, `GEMBA_ORCHESTRATION`, and related
  environment variables.

The minimal server image remains the `ko`/distroless image documented in
`docs/install.md`; it does not include `bd` and is intended for explicit
operator-configured deployments with the smallest possible runtime
surface.

## 11. API and Implementation Notes

Implemented points:

- CLI/server config parsing for `--beads-only`, `--beads-url`,
  `--beads-dir`, `--beads-history`, `--beads-read-only`, and matching
  environment variables.
- Dolt URL mode is explicitly writable by default; the direct SQL
  adaptor creates, updates, deletes, labels, parents, and state changes
  through transactions unless `--beads-read-only` is active.
- Beads-read-only mode implies Beads-only, hides write controls through
  the manifest, and hard-blocks mutations before adaptor dispatch.
  Reads continue through the normal source-specific read path; optional
  `--restart` asks local bd Dolt to enforce readonly beneath Gemba.
- Runtime status and capability endpoints expose mode, source, and
  manifest path.
- Orchestration is not bound in Beads-only mode, and session/cascade
  endpoints reject dispatch attempts.
- Server-side manifest writer records create/edit/state-change events
  from Beads write paths and delete events from the destructive
  work-item route.
- SPA navigation hides session/review/triage surfaces and renders a
  clear unavailable page for deep links.
- RHP registers the Beads history tab in Beads-only mode.
- Create flow suggests Gas Town-style prefixes and exposes milestone /
  decision tag pills.
- Work-item detail and power-list bulk actions can hard-delete beads
  when the active source is writable.
- Board defaults to status columns in Beads-only mode; Refine table,
  hierarchy, swimlanes, and Graph remain available for inventory,
  planning, and dependency inspection.

The manifest writer should live server-side so browser refreshes, REST
clients, and future container use all share the same ledger.

## 12. Test Plan

Unit/API:

- parse `--beads-only` with URL, Beads dir, and history variants;
- parse `--beads-read-only`, ensure it implies Beads-only, and expose
  `beads_read_only` through capabilities;
- resolve container-style environment variables;
- skip the bd CLI probe for Beads-only Dolt URL mode;
- expose `mode=beads_only` in config/capabilities;
- append manifest events for create, edit, delete, and state change
  actions;
- do not append manifest entries on failed Beads writes.
- verify Dolt URL create/update/delete works when writable and returns
  `read_only` only when explicitly configured read-only.

SPA:

- Status panel shows Beads-only or Beads-read-only pill and source
  details;
- Beads surfaces are visible;
- orchestration/session/review surfaces are hidden;
- Beads history tab renders empty, populated, malformed, and append
  states;
- create/edit forms show numbering suggestions and decision/milestone
  tag pills.

E2E:

- boot Beads-only from CLI;
- create a milestone, epic, bead, and decision;
- edit and delete a bead;
- drag a staged bead to in progress and confirm no session dispatch;
- inspect the Beads history tab and graph view.

Current automated E2E coverage uses the fake backend to verify
mode-gated navigation, drag-to-state-change without dispatch, history
ledger rendering, and deep-link unavailability for sessions.

## 13. Work Breakdown

Decision bead: `gm-etq2`

Implementation epic: `gm-4u4l`

| Bead | Scope |
|---|---|
| `gm-4u4l.1` | Boot and source selection |
| `gm-4u4l.2` | Capability gating and hidden surfaces |
| `gm-4u4l.3` | Session manifest JSONL ledger |
| `gm-4u4l.4` | RHP Beads history tab |
| `gm-4u4l.5` | Beads authoring preserves numbering and tag pills |
| `gm-4u4l.6` | Board, refinement, detail, and graph parity |
| `gm-4u4l.7` | Docker-oriented Beads-only packaging and quickstart image |
| `gm-4u4l.8` | Automated coverage for Beads-only mode |

## 14. Open Questions

1. Should manifest files be scoped per browser session, per Beads
   source, or per server process?
2. Should Beads history include read/navigation events, or stay limited
   to mutating actions?
3. Should source selection be implemented as a deterministic Help/RHP
   panel flow, or should the existing Bootstrap source selector become
   the canonical Beads-only source selector?
4. Should dependency graph edits get first-class manifest events once
   the SPA exposes relationship editing outside work-item PATCH?
