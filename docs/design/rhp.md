---
title: "Right-Hand Panel — Help + detail tabs (drawer replacement)"
decision: gm-root.22.1
---

# Right-Hand Panel — Help + detail tabs (drawer replacement)

**Status:** design · tracked in `gm-root.22.1`
**Parent epic:** [gm-root.22](../../.beads/) — Right-hand panel — Help + detail tabs (drawer replacement)
**Author:** mike (captured by polecat)
**Date:** 2026-04-29

## Why this doc exists

The SPA's right-side surface today is a stack of one-off drawers
(`WorkItemDrawer`, `EpicDrawer`, `RecommendOrderDrawer`) plus several
panel-shaped surfaces (`PmPanel`, `EscalationPanel`, `PerspectivePanel`,
agent-detail panels). Drawers stack two-deep at most and disappear on
route change. Help / coaching for new operators has no surface at all.

This document locks the design for the **Right-Hand Panel** (RHP) — a
persistent right-side panel that hosts a vertical tab rail and unified
detail surfaces. RHP replaces the drawer pattern; it is **not** a
unification of every right-side surface in the app (`PmPanel` etc.
stay where they are for v1; revisit after drawers are gone).

## Scope

This document covers v1:

- The RHP shell — chrome, tab rail, collapse/expand, width persistence.
- The Help tab — pinned, hand-authored TSX per route, live links to
  published guides, cold-start branch.
- The detail-tab system — URL state, kind-replace / kind-stack
  semantics, deep-link, per-current-route scoping.
- The migration plan — drawer-by-drawer, with teardown at the end.

Out of scope (deferred):

- Coach as a pinned tab (sibling bead `gm-root.23` ratifies once v1
  lands).
- Migration of non-drawer panel-shaped surfaces (`PmPanel`,
  `EscalationPanel`, `PerspectivePanel`, agent-detail panels).
- Mobile / narrow-viewport behavior. v1 targets desktop.

## Layout

```
┌─sidebar─┬─main pane──────────────────────┬─RHP──────────────────────────┐
│ Plan    │                                │ ▏[?]│                       │
│ Review  │                                │ ▏   │ active tab content    │
│ ...     │     route content              │ ▏[•]│                       │
│         │                                │ ▏[•]│                       │
│ Settings│                                │ ▏   │                       │
└─────────┴────────────────────────────────┴─────┴───────────────────────┘
                                              ↑ tab rail (left of RHP)
```

- RHP renders to the right of `<main>` inside `AppShell`. Always
  present.
- Tab rail is a vertical column on the panel's left edge — Lucide
  icons, ~40px wide.
- Active tab fills the remaining width. Content is per-tab.
- A drag handle on the RHP's left edge resizes the panel. Width
  persisted in `localStorage` under `gemba.rhp.width` (number of
  pixels). Default 384, min 320, max 800.
- A caret button at the top of the rail toggles collapsed/expanded.
  Collapsed = rail only (40px wide); expanded = rail + content.
  Persisted under `gemba.rhp.collapsed` (boolean). Default `false`.
- Cold-start (no tabs registered yet at all) defaults to collapsed
  so the operator sees max canvas.

## Tab rail

- Tabs render as Lucide icons in a vertical column.
- Click an icon → focuses that tab.
- Active tab gets a visual treatment: filled background, accent border
  on the right edge of the icon, optional small label tooltip on hover.
- Rail scrolls vertically when more icons are present than the
  viewport allows. Active tab is kept in view via
  `scrollIntoView({ block: 'nearest' })` when focus changes.
- A small close button (`×`) overlays detail-tab icons on hover (or
  always-visible — implementation choice). Pinned tabs cannot be
  closed.
- Order:
  1. Pinned tabs at the top, in registration order.
  2. A subtle separator (1px line).
  3. Detail tabs below, in pop-order (oldest first; newest at the
     bottom).

### Pinned tabs (v1)

Just **Help**. Coach is deferred (`gm-root.23`).

### Detail tabs

Pop on existing drawer triggers (a `?bead=X` URL param, a
`/board/:epicId` navigation, a click on a session card, etc.).

**Kind-replace / kind-stack rule:**

- Each detail tab has a **kind** (`workitem`, `epic`, `session`,
  `escalation`, `walk-item`, `persona-consult`, …).
- Popping a detail of a kind that is already open **replaces** the
  open one's id and focuses it.
- Popping a detail of a kind that is NOT yet open **stacks** it as a
  new tab (different-kind = new tab) and focuses it.

**Per-current-route scoping:**

Detail tabs are tied to the current route. Route change (a different
`pathname`) clears all detail tabs and the `?rhp` URL param; Help
auto-refocuses. This matches the drawer pattern's behavior — drawers
on `/board` go away when you navigate to `/walk`.

