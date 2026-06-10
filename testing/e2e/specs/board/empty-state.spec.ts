// specs/board/empty-state.spec.ts (gm-5v8v.5).
//
// Tier: route. The empty state is what an operator sees on a fresh
// install — a regression here is a "first-five-minutes" failure.

import { expect, test } from '../../fixtures/server';
import { BoardPage } from '../../pages/BoardPage';

test('board renders the empty state when the WorkPlane has nothing to draw @board', async ({
  page,
  workPlane,
}) => {
  workPlane.seed([]);
  const board = new BoardPage(page);
  await board.gotoEpicView();
  await expect(board.empty).toBeVisible();
  // The CTA copy is "New work item" today (BoardPage.tsx EmptyState).
  // The bead's spec wishlist used "Start bootstrap" — match what's in
  // the SPA, not the wishlist. If the copy lands later, update both.
  await expect(board.empty.getByRole('button', { name: /New work item/i })).toBeVisible();
});

test('clicking the empty-state CTA opens the New WorkItem dialog @board', async ({
  page,
  workPlane,
}) => {
  workPlane.seed([]);
  const board = new BoardPage(page);
  await board.gotoEpicView();
  await board.empty.getByRole('button', { name: /New work item/i }).click();
  // The NewWorkItemDialog is a Radix dialog; its accessible label is
  // 'New work item'. Asserting on role+name keeps the spec from
  // coupling to the dialog component's testid (which gm-e12.10 owns
  // and may rename).
  await expect(page.getByRole('dialog', { name: /New work item/i })).toBeVisible();
});
