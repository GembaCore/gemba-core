# Gemba UI Spec

> **Deliverable 2 for `gm-p27`.** Generated from `ui-spec-template.md` answers + ratified design decisions (gm-2yg Coaches vs Managers, gm-hjx Epic elevation, gm-57b Persona architecture, gm-uf7 Agentic personas, gm-v6f Agentic QA, gm-tfy Checkpoints, gm-iw4 Project entity, gm-e8o Workspace modes, gm-c4l Role Packs, gm-9rv Persona PPPP + Phases, gm-3nk Gemba walks, gm-l1i Agentic code analysis, gm-hvs Subscriptions).
>
> **Authoritative for implementation.** Deviations require a Gemba walk (review of work in progress) with the UX Expert Coach before merging. The "Review" tab in the SPA chrome opens this surface.
>
> Items marked **PROPOSED** are Claude's judgment calls on under-specified questions; mike reviews and ratifies.

---

## 1. Design system

### 1.1 Vibe, density, voice
- **Aesthetic:** Playful / illustrative. Friendly, approachable, not corporate. Think GitHub when it has personality, Basecamp's warmth without the preciousness.
- **Density:** Comfortable. Balanced whitespace; information-rich where work demands it (Board list, Insights), generous where reflection matters (RHP detail tabs, Gemba walk).
- **Voice + tone:** Confident, friendly, precise. Copy is short. Product speaks in plain verbs. No product-marketing hedging.

### 1.2 Palette
- Theme: **system-theme-respecting**; defaults to OS light/dark. User can lock.
- Primary: blues and tans. **PROPOSED** primary accent — a mid-saturation navy (`#3B5B7A`-class for light theme, `#7FA3C3`-class for dark); tan secondary (`#C9A878` warm neutral) for surfaces that need to distinguish from the primary without shouting.
- Reserved semantics (locked):
  - **Red** = blocked / critical / error
  - **Yellow** = warning / stale / degraded
  - **Green** = passed / ready / healthy
  - **Blue** = in-progress / info
  - **Purple** = persona / advisory
- Coach badge uses purple; Manager badge uses the primary navy (authority implies closer to project core).

### 1.3 Typography
- **Body: PROPOSED** Inter (variable). Broadly used in this tool class (Linear, Vercel, Raycast); modern, neutral, designed for screens.
- **Monospace: PROPOSED** JetBrains Mono (variable). Ligature-capable, excellent for code citations and bead IDs (gm-*).
- Scale: 12 / 14 / 16 / 20 / 24 / 32. 14 is body default; 12 for metadata; 16 for headings within surfaces; 20+ for screen titles.

### 1.4 Shape + motion
- **PROPOSED** 8px base radius; cards use 12px; modals use 16px. Inputs and badges use 6px.
- Motion: 150ms default for hover/focus; 250ms for panel transitions; 400ms for full-screen changes. Reduced-motion OS preference honored.

---

## 2. Layout + chrome

### 2.1 Application shell

```
┌──────────────────────────────────────────────────────────────────────┐
│  Top bar                                                              │
├───┬────────────────────────────────────────────────────────────────┬─┤
│   │                                                                │ │
│ S │                                                                │ │
│ i │                       Main content                             │ │
│ d │                                                                │ │
│ e │                                                                │ │
│ b │                                                                │ │
│ a │                                                                │ │
│ r │                                                                │ │
│   │                                                                │ │
├───┴────────────────────────────────────────────────────────────────┴─┤
│  PM panel (bottom drawer — always available, collapsible)            │
└──────────────────────────────────────────────────────────────────────┘
```

- **Sidebar:** collapsible (default collapsed on Board to maximize the Gemba view; expanded elsewhere).
- **Top bar:** always visible.
- **PM panel:** bottom drawer. Default state = open-last-state (restored on reload).
- **Main content:** owns everything between.

### 2.2 Sidebar (primary nav)

Top-to-bottom, in order:

1. **Board** — `/board`; home status surface with Ready / In Progress / Done columns, milestone and scope filters, and card attention pills.
2. **Refine** — `/refine`; backlog grooming, hierarchy, and planning swimlanes.
3. **Review** — `/walk`; Gemba walk surface for work-in-progress review.
4. **Triage** — `/escalations`; blockers, HITL, permission prompts, and degraded-adaptor attention.
5. *(divider)*
6. **Sessions** — `/sessions`; live runtime inventory.
7. **Settings** — `/settings`; global/project configuration, with `/settings/pools` for pool dispatch.

> **Sidebar consolidation rationale (updated 2026-05-05):** Recent and Grid are not top-level concerns. **Recent** survives as a deep link for catch-up workflows. **Grid** folds into Refine's table mode; `/grid` redirects to `/refine`. **Graph** is first-order because dependency and relationship inspection is a core Beads-management surface, especially in Beads-only quickstart. Agents, Agent groups, Capabilities, Insights, Drift, Sprints, and Coach remain routeable secondary surfaces but do not occupy first-order sidebar slots.

Secondary surfaces (`/insights`, `/drift`, `/coach`, `/sprints`, `/agent-groups`, `/capabilities`, `/project/config`, `/qa/health`, `/qa/gates`, `/checkpoints`) are NOT in the sidebar. They're reachable via:
- **Cmd-K** (primary: typing the surface name jumps to it)
- **Deep link** (URL-navigable)
- **Inline buttons** inside related surfaces (e.g., "Insights" button on Board top-right)

Sidebar items show a small badge when they have pending attention (Escalations: count of open; Gemba walks: count of active; Setup: count of degraded adaptors or unsigned packs awaiting confirmation).

### 2.3 Top bar (left to right)