**Cap and overflow:**

The kind set is finite (~7-10 kinds across all routes), and same-kind
replaces, so the tab rail is naturally bounded. Vertical scroll
handles overflow if it ever happens. No FIFO eviction in v1.

## URL state

Detail-tab state lives in a single query param:

```
?rhp=<kind>:<id>(,<kind>:<id>)*
```

- Order = stacking order. Rightmost = focused.
- Examples:
  - `?rhp=workitem:gm-1` — single workitem detail tab open.
  - `?rhp=workitem:gm-1,epic:gm-2` — workitem + epic, epic focused.
- Parser tolerates malformed segments by dropping them silently and
  rewriting the URL on first paint to the canonical shape.
- Codec is colon-and-comma to keep URLs readable; ids are
  workspace-prefixed in practice (`gemba/gemba/gm-1`) so the parser
  splits on the FIRST `:` only and treats everything after as the id.
- Deep-links work: navigating to `/board?rhp=workitem:gm-1,epic:gm-2`
  pre-pops both tabs; epic is focused.
- Closing a detail tab updates the URL via `setSearchParams(next,
  {replace: true})` so the history entry stays clean.

### Back-compat with legacy URL params

Two existing URL conventions need migration:

- `/board?bead=X` → translates to `?rhp=workitem:X` on first paint.
  The legacy `bead` param is cleared from the URL after migration.
- `/board/:epicId` → opens an `epic` detail tab on mount. The path
  segment stays (route shape is unchanged); the RHP picks it up.

The migration shim runs once on first paint of a route; it does not
loop with `setSearchParams` causing re-renders.

## RHP context API

```ts
// web/src/components/rhp/RhpContext.tsx — or whatever the shell bead picks
interface RhpTab {
  id: string;                  // unique within the panel ('help', 'workitem:gm-1')
  kind: string;                // 'help' for pinned; otherwise the detail kind
  pinned: boolean;
  icon: LucideIcon;
  label: string;               // shown as title bar of the tab body + tooltip on rail
  // The render function receives no props for pinned tabs; for detail tabs
  // the registry resolves a content component by kind and passes the id.
}

interface RhpDetailRequest {
  kind: string;
  id: string;
  // The icon and label are derived by the kind registry; callers don't
  // pass them through.
}

interface RhpAPI {
  // State
  tabs: RhpTab[];
  activeTabId: string | null;

  // Tab management
  focusTab(id: string): void;
  closeTab(id: string): void;          // no-op on pinned tabs
  popDetail(req: RhpDetailRequest): void;
  closeDetail(kind: string, id?: string): void;

  // Pinned-tab registration
  registerPinnedTab(tab: { id: string; icon: LucideIcon; label: string }): () => void;

  // Layout state
  collapsed: boolean;
  setCollapsed(next: boolean): void;
  width: number;
  setWidth(next: number): void;
}

const useRhp = (): RhpAPI;
```

The shell bead (`gm-root.22.2`) ships `useRhp`. Pinned-tab consumers
(Help in `.3`) call `registerPinnedTab` once on mount. Detail-tab
consumers (drawer migrations) call `popDetail` from the trigger sites.

### Detail content registry

```ts
// Registers a content component for a kind. Returns an unregister function.
interface RhpDetailContentRegistration {
  kind: string;
  render: (id: string) => ReactNode;
  icon: LucideIcon;
  label: string;
}
function registerDetailContent(reg: RhpDetailContentRegistration): () => void;
```

The detail-tab system bead (`gm-root.22.4`) ships the registry. Each
drawer-migration bead (`.5`, `.6`, `.7`) calls `registerDetailContent`
with its kind once on mount. The kind owner supplies the icon and
label as part of the registration so kind metadata stays in one place
— the RHP itself does not bake a kind→icon map. Calling
`popDetail({kind: 'unregistered'})` falls back to a placeholder ("no
content registered for kind X") and the rail uses a sentinel fallback
icon registered by the RHP itself on mount.

A hook variant `useRegisterDetailContent(reg)` is provided for
components that want to register on mount without writing the
useEffect boilerplate manually.

## Help tab

### Authoring

One TSX module per top-level route, plus a cold-start variant and a
default fallback. Modules live under `web/src/help/`:

```
web/src/help/
├── index.ts          ← route → module registry
├── BoardHelp.tsx
├── WalkHelp.tsx
├── EscalationsHelp.tsx
├── InsightsHelp.tsx
├── SessionsHelp.tsx
├── SettingsHelp.tsx
├── ColdStartHelp.tsx
└── DefaultHelp.tsx
```

Each module exports a `<RouteHelp />` React component returning JSX:

- **Route summary** — 1-2 sentences describing what the route is for.
- **What you can do here** — a bulleted list with `<Link>` /
  button-as-link affordances:
  - Internal route links (`<Link to="/board">`)
  - Modal openers (e.g. open the Create-project modal)
  - Hotkey hints (`Press D to defer the active item`)
- **Learn more** — anchor links to the published docsite guides
  (URLs already in `README.md`).

`HelpTab.tsx` reads `useLocation()` to pick the right module. Unknown
routes fall back to `DefaultHelp.tsx`. Cold-start (no active project)
overrides any route to `ColdStartHelp.tsx`.

### Default-active behavior

Help is the default-active tab on first mount. When the operator pops
a detail tab, focus shifts to that detail. When all detail tabs are
closed (or a route change wipes them), Help auto-refocuses.

### Cold-start

Cold-start `HelpTab` greets the operator and points at:

- The `+` affordance / project picker for project creation.
- Settings (always operational on cold-start).
- The Getting-Started guide on the docsite.

It does NOT mention any workspace-scoped surfaces.

## Modals

Existing modals (`RatifyModal`, `ResolveModal`, `BindDialog`,
Create-project modal, etc.) continue to overlay the page on top of
the RHP. The RHP's z-index is lower than modal-overlay z-index (which
already sits above sidebar + topbar + main content). No changes
required to modal infrastructure.

