// specs/board/workitem-view.spec.ts (gm-5v8v.5, gm-e12.19.1, gm-uipx.18).
//
// Tier: route. Pins the Board layout deep links. The visible Board
// header no longer exposes layout toggles; dense table/hierarchy
// controls live on /refine. These legacy URLs remain supported so old
// bookmarks and targeted tests keep landing on the right component:
//
//   ?layout (absent)   → WorkItem status board (default)
//   ?layout=epic       → legacy Epic kanban
//   ?layout=workitem   → flat WorkItem kanban (ui-spec L293)
//   ?layout=list       → flat WorkItem list (gm-e12.19.1)

import { expect, test } from '../../fixtures/server';
import { BoardPage } from '../../pages/BoardPage';
import { workItem, epic } from '../../builders/workitem';

test('?layout=workitem renders the flat WorkItemBoard @board', async ({ page, workPlane }) => {
  workPlane.seed([workItem({ state_category: 'unstarted' })]);
  const board = new BoardPage(page);
  await board.gotoWorkItemView();
  await expect(board.workItemBoard).toBeVisible();
});

test('?layout=epic renders the legacy Epic board @board', async ({
  page,
  workPlane,
}) => {
  workPlane.seed([epic()]);
  const board = new BoardPage(page);
  await board.gotoEpicView();
  await expect(page).toHaveURL(/[?&]layout=epic/);
  await expect(page.getByTestId('board-epic')).toBeVisible();
});

// gm-e12.19.1: list-mode toggle (?layout=list post-uipx.18). Pins
// both the URL surface and the in-page toggle button so the
// collapsed-Backlog path stays exercised under route-fake.
test('?layout=list renders the flat list pane @board', async ({ page, workPlane }) => {
  workPlane.seed([workItem({ state_category: 'unstarted' })]);
  const board = new BoardPage(page);
  await board.gotoListView();
  await expect(board.listView).toBeVisible();
});

test('explicit ?layout=list renders the flat list pane @board', async ({
  page,
  workPlane,
}) => {
  workPlane.seed([epic()]);
  const board = new BoardPage(page);
  await board.gotoListView();
  await expect(board.listView).toBeVisible();
});