1. Collapsed-sidebar toggle
2. **Workspace switcher** — current workspace name with dropdown; switching preserves the URL path where possible
3. **Mode indicator** — color-coded pill: green = unsupervised, blue = supervised, yellow-outlined = managed
4. **Phase indicator** — color-coded: ideation / design / building / validating / shipping / commercialization (single-state pill; click reveals active purviews)
5. **Budget gauge** — dual display: "Agents 43%" | "Advisors 18%" (advisor spend separate per `gm-e11.2`). Click expands full gauge.
6. *(flexible space)*
7. **Search** — Cmd-K trigger
8. **Gemba walk trigger** — appears only when a walk is active; badge shows agenda-item count
9. **PM panel toggle** — opens/collapses bottom drawer
10. **User menu**

No active-guardrail banner in the top bar. Guardrail violations surface inline on affected Epic cards AND in the Escalations inbox under `purview_violation` kind. This keeps the chrome quiet.

### 2.4 PM panel (bottom drawer)

- **Position:** bottom; overlays content on expand.
- **Default state:** open-last-state (persists across reloads).
- **Keyboard:** Cmd-P / Ctrl-P toggle. Escape collapses without losing conversation.
- **Height:** 320px default expanded; drag-resize to 25-75% of viewport.
- **Content when no active conversation:** quick-action buttons (context-sensitive to current view) + a blank input prompt with placeholder "Ask a Coach or consult a Manager…"
- **Active conversation:** terminal-style chat (monospace, alternating-indent, explicit speaker labels). SuggestedActions group at the bottom of the relevant turn. ExecutedActions rendered with a distinct icon + color badge (green-check + "applied" label).
- **Persona selection:** dropdown above the input; shows currently-active persona; click to switch or open roster.
- **Cost display:** running total in the header ("$0.34 this session · ~$0.003/turn"); per-turn cost on hover of each turn.
- **Gemba walk takeover:** when a walk is active, the PM panel becomes the walk's chat surface. Agenda is in the main content area (see §5.4). Exiting the walk returns the panel to the last non-walk conversation.

### 2.5 Deep-link conventions

- `/board` — Gemba view (home)
- `/board/:epicId` — Epic detail opens in the RHP on top of the board
- `/w/:workspace/*` — prefix for when URL needs to be workspace-unambiguous
- Workspace-in-URL is **optional** (omit uses active workspace)
- Query params persist filter state (e.g., `?phase=building&swimlane=by-parent-epic`)

---

## 3. Vocabulary + copy primitives

### 3.1 Entity nouns (ratified)

| Concept | Term |
|---|---|
| Unit of work generically | **WorkItem** |
| Parent Kanban card | **Epic** |
| Runtime agent | **Worker** |
| Group of workers | **Work Group** |
| Workspace boundary | **Workspace** |
| Sprint analog | **Sprint** |
| Conversational persona | **Coach** |
| Agentic persona | **Manager** |
| The view | **the Gemba** |
| The decisioning conversation | **Gemba walk** |
| Snapshot | **Checkpoint** |

**No "assigned" concept.** Work is never assigned to a user. Work is either **staged** (in scope, not yet started) or **in progress** (a Worker is actively on it) or at another state_category. The user is the operator; they stage, start, complete — they don't get assigned.

### 3.2 State Categories And Board Labels (ratified)

Gemba keeps six canonical `state_category` buckets at the WorkPlane
contract boundary, but the Board presents a smaller operational view.
The hidden detail stays visible on-card as a state pill.

| State category | Canonical meaning | Default Board presentation |
|---|---|---|
| Backlog | Unrefined / not in the execution funnel | Hidden by default; shown by the Backlog toggle or `/refine` |
| Unstarted | Accepted/refined but not staged into active execution | **Ready** column, card pill **Next up** |
| Staged | Pulled into active scope, waiting for dispatch/dependencies | **Ready** column, card pill **Staged** |
| Started | Active work or wrapper with active child work | **In Progress** column, card pill **In progress** |
| Completed | Finished and accepted | **Done** column, card pill **Done** |
| Canceled | Terminal abandoned/rejected state | List/filter surfaces, not a default Board column |

### 3.3 State transition verbs

| Transition | Verb |
|---|---|
| → `staged` | **Stage** |
| → `started` | **Start** |
| → Completed | **Complete** |
| → Backlog | **Defer** (primary) / "Move to backlog" (secondary) |
| → any earlier state | **Reopen** |

Button labels use the bare verb. Confirmation copy expands: "Stage this Epic?" → "Staged." / "Start this Epic? Workers will be dispatched." → "Started."

### 3.4 Persona action verbs

| Action | Copy |
|---|---|
| Open conversation with a Coach | **Ask** [Coach name] |
| Alternate for casual flow | **Chat with** [Coach name] |
| Invoke a Manager | **Consult** [Manager name] |
| Accept persona output | **Apply** |
| Reject persona output | **Ignore** |

"Consult" implies agentic action may follow. "Ask" implies advisory only. This matches the Coach/Manager distinction from `gm-2yg`.

### 3.5 Gemba walk verbs

| Action | Copy |
|---|---|
| Begin | **Start Gemba walk** |
| End | **End Gemba walk** |
| Pause | **Pause** |
| Decisions on agenda items | **Ratify** / **Modify** / **Reject** / **Defer** |

### 3.6 Checkpoint verbs

| Action | Copy |
|---|---|
| Create | **Checkpoint** (verb) |
| Restore | **Restore** |
| Label | **Label** |

### 3.7 Values + guardrails headers

- Section header: **Project Values**
- Section header: **Guardrails**
- Unmet-guardrail copy pattern: `Guardrail '<name>' violated: <reason>. [action button]`

### 3.8 Value statements injected at top prompt layer (ratified)

```
INNOVATION: favor innovation; seek novel solutions; evaluate against prior art.
TRANSPARENCY: always let the user know when unsure; always disclose unfinished work.
EXECUTION: always trend toward completion and delivery.
EMPATHY: users with a variety of skill, experience, and abilities must be considered.
```

