# Recent view: catch up on what agents just made

When agents are dispatching at scale, they spawn epics and beads faster
than you can read them. **Recent** (`/recent`) is a top-level pane that
shows the work items created in a window you choose, so you can scan the
freshest output without forcing yourself to ack every bead.

## At a glance

| | |
|---|---|
| **Route** | `/recent` |
| **Sidebar slot** | Between **Refine** and **Review** |
| **Default window** | Last 24 hours |
| **State** | Per-browser `localStorage` (no server round-trip) |
| **Backend** | `GET /api/work-items?created_since=<ISO8601>` |

## How the watermark works

The watermark is a single timestamp that says *"only show me beads
created at or after this point."* It is **not** a per-bead "reviewed"
flag — there is no list to drain, no checkboxes, nothing to ack. You
move the cutoff; the list re-renders.

You control the watermark with eight preset stops:

```
1h    4h    12h    [24h]    3d    7d    30d    All
```

- **1h–7d** are progressively wider windows. Pick the one that matches
  how often you check in.
- **30d** and **All** are for catching up after time away or auditing
  what an agent has produced over a long stretch.
- **Advance to now** snaps the cutoff to the most recent (1h) window —
  use it once you've scanned the page and want a quick "I've seen this"
  reset.

Your selection persists across reloads; reopen the tab tomorrow and the
watermark is where you left it.

## Why a watermark and not "mark as reviewed"

We considered an inbox-style queue where each bead had to be explicitly
acked. We dropped it for two reasons:

1. **Agents create faster than humans ack.** A queue that never empties
   stops feeling like a queue and starts feeling like noise.
2. **The information you actually need is "what's new since I last
   looked."** A watermark answers that without per-bead bookkeeping.

If you want the queue model — explicit ack with a per-bead reviewed
flag — open an issue. We're happy to revisit if the watermark turns
out to be the wrong shape once it's been in operators' hands for a
while.

## Layout

Items are grouped by parent epic / milestone when their parent is also
in the window. Otherwise they appear under a **Standalone** section.
Within each group, rows are sorted newest-first. Click any row to open
the work item in the right-hand-panel drawer (same drawer as Plan /
Refine / Review).

## When to use Recent vs other views

- **Recent**: "What did agents make in the last few hours?" Reading
  surface; no triage actions.
- **Refine** (`/refine`): "What needs grooming in the backlog?" Action
  surface — defer, dismiss, drop into epic, bulk-edit.
- **Plan** (`/board`): "What's executing or staged for execution?"
  Kanban with drag-to-spawn.
- **Review** (`/walk`): "Let's go over the in-progress work item by
  item." Conversational walk-through with PM persona.
- **Triage** (`/escalations`): "Something is blocking — does someone
  need to look?"

Recent is the cheapest of these — it makes no claim about what you
*should* do, only what just happened.

## Multi-rig

Recent is scoped to the active project, like every other read surface.
Switch projects in the picker and the watermark + filter rebind to the
new workspace's beads. Per-workspace watermarks are not separate today;
the single key persists across project switches. If you find yourself
wanting per-project watermarks, file an issue.

## API

The view is a thin client over a single backend filter:

```
GET /api/work-items?created_since=2026-04-30T10:00:00Z
```

`created_since` accepts RFC3339 (with or without nanos) or
`YYYY-MM-DD`. Items created at or after the cutoff are returned; older
items are filtered out at the bd / Dolt adaptor layer (`bd list
--created-after`) so the wire payload stays small.

## Source pointers

- Route + page: `web/src/pages/RecentPage.tsx`
- Sidebar nav entry: `web/src/components/Sidebar.tsx`
- Filter shape: `web/src/api/workItems.ts` (`WorkItemListFilter.created_since`)
- Backend parser: `internal/server/work_items.go` (`parseFilterTimestamp`)
- Adaptor pushdown: `internal/adapter/bd/workplane.go` (`--created-after`),
  `internal/adapter/dolt/workplane.go` (`created_at >= ?`)
- Tests: `web/src/pages/__tests__/RecentPage.test.tsx`,
  `internal/server/work_items_filter_test.go`,
  `internal/adapter/bd/workplane_filter_test.go`
- Tracking: epic `gm-g5xz`, children `gm-g5xz.1` (backend) /
  `gm-g5xz.2` (route + page) / `gm-g5xz.3` (watermark) /
  `gm-g5xz.4` (docs)
