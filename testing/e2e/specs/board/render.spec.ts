// specs/board/render.spec.ts (gm-5v8v.5).
//
// Tier: route. Pins the board's structural rendering: column order,
// per-column counts, empty / loading / error states, and the swimlane
// switcher's modes that the UI actually ships today.

import { expect, test } from '../../fixtures/server';
import { BoardPage, COLUMN_ORDER } from '../../pages/BoardPage';
import { workItem } from '../../builders/workitem';
import type { StateCategory } from '../../../../web/src/types/core.gen';

function inColumn(cat: StateCategory, n: number) {
  return Array.from({ length: n }, () => workItem({ state_category: cat }));
}

test('flat board renders collapsed display columns in the canonical order @board', async ({
  page,
  workPlane,
}) => {
  // Seed at least one bead so the flat board (not the empty-state)
  // mounts. state_category doesn't matter for the order assertion.
  workPlane.seed([workItem({ state_category: 'unstarted' })]);

  const board = new BoardPage(page);
  // gm-5ekd: Backlog hidden by default; opt-in to keep six-column
  // contract this spec pins.
  await board.gotoWorkItemView({ showBacklog: true });
  await board.expectColumnOrder();
});

test('column counts reflect seeded state_category distribution @board', async ({
  page,
  workPlane,
}) => {
  workPlane.seed([
    ...inColumn('backlog', 3),
    ...inColumn('unstarted', 2),
    ...inColumn('staged', 1),
    ...inColumn('started', 4),
    ...inColumn('completed', 1),
  ]);
  const board = new BoardPage(page);
  // gm-5ekd: opt into the Backlog column so the seeded backlog beads
  // surface for the count assertion.
  await board.gotoWorkItemView({ showBacklog: true });
  await board.expectColumnCount('backlog', 3);
  // Ready is a display lane: it combines unstarted + staged beads.
  await board.expectColumnCount('ready', 3);
  await board.expectColumnCount('started', 4);
  await board.expectColumnCount('completed', 1);
});

test('empty board renders the New work item CTA @board', async ({ page, workPlane }) => {
  // No seed → empty-state renders.
  workPlane.seed([]);
  const board = new BoardPage(page);
  await board.gotoEpicView();
  await expect(board.empty).toBeVisible();
  // The CTA inside the empty state opens the New WorkItem dialog. We
  // don't assert on dialog open here — that's empty-state.spec.ts.
  await expect(board.empty.getByRole('button', { name: /New work item/i })).toBeVisible();
});

test('every column data-testid has a stable selector @board', async ({ page, workPlane }) => {
  // Defensive: a refactor that drops `board-column-*` testids would
  // silently break every other spec in this file. Pin the contract
  // explicitly so a regression points at this test, not at a
  // mysterious "selector not found" further down the suite.
  workPlane.seed([workItem({ state_category: 'started' })]);
  const board = new BoardPage(page);
  // gm-5ekd: opt-in to keep the Backlog column in the rendered set.
  await board.gotoWorkItemView({ showBacklog: true });
  for (const cat of COLUMN_ORDER) {
    await expect(board.column(cat)).toBeVisible();
  }
});

// gm-uekk superseded the swimlane-mode dropdown with the ScopePicker
// (a single pill that names the active board scope: All / a root
// epic / a direct child epic). The old test asserted that the
// swimlane switcher exposed three modes (`by-parent-epic`, `by-label`,
// `none`); those modes are no longer user-facing — `by-parent-epic`
// is the only swimlane shape rendered today, gated by scope.
test.fixme(
  'Epic view exposes the swimlane switcher with implemented modes @board',
  () => {
    /* fixme: gm-uekk replaced the swimlane-mode dropdown with the
       ScopePicker (board-scope-picker / board-scope-trigger). A
       follow-up should rewrite this assertion against the new
       picker's option list — or drop it entirely if the modes are
       no longer a contract worth pinning. */
  }
);

// gm-5v8v.5 follow-up: the bead lists these features but they aren't
// in the SPA yet. Each fixme references the bead that would unblock
// it so a future contributor can flip these into real specs without
// re-deriving the design.
test.fixme(
  'WIP-limit yellow row highlights the In Progress column when over capacity @board',
  () => {
    /* fixme: WIP limits not yet in BoardPage.tsx. Filed under gm-5v8v
       follow-up; the column header's count badge would gain a colour
       once the limit is exceeded. */
  }
);

test.fixme(
  'swimlane "by-parallel-group" partitions cards by parallel-group label @board',
  () => {
    /* fixme: SwimlaneMode currently exposes by-parent-epic / by-label /
       none. The bead's "by-parallel-group" mode lands when the SPA
       grows the parallel-group label convention. */
  }
);