Each gets injected as a first-position system message layer via PromptEnvelope (`gm-3d6`). All four always present; workspace mode cannot disable.

---

## 4. The Gemba (home screen)

**Route:** `/board` (default) or `/` (redirects).
**Primary entity:** Epic.
**Primary action:** Drag-reorder Epic cards to restage. Secondary: multi-select + "Stage selected" batch action.

### 4.1 First-viewport priorities

When a user loads Gemba fresh, these must all be visible without scrolling:
- The operational columns side-by-side: **Ready → In Progress → Done**
- Every Epic card currently in the active swimlane
- Top bar with mode indicator, phase, budget gauge, PM panel toggle
- PM panel (in its last-saved state, collapsed or expanded)
- **Sidebar is collapsed by default on the Board** to maximize the Gemba view

### 4.2 Epic card anatomy

Top-to-bottom inside the card:
1. **Priority stripe** (left edge, 3px wide, color-coded by P0-P4)
2. **Title** (14px, bold, wraps to 3 lines max then ellipsis)
3. **State / attention pills** — canonical state (`Next up`,
   `Staged`, `In progress`, `Done`) plus actionable signals like
   `Triage`, `Needs input`, `Review`, and `Ready`.
4. **Readiness counts** — `3/7 ready · 2 blocked · 1 in progress · 1 done` (12px, muted)
5. **Token-budget gauge** (thin horizontal bar under readiness, hidden if no Sprint+TokenBudget active)
6. **Badges row:**
   - **Parallel-group glyph** (small circle with group letter/color)
   - **Triage pill** for open EscalationRequests targeting the card;
     it clears when the escalation is resolved and the Board refreshes
   - **Purview-violation dot** (yellow-outlined if blocked by a persona Purview in active phase)
   - **Perspective indicator** (small persona icons, 3-max visible, faded; click opens perspective sidebar)

Not in card anatomy (explicitly excluded per template):
- Age since state change — available in the RHP detail tab, not card
- Assignee — not a concept
- Labels — available in the RHP detail tab, not card

### 4.3 Columns

Default execution Board columns, fixed order:
**Ready → In Progress → Done**.

Optional / alternate surfaces:
- **Backlog** appears when the Board Backlog toggle is on; `/refine`
  remains the preferred refinement surface.
- **Canceled** is intentionally not a default Board lane; use Refine table
  filters for audit or recovery.

The **Ready** column is a presentation grouping for canonical
`unstarted` and `staged` work. Cards retain the exact state in a pill
so operators can distinguish "Next up" from "Staged" without paying for
an extra column.

- Column header: bold label + count (`Ready · 3`)
- Column actions on hover: "collapse" / "filter this column"
- WIP limits (from CapabilityManifest) render as `In Progress · 3 / 5`; exceeded = yellow row under header
- Columns auto-scroll independently if needed

### 4.4 Swimlanes

Default: **by parent-epic** (Epics grouped by their root epic; orphans in their own group).
Swimlane switcher (top-right of board): by parent-epic / by parallel-group / by label / none.

### 4.5 Interactions

- **Drag:** whole-card-draggable. Between columns = state transition (nonce-gated per mode). Within column = reorder (sets `UserOrder`).
- **Double-click:** opens Epic detail in the RHP.
- **Right-click:** context menu (stage / start / defer / open in graph / apply checkpoint / open detail / copy ID).
- **Space:** multi-select current card.
- **Cmd-A:** select all visible.
- **Enter after selection:** "Stage selected" for unstarted work or
  "Start selected" for staged work; both appear in the Ready column.
- **J / K:** navigate cards down / up within column.
- **H / L:** move focus to adjacent column.

### 4.6 Empty state

**PROPOSED copy:**
> **No Epics yet.**
> Start a new project with the Onboarder, or import from an existing source.
>
> [New project →] [Import from advanced source →]

### 4.7 Loading state

- Skeleton cards (gray, pulsing) in each column for 300ms minimum
- After 300ms, real content swaps in with 150ms fade
- If SSE connection drops, column header shows a small yellow "reconnecting" indicator; no full-screen blocker

### 4.8 Error state

- Adaptor-degraded: persistent banner across top of board with `Backend 'beads' is degraded: <reason>. [retry]` — NOT modal
- Per-Epic fetch error: card shows a tiny red "!" badge; click reveals error detail

### 4.9 WorkItem-granular view

Alternate view at `/board?view=workitem`. Same operational columns
(`Ready`, `In Progress`, `Done`), but cards are individual WorkItems
instead of Epics. Accessible via keyboard shortcut and the board header
view toggle.

### 4.10 Board controls

Board is the status surface. It exposes the Ready / In Progress / Done
columns by default in both full and Beads-only modes. The `layout` URL
param still accepts legacy values for old bookmarks, but the visible
controls no longer present Board as a multi-layout selector:

| Mode | URL | Purpose |
|---|---|---|
| `workitem` (default) | `/board` | Ready / In Progress / Done status board; the canonical Gemba view |
| `epic` (legacy) | `/board?layout=epic` | Wrapper-level legacy board for existing links |
| `list` (legacy) | `/board?layout=list` | Flat legacy list for existing links |
| `cascade` (legacy) | `/board?layout=cascade` | Hierarchy-first legacy link; Refine hierarchy is the visible home |

The Board header carries focus selectors and a small funnel menu for
board-specific controls: order and optional Deferred lane visibility.
The order selector is:

- Modified
- Created
- Edited
- ID

In full and Beads-only mode the Board stays a status board. Refine owns
table, hierarchy, and swimlane planning projections. `Edited` currently
uses the same persisted `updated_at`
timestamp as `Modified` until the Beads source provides a separate edit
timestamp.