## Test conventions

- Component tests for the shell live at
  `web/src/components/rhp/__tests__/RhpShell.test.tsx`. Cover render,
  focus, close, collapse, width persistence, rail overflow scroll.
- Help tests at `web/src/components/rhp/__tests__/HelpTab.test.tsx`
  plus per-route help module tests under `web/src/help/__tests__/`.
- Detail-tab system tests at
  `web/src/components/rhp/__tests__/RhpDetail.test.tsx` cover the
  kind-replace / kind-stack rule, URL codec, deep-link, route-change
  cleanup.
- E2E specs under `testing/e2e/specs/rhp/`:
  - `shell.spec.ts` — chrome, collapse, persistence.
  - `help-tab.spec.ts` — default-active, route switching swaps
    content, cold-start variant.
  - `detail-tabs.spec.ts` — pop a workitem, pop another → replace;
    pop epic → stack; close → URL drops segment; deep-link round-trip.

## Migration plan

Drawer-by-drawer. Each migration is its own bead and has its own
content component + tests.

| Wave | Bead | Drawer → tab |
| --- | --- | --- |
| 1 | `gm-root.22.2` | RHP shell |
| 1 | `gm-root.22.3` | Help tab v1 |
| 1 | `gm-root.22.4` | Detail-tab system |
| 2 | `gm-root.22.5` | `WorkItemDrawer` → `WorkItemDetail` (kind: `workitem`) |
| 2 | `gm-root.22.6` | `EpicDrawer` → `EpicDetail` (kind: `epic`) |
| 2 | `gm-root.22.7` | `RecommendOrderDrawer` → `RecommendOrderDetail` (kind: `persona-consult` or `consult:recommend_order`) |
| 3 | `gm-root.22.8` | Tear down legacy drawer code + sweep test-ids |

Each Wave-2 migration:

1. Build a new `*Detail.tsx` component containing the same content
   the drawer rendered (sans overlay shell, sans close button — those
   live on the tab).
2. Register it via `registerDetailContent({kind: 'X', render})`.
3. Update the trigger sites (e.g. card click on the board) to call
   `popDetail` instead of opening the drawer.
4. Delete the drawer + its tests.
5. Sweep e2e specs that referenced the drawer's `data-testid`s.

## Coach (deferred)

Sibling bead `gm-root.23` decides Coach. Locked context:

- Dedicated mini-persona (similar to Onboarder), distinct from
  PmPanel.
- Backed by a `route_help` skill in the consults dispatcher.
- Conversation lifetime: per (route + active detail tab) — fresh
  thread when the context shifts.

What `gm-root.23` decides at pickup:

- Whether Coach is a separate pinned tab or a sub-mode of the Help
  tab.
- When to ratify (probably after at least the WorkItem + Epic
  migrations land, so the help-text surface has matured).
- Authoring + scope of the `route_help` skill.

## References

- Parent epic: `gm-root.22` — Right-hand panel.
- Drawer surfaces being replaced (today):
  - `web/src/components/board/WorkItemDrawer.tsx`
  - `web/src/components/board/EpicDrawer.tsx`
  - `web/src/components/persona/RecommendOrderDrawer.tsx`
- Adjacent panel-shaped surfaces (NOT in v1 scope):
  - `web/src/components/pm/PmPanel.tsx`
  - `web/src/components/sessions/EscalationPanel.tsx`
  - `web/src/components/walk/PerspectivePanel.tsx`
  - `web/src/views/agentDetailPanels/*Panel.tsx`
- Coach decision: `gm-root.23`.
