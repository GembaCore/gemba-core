# Recent work: catch up on what agents just made

When agents are dispatching at scale, they can create epics and beads
faster than an operator can read them. **Recent** is now a catch-up deep
link rather than a top-level sidebar pane.

## At a glance

| | |
|---|---|
| **Primary route** | `/board?view=recent&layout=list` |
| **Sidebar slot** | None; use the deep link or command palette |
| **Legacy route** | `/recent` still exists as a deep link |
| **Default window** | Last 24 hours |
| **State** | URL + unified work-item view storage |
| **Backend** | `GET /api/work-items` plus client-side view filtering |

## How it works

The work-item view registry still defines **Recent** as work items
created in the last 24 hours, sorted newest first. **Done · 7d** is the
companion catch-up view for recently closed work.

Use the command palette or legacy link when you need catch-up:

1. Open `/recent` or `/board?view=recent&layout=list`.
2. Review the newly created work in list form.
3. Return to **Board** (`/board`) when you need state columns, or
   **Refine** (`/refine`) when you need table/hierarchy planning.

The old standalone Recent page is no longer a first-order navigation
concern. It remains routeable for bookmarks and direct links, but new
operator flows should prefer the Board named view.

## Why it moved

Recent is a filter over work items, not a separate job. Folding it into
the Board keeps daily navigation focused on the core verbs:

- **Board** (`/board`): execute, inspect, and filter current work.
- **Refine** (`/refine`): groom backlog work before it enters execution.
- **Review** (`/walk`): take a Gemba walk over work in progress.
- **Triage** (`/escalations`): respond to blockers and escalations.

## URL migration

The Board migration layer normalizes old view/preset shapes into the
canonical `?view=` slot. Examples:

```text
/board?preset=recent       -> /board?view=recent
/board?view=recently-done  -> /board?view=done-recent
/grid                      -> /refine
```

## Source pointers

- View registry: `web/src/lib/workItemViews.ts`
- Board toolbar: `web/src/pages/BoardPage.tsx`
- Sidebar nav: `web/src/components/Sidebar.tsx`
- Legacy Recent route: `web/src/pages/RecentPage.tsx`
- URL migration tests: `web/src/lib/__tests__/workItemViews.test.ts`