`/grid` (§5.3) is intentionally NOT a Board view-mode: Grid folds into
Refine's table mode because inline edit, column presets, and bulk-action
ergonomics do not fit a board frame.

### 4.11 Built-in Named Views

Named views pin filters and preferred layouts. They live in the URL as
`?view=<name>`; legacy `?preset=` links migrate to the same shape.

| View | Filter | Default layout |
|---|---|---|
| `staged` | state_category = staged | kanban; appears under Ready with Staged pills |
| `in-progress` | state_category = started | kanban |
| `blocked` | derived human-action-required / escalation signals | list |
| `ready-to-stage` | state_category = unstarted + agent-claimable | list |
| `backlog` | state_category ∈ {backlog, unstarted} | list; `/refine` remains the dedicated backlog triage surface |
| `recent` | created within the last 24h | list |
| `done-recent` | state_category = completed; closed within 7d | list |
| `mine` | assignee = current_agent | kanban |

Selecting a named view updates filter chips but doesn't lock them —
users can layer additional chips on top. Saved personal views (per
§6.8) are named-view extensions.

---

## 5. Other screens

### 5.1 Dispatch planning view (`/plan`, planned)

- Primary action button: **Execute all Staged** — top-right, primary color, cmd-Enter shortcut
- Layout: vertical list of Staged Epics, grouped by parallel-group (each group in an outlined cluster)
- Per-Epic row: title / priority / readiness counts / token-budget gauge / drag handle / "unstage" icon
- Scope toggle: drag between columns (Staged ↔ Next Up)
- Cost preview: always visible at the top — `Total cost estimate: $4.20 · ~142k tokens`
- Post-execute: navigate to Board with dispatched Epics visibly transitioning to In Progress

### 5.2 Refine / Backlog (`/refine`)

Backlog triage lives in `/refine`, not in the default Board. The Board
can temporarily show a Backlog column for inspection, and list mode can
filter backlog/unstarted work, but dense refinement belongs on the
Refine surface.

- `/backlog` redirects to `/refine` for legacy links.
- `/refine` filters `state_category=backlog` and exposes dense triage
  controls: priority, age, suggested epic, blockers, labels, and
  dispatch status.
- Refine also offers hierarchy and swimlane views for planning without
  turning Board into a layout switcher.
- Graduating backlog work moves it to `unstarted`; it then appears in
  the Board's **Ready** column with a **Next up** pill.

### 5.3 Grid (`/grid`)

- Legacy redirect to `/refine`; the power-user WorkItem table now lives
  inside Refine.
- TanStack Table with inline-edit, sortable columns, column presets.
- **Default columns:** ID / Type / Title / State / Priority / Parent Epic / Labels / Updated
- Column preset system: saved-views (personal). Shared views are v2.
- Virtualization: up to **1k rows** without performance concern. 10k+ gracefully degrades.
- Bulk-action access: **right-click** on selection OR keyboard shortcut (Cmd-E for edit; Cmd-D for defer; etc.)
- Inline-edit: **click-to-edit** (single click enters edit mode on supported fields; Escape cancels; Enter commits)
- Selection: space-to-toggle; range-select with shift-click

### 5.3.1 Recent catch-up deep link

> Added 2026-04-30 (`gm-g5xz`). User-facing guide:
> [`docs/concepts/recent-view.md`](concepts/recent-view.md).

Reading surface that lists beads created or completed in a chosen
window. It is not a sidebar slot: use `/recent` or the legacy
`/board?view=recent&layout=list` deep link for new work and return to
Board or Refine for the primary working surfaces.

- **Cutoff control:** eight discrete preset stops (1h / 4h / 12h / 24h
  default / 3d / 7d / 30d / All) plus an **Advance to now** button that
  snaps to the 1h window. No free-form date picker — discrete stops
  keep the chrome obvious and prevent picker fatigue.
- **State:** per-browser `localStorage` (key `gemba.recent.watermark`).
  No per-bead "reviewed" flag, no PATCH endpoint — the watermark is
  the entire control surface.
- **Layout:** rows grouped by parent epic / milestone when the parent
  is also in the window; orphans under a **Standalone** group. Sort:
  newest-first inside each group. Click row → standard work-item detail
  tab in the RHP (same as Plan / Refine).
- **Empty state:** `No new beads since {relative}. Try widening the
  window or wait for agents to produce work.`
- **Backend:** `GET /api/work-items?created_since=<ISO8601>` —
  `parseFilterTimestamp` accepts RFC3339 or `YYYY-MM-DD`; the bd
  adaptor pushes `--created-after` to bd CLI; the dolt adaptor adds
  `created_at >= ?` to its WHERE.

Why not a per-bead inbox: agents create faster than humans ack, so a
queue that never empties stops feeling like a queue. The watermark
gives "what's new since I last looked" without per-bead bookkeeping.

### 5.4 Gemba walk (`/walk` or takes over main content when active)

Layout: **two-pane** — agenda left, chat right. PM panel pinned to bottom as usual (but the walk takeover shows the walk chat there).

- **Agenda pane (left, 360px):**
  - Cards per item: icon by source kind / title / urgency badge / drag handle / "decided" checkmark when resolved
  - Kanban-mini sub-layout: Queued / Active / Decided / Deferred columns (vertical mini-kanban)
  - Drag to reorder / mark active / defer
  - Agenda sources (icon legend): ● open escalation, ◉ HITL question, ◆ filed bead, ◇ closed bead, ★ user-added
- **Chat pane (right, flex):**
  - Current active agenda item framed at top: "Agenda #3: [item title] · [source]"
  - PM's framing + proposed actions with inline "Ratify / Modify / Reject / Defer" buttons
  - Volunteered perspectives from other personas surface as inset quote-style sub-turns (not separate messages); up to 3 per item by default, user can expand-all
  - Consulted-personas inline ("[PM consulted Architect] — 1-line perspective")
