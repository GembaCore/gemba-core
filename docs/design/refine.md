---
title: "/refine — dedicated triage + refinement surface"
decision: gm-3ofd
---

# /refine — dedicated triage + refinement surface (gm-3ofd)

> **Status**: accepted 2026-04-29.
> **Supersedes**: the `?layout=list&view=backlog` crutch.
> **Sibling**: gm-5ekd — drop the Backlog column from Board's kanban
> with a toggle.

## Goal

A workspace dedicated to triage and refinement of
`state_category=backlog` work items, distinct from the Board's
execution kanban. Backlog work has different ergonomics from
in-flight work — density-rich tabular layout, priority weighting,
dependency visibility, suggested-epic placement, bulk triage
actions.

## Why this lives outside the Board

The Board is execution chrome — flow stages, swimlanes, scope.
Refinement is a sorting / triaging activity, not a flow stage. The
Board's list mode (`?layout=list`) was an interim affordance; it
does fine for general list browsing but is anemic for actual
refinement. /refine reclaims the surface.

## Surface

- **Route**: `/refine`. Reclaims `/backlog` (which gm-e12.19.1
  redirected to Board's list mode); the redirect target shifts to
  `/refine`.
- **Sidebar entry**: top-level pane between Plan and Review. Same
  visual treatment as the other top-level entries; `workspaceScoped:
  true` so cold-start mutes it.

## Default view

- **Filter**: `state_category=backlog` is hardwired. The state-
  category chip bar is hidden — this is a backlog surface by
  definition.
- **Sort**: priority desc, then age desc (older items bubble up
  inside a priority band so true neglect is visible).
- **Power features**: bulk-action surface, column presets, JSONL
  import are always on. /refine's whole reason to exist is dense
  triage; no soft mode.

## Columns

Tabular grid keyed to backlog rows. v1 column set:

| Column | Source | Notes |
|---|---|---|
| (selection) | grid | Bulk-action checkbox |
| id | `WorkItem.id` | mono, fixed-width |
| kind | `WorkItem.kind` | chip |
| title | `WorkItem.title` | truncate; wrap on hover |
| age | `now() - created_at` | "12d", "3mo" |
| priority | `WorkItem.priority` | inline-editable |
| blockers | derived from blocks/parent_child | count chip |
| suggested epic | Selection (planner-coach) opinion | dim when absent |
| last-touched | `WorkItem.updated_at` | "2h", "5d" |
| labels | `WorkItem.labels` | chips |
| dispatch_status | `WorkItem.dispatch_status` | non-default only |

Sortable on every column. Visible-column presets (operator-saved)
ride on the existing power-mode preset infrastructure.

## Single-row actions

- **Triage to Unstarted** — `state_category` → `unstarted` (graduate
  to the active funnel).
- **Set priority** — inline edit + bulk dialog.
- **Drop into an epic** — pick a target epic; sets `parent_id` via
  the gm-gsbj plumbing. Clears any prior parent.
- **Defer** — back to backlog with optional defer-until note.
- **Dismiss** — `state_category` → `canceled` with an optional reason.

## Bulk actions

Mirrors the gm-uipx.17 power-mode list bulk surface:
- Cmd-E — bulk edit (priority, labels)
- Cmd-D — bulk dismiss (with confirm)
- Right-click — context menu carrying every single-row action
  applied to the selection
- JSONL import — bulk priority adjustments

The bulk surface is the existing `WorkItemGrid` machinery — /refine
does not invent its own.

## Capability gating

A WorkPlane adaptor that doesn't expose `state_category=backlog` (a
flow without a backlog stage) MUST hide the /refine route. The
sidebar entry mutes; direct navigation 404s. Detection: if the
WorkPlane's manifest `StateCategoriesSupported` excludes `backlog`,
`/refine` is unavailable. (Today every shipping adaptor exposes
backlog; this gate is forward-compat.)

## What it is NOT

- Not a Kanban — refinement is not a flow stage.
- Not a sprint planner — that's `/sprints` (gm-e11.5).
- Not a coach surface — Selection / planner-coach lives in `/coach`
  and feeds suggestions in via the suggested-epic column.

## Implementation plan

Children (filed under gm-3ofd):

1. **Page scaffold + route + sidebar** — `RefinePage` renders the
   existing `WorkItemGrid` with hardcoded backlog filter, default
   sort, power features always on. `/backlog` redirect re-targets
   `/refine`. Sidebar entry between Plan and Review.
2. **Refine-specific columns** — age, suggested-epic, blockers
   count, dispatch_status. Adds rendering to `WorkItemGrid`'s
   column registry.
3. **Drop-into-epic action** — single-row + bulk. Uses the parent_id
   PATCH path (gm-gsbj).
4. **Defer action** — single-row + bulk. State stays backlog; a
   `defer_until:<iso>` label rides as the implementation.
5. **Dismiss action** — single-row + bulk. State → canceled with
   optional reason note.

Out of v1 scope (filed but deferred):

- Suggested-epic computation pipeline (Selection feeds this; the
  column renders whatever the WorkItem carries).
- Cross-workspace refinement.
