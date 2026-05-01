# Recent work: catch up on what agents just made

When agents are dispatching at scale, they can create epics and beads
faster than an operator can read them. **Recent** is now a Board named
view rather than a top-level sidebar pane.

## At a glance

| | |
|---|---|
| **Primary route** | `/board?view=recent&layout=list` |
| **Sidebar slot** | None; use **Plan → View → Recent** |
| **Legacy route** | `/recent` still exists as a deep link |
| **Default window** | Last 24 hours |
| **State** | URL + unified work-item view storage |
| **Backend** | `GET /api/work-items` plus client-side view filtering |

## How it works

The Board has one canonical named-view registry. The **Recent** chip
filters to work items created in the last 24 hours and sorts newest
first. **Done · 7d** is the companion catch-up view for recently closed
work.

Use the Board toolbar:

1. Open **Plan** (`/board`).
2. Click **View → Recent**.
3. Stay in the default list layout or switch to kanban/work-item layout
   if you need to compare the recent items in their execution columns.

The old standalone Recent page is no longer a first-order navigation
concern. It remains routeable for bookmarks and direct links, but new
operator flows should prefer the Board named view.

## Why it moved

Recent is a filter over work items, not a separate job. Folding it into
the Board keeps daily navigation focused on the core verbs:

- **Plan** (`/board`): execute, inspect, and filter current work.
- **Refine** (`/refine`): groom backlog work before it enters execution.
- **Review** (`/walk`): take a Gemba walk over work in progress.
- **Triage** (`/escalations`): respond to blockers and escalations.

## URL migration

The Board migration layer normalizes old view/preset shapes into the
canonical `?view=` slot. Examples:

```text
/board?preset=recent       -> /board?view=recent
/board?view=recently-done  -> /board?view=done-recent
/grid                      -> /board?layout=list&power=1
```

## Source pointers

- View registry: `web/src/lib/workItemViews.ts`
- Board toolbar: `web/src/pages/BoardPage.tsx`
- Sidebar nav: `web/src/components/Sidebar.tsx`
- Legacy Recent route: `web/src/pages/RecentPage.tsx`
- URL migration tests: `web/src/lib/__tests__/workItemViews.test.ts`