- **Pause / Resume:** menu option in top-right of the walk pane ("Pause walk", "End walk"). Not a prominent button — walks should feel continuous.
- **Active-walk indicator in chrome:** yellow-tinted banner across the top of the content area: `Gemba walk active · 4 of 11 decided · [resume / end]`

### 5.5 Persona roster — current `/personas` deep route

> **Current implementation:** `/personas` remains a routeable secondary
> surface. The Setup consolidation described in §5.20 is proposed, not
> the current route contract.


- Layout: **grid of cards** (3-4 per row on comfortable desktop)
- Per-card (closed state):
  - Icon / role (big) / variety badge (Coach = purple, Manager = navy)
  - Enabled toggle (persistent)
  - Current model (subtle, bottom)
- Click card → opens an RHP detail tab showing: personality statement / perspective statement / purview (with active phases) / skills opt-in list / budget policy
- Edit config: **modal with TOML editor** (monospace, syntax-highlighted); saves to `.gemba/personas/<id>.toml`

### 5.6 Epic detail tab (drill-down from Board/Refine)

Right-hand panel detail tab.

- **Header:** Epic title / state pill / priority / parallel-group glyph / close X
- **Primary surface:** member WorkItems rendered as **mini-kanban** (four thin columns: Ready / In Progress / Review / Done) with stage-swimlane headers ("Stage 0", "Stage 1", ...)
- **Actions toolbar:** Stage / Start / Complete / Defer / Split / Merge / Add member / Open in graph
- **Tabs below mini-kanban:** Description / DoD / Activity / Comments
- **Inline state-change on members:** click state pill on a WorkItem → popover with valid transitions

### 5.7 WorkItem detail tab

Similar RHP detail tab. Tabs: **Summary / Description / DoD / Activity / Comments**. (Edges tab consolidated into Summary via mini-graph; user answered they don't want a dedicated tab.)

- **Edges display:** typed lists under Summary (blocks / parent_child / relates_to; adaptor extensions grouped below)
- **Evidence:** rendered as a **table** on the Summary tab (not a separate tab — Evidence is always loaded if present)
- **Citations from personas:** linked inline

### 5.8 Escalations inbox (`/escalations`)

- **Grouping:** by severity (critical → high → medium → low → info)
- **Sort default:** severity desc, then age desc (oldest-critical-first)
- **Bulk-triage:** multi-select (checkbox), bulk ack / bulk dismiss / bulk move-to-Gemba-walk
- **Per-card primary CTA:** **Resolve** (primary button) or **Hand-off** (secondary, opens mini-modal to pick a persona)
- **Inline context:** 2-3 lines of the triggering event, plus a link to the originating bead/session

### 5.9 Capability browser — current `/capabilities` deep route

> **Current implementation:** `/capabilities` remains a routeable
> secondary surface. The Setup consolidation described in §5.20 is
> proposed, not the current route contract.


- Layout: **adaptor-per-row**
- Per-row: adaptor ID / transport / last conformance run timestamp + status / declared capability count / health indicator
- **Drill-down:** expand-in-place reveals the full CapabilityManifest (state_map, edge_extensions, field_extensions, etc.) as a nested table

### 5.10 Dep graph (`/graph`)

- **Engine:** React Flow
- Node-per-WorkItem (or Epic if zoomed out; user toggles granularity)
- **Core edges:** solid, colored — blocks (red) / parent_child (navy) / relates_to (tan)
- **Extension edges:** dashed, muted
- **Critical path:** toggle in top-right; also on-hover highlights critical path through the focused node
- **Zoom default:** focus-selected (auto-pan to the selected Epic); empty-selection = fit-all

### 5.11 Insights panel (`/insights`)

- Chart library: recharts
- Default time window: 14d (balances "last sprint" and "this sprint" framing)
- First-viewport metrics (all four visible without scroll):
  - Spawn rate (sessions started per day)
  - Burn-down (current Sprint's TokenBudget consumption)
  - Escalation backlog (open count over time)
  - Token cost (stacked: agent vs advisor per `gm-e11.2`)
- Below: stuck-session minutes, completion rate, merge-queue latency, flake rate
- **Advisor-cost vs agent-cost:** separate charts (ratified), side-by-side at top of the relevant section

### 5.12 QA Health (`/qa/health`)

- Suites grouped by scope (adaptor / core / transport / ui / e2e)
- Per-suite row: name / scope / depth / last run status (green/yellow/red) / coverage delta / flake rate / "Run now" button
- Depth selector on "Run now" button: fast / slow / complete
- Bulk "Run all" with depth selector

### 5.13 QA Gates (`/qa/gates`)

- Layout: list sorted by status (failed → stale → passed)
- Per-gate row: ID / scope / target / condition summary / last-check timestamp / status badge / "Override" dropdown
- Override flow: dropdown reveals two options — "Request persona consensus" (opens modal with persona-chooser) / "Request user override" (opens typing-guard + justification textarea)

### 5.14 Checkpoints timeline (`/checkpoints`)

- Dual visualization: **horizontal timeline** (primary) + **calendar grid** (secondary, collapsible)
- Per-checkpoint: label / trigger kind glyph / age / "Restore" button
- **Restore UX:** confirm-dialog with typing-guard
- **Typing-guard copy:** "Type `restore` to confirm. This will roll back every git repo, the Beads database, live session state, sidecar, and artifacts to this checkpoint. Pushed commits cannot be un-pushed."

### 5.15 Import from advanced source — legacy `/bootstrap` wizard

> **Superseded by `docs/design/newproject.md` (ratified 2026-04-28; amended 2026-04-28 by gm-e12.21.3 + gm-root.17.13).** The primary project-creation path is now the unified **Create-project modal** opened from `/board` (the `/new` route is a redirect that auto-opens the modal). The optional conversational planner lives at `/onboard` and is reachable from the board's empty-state CTA when an LLM client is configured. The four-source Bootstrap wizard has been retired as the primary entry point but still exists at `/bootstrap` for advanced imports while Settings/import consolidation remains unshipped. The "Fresh" source tile is removed — that use case is handled by the Create-project modal.

**Import from advanced source** (`/bootstrap`):

Steps (3): **Source → Analysis → Plan review → Ratify**

- **Source tiles:** Jira / Beads workspace / Source-code repo (each with icon + 1-line description). "Fresh" source tile removed — use the Create-project modal (opened from `/new` or the top-bar `+`) for that case, and the optional `/onboard` conversational planner if an LLM is configured.
- **Analysis:** loading state with live progress ("Scanning 247 issues…" / "Analyzing module structure…" / "Generating epic decomposition…")
- **Plan review:** side-by-side — generated plan on left, consistency report on right. Report format: **summary-then-details** (top-line PASS/WARN/FAIL with drill-down per finding)
- **Ratify:** nonce-confirmed commit of the project config

### 5.16 Project config — current `/project/config` route

> **Current implementation:** `/project/config` remains a shipped
> deep route. The single-scroll Setup consolidation in §5.20 is a
> proposed/historical design direction, not the current route contract.


- Layout: **single-scroll** with sticky section nav on left
- Sections: Values / Guardrails / Goals / Personas / Adaptors / Packs / Integrations / Mode / Subscriptions / Workspace repos / Advanced
- **Values editor:** **modal** (table of current values; add/edit/remove; priority rank; statement textarea)

### 5.17 Pack browser — proposed Setup section

> **Current implementation:** pack browsing is not a primary shipped
> sidebar surface. Treat this as proposed Setup consolidation language
> until a `/setup` route or replacement Settings section ships.


- Layout: marketplace-grid (installed packs highlighted) with list view toggle
- Per-pack card: icon / name / author / version / signed-status badge / description / install button
- **Install UX:** one-click for signed packs; signed-vs-unsigned = warn + checkbox confirmation for unsigned
- **Enable/disable granularity:** per-persona-within-pack (accordion on pack detail page)

### 5.18 Worker session detail

Accessed from: Epic detail → member WorkItem with active session, OR Insights → Active Workers, OR Escalations → session-related escalation.

Workspace.kind-specific affordances (from template):

| Kind | Surface |
|---|---|
| tmux | **Attach** button + **xterm embed** (both; user picks) |
| k8s_pod | Peek + pod status + kubectl-exec button + logs stream |
| container | Railway link (if configured) + docker-exec button |
| subprocess | Process tree viewer |
| exec | Last command + exit code |

### 5.20 Settings and admin consolidation — current + proposed

**Current route contract:** `/settings` and `/settings/pools` are
shipped Settings surfaces. `/project/config`, `/bootstrap`,
`/capabilities`, `/agents`, and `/personas` remain separate deep routes.
There is no shipped `/setup` primary sidebar pane; the single-scroll
consolidation below is retained as a proposed/historical design target.

**Proposed route:** `/setup` (sidebar primary nav). Default landing
section: Adaptors (most operationally relevant).

**Layout:** **Single-scroll** with sticky section nav on left — same pattern §5.16 specifies for Project config, lifted up one level. Each section renders the surface that previously stood alone.

**Sections (top-to-bottom in the sticky nav):**

| Anchor | Section | Was | Notes |
|---|---|---|---|
| `#adaptors` | **Adaptors** | §5.9 Capability browser | adaptor-per-row + drill-down to CapabilityManifest. Default section on `/setup` load |
| `#agents` | **Agents** | (current SPA `/agents`) | agent registry: identity / role / rig / last-seen / health |
| `#personas` | **Personas** | §5.5 Persona roster | grid of persona cards + TOML editor modal |
| `#packs` | **Packs** | §5.17 Pack browser | marketplace-grid + install UX |
| `#project-config` | **Project config** | §5.16 Project config | values / guardrails / goals / mode / subscriptions / workspace repos / advanced (the inner sticky-nav from §5.16 collapses into a single nested accordion within this Setup section) |
| `#import` | **Import from advanced source** | §5.15 Import from advanced source | 3-step import wizard (Jira / Beads workspace / Source-code repo) would render inline in this section. Current implementation keeps it at `/bootstrap`. New project creation goes through the Create-project modal opened from `/board` via the `/new` redirect or the top-bar `+`. |
| `#workspace` | **Workspace + repos** | (new) | repository registry, surface allow-lists (gm-v8vr), workspace mode |

**Behavior:**
- **Deep links** for every legacy route would land on the matching anchor (`/personas` → `/setup#personas`, etc.). This is not the current implementation.
- **Sticky nav** highlights the section currently in view as the user scrolls; clicking jumps with smooth scroll + URL hash update.
- **Per-section actions** (e.g., "Add adaptor", "Run conformance suite", "Install pack") render in a section-local toolbar at the top of each section, not in a global Setup toolbar.
- **Health rollup:** Setup sidebar item shows a badge when any subsection has a degraded adaptor (per §6.5), an unsigned-pack confirmation pending, or a workspace-repo write-allow-list change pending review.
- **Search-within-Setup:** Cmd-F inside `/setup` filters sections to those matching the query (e.g., "openai" surfaces the openai-adaptor row + any pack mentioning it). This supplements global Cmd-K, which still works.

**Why one tab, not seven:** these surfaces are configuration / inspection — high-leverage but low-traffic. Daily users open them once a sprint; consolidating cuts sidebar weight (7 → 4 admin-adjacent items) without hiding anything (Cmd-K still indexes section names and contents).

**What stays out of Setup:**
- **Sessions** (`/sessions`) — operational, not configuration; live agent activity belongs near Insights.
- **QA Health / QA Gates** (§5.12, §5.13) — operational quality surfaces, not setup.
- **Checkpoints** (§5.14) — destructive recovery, not setup.
- **Escalations** (§5.8) — workflow inbox, not setup.

### 5.19 Additional screens deferred to v1.1

Template left these unchecked:
- Retakes lane (`gm-8h9`) — post-ship review; useful-but-not-critical
- Session history timeline (`gm-3dp`) — session-level evidence replay
- Lease dashboard (`gm-a3p`) — active-assignment pressure
- HITL inbox — **RESOLVED:** HITL questions are a subset of Escalations (kind = `hitl_question`), NOT a standalone inbox. Already covered by §5.8.

Retakes lane, session history, lease dashboard: v1.1 unless a Gemba walk promotes them.

---

## 6. Interaction patterns

### 6.1 Modal vs panel vs page

- **Modal** = destructive confirmations (restore checkpoint, force-close convoy, hard-override gate), nonce-gated persona output-apply, import wizard step boundaries (§5.15), Create-project modal (opened from `/board` via the `/new` redirect or the top-bar `+`), Onboarder ratification confirm at `/onboard`, project picker `BindDialog` (gm-root.17.14), value-edit forms, TOML editor on persona config.
- **RHP detail tab** = Epic detail, WorkItem detail, persona config read-only view, quick persona-consult, Gemba walk agenda-item detail. Tabs are opened from the right-hand panel and can be dismissed independently.
- **Page** = top-level navigation surfaces (Board / Refine / Review / Triage / Sessions / Settings / all deep-linked surfaces). Changing pages changes the URL and updates history.

### 6.2 Confirmation patterns (per workspace mode)

All mutations are nonce-gated. UX differs by mode:

| Mode | Confirmation UX |
|---|---|
| Unsupervised | Toast-only ("Staged Epic gm-e3") after success; no pre-action dialog |
| Supervised (default) | Inline confirm for single actions; dialog for destructive ones; batch summary at end of batch actions |
| Managed | Blocking dialog before every mutation: "You are about to [Stage gm-e3 + 2 Epics]. Confirm?" Summary-then-confirm pattern mandatory |

Destructive actions ALWAYS require typing-guard, regardless of mode: restore checkpoint, force-close (convoy), hard-override (gate), delete (pack), cancel Epic (if In Progress or later).

### 6.3 Loading states

- **Skeleton screens:** only-on-slow (>300ms). For fast loads, the UI just renders — no skeleton flash.
- **Spinner:** inline for action-specific feedback (e.g., "Consulting Architect…" in PM panel). Never full-screen.
- **Streaming indicators (SSE):** small live-dot in top bar. When data arrives mid-render, surface with a subtle fade-in; do NOT re-flow aggressively (user was looking at something).

### 6.4 Empty states

- **Tone:** welcoming (friendly / illustrative aesthetic; not instructive-bossy, not quirky-cringe)
- Pattern: illustration (small, playful) + one-sentence empty message + single primary CTA
- **Primary CTA rule: PROPOSED** — always offer the next logical action, never a dead-end. "No Epics" → "New project" (primary) / "Import from advanced source" (secondary). "No escalations" → "See the Board" (because no escalation is good news).

### 6.5 Error states

| Error | Surface |
|---|---|
| API error (non-fatal) | Inline alert (next to the affected UI; dismissible) |
| API error (fatal, can't load view) | Full-page error with retry + diagnostic link |
| Adaptor-degraded (`gm-b1`) | **Both** persistent banner (top of affected view) AND degraded-badge on affected surfaces (e.g., per-Epic if that Epic's adaptor is degraded) |
| Rate-limit | Backoff-toast with ETA; queue visible in Insights (adaptor-level health section) |
| Persona consult failure | Inline in PM panel: "Consult failed: <reason> · [retry] · [switch persona]" |

### 6.6 Keyboard-first ergonomics (per gm-7hj 26-shortcut plan + overrides from template)

- **Global:** (no overrides beyond gm-7hj defaults)
- **Board:** Cmd-K opens palette; J/K/H/L navigate; Space selects; Enter acts on selection; Shift-Click range-selects
- **Grid:** J/K/Home/End navigate; Space selects; Cmd-E edits focused row
- **RHP:** Enter opens focused member's detail tab; Escape dismisses the active transient surface when applicable
- **Persona chat:** (no overrides; Cmd-Enter sends)
- **Gemba walk:** Cmd-G toggles walk; J/K navigate agenda; R/M/X/D decide (Ratify/Modify/reject/Defer)

### 6.7 Search + command palette (Cmd-K)

- **Content indexed:** beads / artifacts / commands / all (per template)
- **Ranking:** mixed (combines recency, frequency, textual relevance)
- **Action vs. navigation split:** keyboard-hint only — a small badge on the right of each result ("action" / "go to") without visual tabs. Palette is scanable without vertical-segmenting.

### 6.8 Filtering + saved views

- **Filter persistence:** URL (ratified)
- **Saved views visibility:** personal (ratified; shared views v2)
- **Default views shipped** (renamed per template clarification — no "assigned to me" concept):
  1. **Staged** — `state_category=staged`; appears in the Board's Ready column with a Staged pill
  2. **In Progress** — active work
  3. **Blocked** — open EscalationRequest OR unmet guardrail OR Purview violation
  4. **Ready to stage** — unstarted + agent-claimable
  5. **Recently Done** — Completed in last 7d

### 6.9 Multi-select + bulk actions

- **Selection affordance:** checkboxes on Grid rows; space-to-toggle on Board cards
- **Bulk bar:** appears as a **sidebar** (right side, overlays content briefly) when ≥1 selection; auto-dismisses 5s after selection clears

### 6.10 Responsive behavior

- Minimum viewport: **1024px** (below this, show compact notice "Best on 1024+; you may experience layout issues.")
- Touch optimization: **yes** for tap targets (44px min) but primary UX assumes mouse + keyboard
- iPad support: nice-to-have (works in landscape; portrait is not polished)
- Mobile native: explicitly out of scope (§9)

---

## 7. Cross-cutting UX

### 7.1 Always-on elements

- **PM panel** (bottom drawer): always available; no per-screen override needed
- **Active-Gemba-walk banner**: shown when walk is active, across top of content area
- **Unmet-guardrail banner**: shown only where guardrail applies (inline on the affected Epic/view, NOT top-bar-global)
- **Mode indicator**: icon-pill in top bar (always)
- **Budget gauge**: top-bar (always, dual-axis)
- **Phase indicator**: top-bar pill (always; click reveals active purviews)

### 7.2 Persona invocation feedback

- **Pre-invocation:** **nothing** (per template — no cost-preview modal cluttering fast flows). User clicks; action initiates. Cost surfaces post-hoc.
- **In-flight:** spinner next to the persona's turn in the PM panel ("Architect is thinking…")
- **Completed:** scroll-into-view + optional browser notification (user opt-in at workspace level)

### 7.3 Citations rendering

- **Inline superscript links** (per template). Hover reveals the cited entity preview (bead card, file snippet, prior consult summary).
- Citations collapse to a compact "3 citations" footer badge if >5 per turn; click expands.

### 7.4 Audit log surface

- `/insights/personas` — visible to all workspace users (per template)
- Filter default: **last-7d**
- Filters: persona / skill / user-action (applied/dismissed/modified) / cost bucket

### 7.5 External-tool handoffs

| Target | Behavior |
|---|---|
| Jira link | open-new-tab |
| XRay link | open-new-tab (consistent with Jira) |
| GitHub PR | embed (inline preview in the RHP detail tab; full PR opens new tab if requested) |

---

## 8. Adaptor + pack UX

### 8.1 Adaptor switching
- **Active adaptor pair display:** intentionally hidden from most surfaces. The point of the abstraction is that users don't think about it.
- **Where it IS shown:** Project Config / Integrations section; Capability browser
- **Switch-adaptor UX:** **forbidden via UI in v1.** Config-file-only (`.gemba/workspace.toml`). Rationale: switching adaptors mid-project is high-risk and rare; making it UI-accessible invites accidents.

### 8.2 Pack management

- **Install:** from URL OR local-path (v1; marketplace-browse is v2)
- **Signed vs unsigned:** warn + explicit checkbox confirmation for unsigned
- **Enable/disable granularity:** per-persona-within-pack (an installed Pack's personas are individually toggleable)

### 8.3 Extension widgets (`web/src/extensions/<adaptor-id>/`)

- **Discoverability:** badge-on-adaptor-views (small adaptor-ID chip on surfaces where an extension contributes)
- **Adaptor-specific copy:** inline (extensions render in-context, not in a separate panel)

---

## 9. Accessibility + i18n

Per template's explicit answers (lean toward acceptable-not-bleeding-edge):

- **WCAG target:** best-effort / no formal target. Do the basics: sufficient contrast, semantic HTML, visible focus rings. Don't gate releases on audit.
- **Keyboard-only coverage:** most actions keyboard-reachable; some mouse-only is acceptable (bulk-drag operations)
- **Screen reader:** reasonable (semantic HTML; no custom ARIA unless necessary for a specific component)
- **Languages:** English only; schema supports `language` field for future i18n (no translations shipped v1)
- **RTL:** designed RTL-ready from day one (use logical CSS properties, avoid hard-coded `left`/`right`)

---

## 10. Non-functional priority order

Per template ranking (1 = highest):

1. **Visual polish** — the product must feel good to use; friendly illustrative aesthetic
2. **Cognitive-load minimization** — every UX tradeoff resolves in favor of less thinking
3. **Mutation safety** — nonce-gating + audit trail non-negotiable
4. **Keyboard ergonomics** — power-user features via keyboard, not mouse-exclusive
5. **Performance** — 60fps nice but not required; <500ms for primary interactions acceptable
6. **Accessibility** — best-effort, semantic, keyboard-reachable; not AA-certified
7. **Internationalization readiness** — architectural readiness, not shipped translations
8. **Testability / automatable** — sufficient to prove correctness; not exhaustive

Performance-v-polish tradeoffs that come up: **choose polish unless the performance impact is >2x perceived-lag.** Loading skeletons, animated transitions, illustrative empty states, live-update flow animations all stay in. If a specific interaction drops under 30fps, then optimize.

---

## 11. What NOT to build (ratified)

- Mobile native apps
- Offline mode
- Real-time collaborative editing (Google Docs-style cursors)
- Built-in chat between humans
- Native calendar integration
- Time-tracking / billable-hours
- PDF / printable exports (exception: release notes via Documentarian)
- Multi-workspace federation in the UI (v1)

---

## 12. Open — flagged for mike's next review

Items marked **PROPOSED** above that would benefit from an explicit ratification but don't block implementation:

1. §1.2 Primary accent color — navy + tan proposal
2. §1.3 Body font = Inter; monospace = JetBrains Mono
3. §4.6 Empty-state copy for the Board
4. §6.4 Primary-CTA rule for empty states
5. §6.2 "Destructive actions always require typing-guard" — confirm the list is complete (restore, force-close, hard-override, delete pack, cancel In-Progress Epic)

None block generation of component-level specs or implementation. These are aesthetic / copy polish decisions that a UX Expert Coach consult can close later.

---

*Authoritative UI spec. Ratified answers from `ui-spec-template.md` + ratified design decisions upstream. Deviations from this document during implementation should trigger a Gemba walk with UX Expert + Project Manager present.*
