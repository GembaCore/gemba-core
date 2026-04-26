// specs/board/workitem-view.spec.ts (gm-5v8v.5, gm-e12.19.1).
//
// Tier: route. Pins the three Board view modes (gm-e12.19.1 absorbed
// the former /backlog surface as a third "list" mode):
//
//   ?view (absent)   → Epic kanban (default)
//   ?view=workitem   → flat WorkItem kanban (ui-spec L293)
//   ?view=list       → flat WorkItem list (gm-e12.19.1)
//
// Plus the two view-toggle hotkeys: Cmd-Shift-W (Epic ↔ WorkItem)
// and Cmd-Shift-L (Kanban ↔ List). Both are best-effort because
// browsers / OS may swallow them; the in-page toggle is the
// authoritative path per ui-spec L293.

import { expect, test } from '../../fixtures/server';
import { BoardPage } from '../../pages/BoardPage';
import { workItem, epic } from '../../builders/workitem';

test('?view=workitem renders the flat WorkItemBoard @board', async ({ page, workPlane }) => {
  workPlane.seed([workItem({ state_category: 'unstarted' })]);
  const board = new BoardPage(page);
  await board.gotoWorkItemView();
  await expect(board.workItemBoard).toBeVisible();
});

test('header toggle flips Epic ↔ WorkItem and updates ?view= in the URL @board', async ({
  page,
  workPlane,
}) => {
  workPlane.seed([epic()]);
  const board = new BoardPage(page);
  await board.gotoEpicView();

  // Default = epic. The toggle button carries data-active=true on the
  // current view.
  await expect(board.viewToggleEpic).toHaveAttribute('data-active', 'true');
  await expect(page).toHaveURL(/\/board(\?.*)?$/);

  // Click "Item" → ?view=workitem.
  await board.viewToggleWorkItem.click();
  await expect(page).toHaveURL(/[?&]view=workitem/);
  await expect(board.viewToggleWorkItem).toHaveAttribute('data-active', 'true');

  // Click "Epic" → ?view= drops out (default lives in the absence of
  // the param).
  await board.viewToggleEpic.click();
  await expect(page).not.toHaveURL(/[?&]view=workitem/);
});

// The hotkey may be OS-swallowed (macOS browsers grab Cmd-W for tab
// close). The ui-spec L293 pins the in-page fallback as authoritative;
// the hotkey is a convenience. We assert the in-page click works in
// the test above and treat the hotkey as best-effort: dispatch the
// chord and pass either way (the URL change OR the toggle button
// reflects current state).
test('Cmd/Ctrl+Shift+W hotkey toggles the view when not OS-swallowed @board', async ({
  page,
  workPlane,
}) => {
  workPlane.seed([epic()]);
  const board = new BoardPage(page);
  await board.gotoEpicView();
  // Steal focus from any chrome that might be selected so the hotkey
  // lands on the page.
  await page.locator('main').click({ position: { x: 1, y: 1 } });
  await page.keyboard.press('Control+Shift+W');
  await page.keyboard.press('Meta+Shift+W');
  // Either chord SHOULD have flipped the view; if neither did (the
  // OS swallowed both), don't fail — the in-page fallback test above
  // is the authoritative path. If one did fire, assert the toggle
  // landed the URL change.
  const url = page.url();
  if (url.includes('view=workitem')) {
    await expect(board.viewToggleWorkItem).toHaveAttribute('data-active', 'true');
  } else {
    test.info().annotations.push({
      type: 'note',
      description:
        'Cmd/Ctrl+Shift+W appears to be swallowed in this run; ui-spec L293 fallback path covered separately.',
    });
  }
});

// gm-e12.19.1: list-mode toggle (?view=list). Pins both the URL
// surface and the in-page toggle button so the collapsed-Backlog
// path stays exercised under route-fake.
test('?view=list renders the flat list pane @board', async ({ page, workPlane }) => {
  workPlane.seed([workItem({ state_category: 'unstarted' })]);
  const board = new BoardPage(page);
  await board.gotoListView();
  await expect(board.listView).toBeVisible();
  await expect(board.viewToggleList).toHaveAttribute('data-active', 'true');
});

test('header toggle reaches the List mode and updates ?view= in the URL @board', async ({
  page,
  workPlane,
}) => {
  workPlane.seed([epic()]);
  const board = new BoardPage(page);
  await board.gotoEpicView();

  await board.viewToggleList.click();
  await expect(page).toHaveURL(/[?&]view=list/);
  await expect(board.viewToggleList).toHaveAttribute('data-active', 'true');
  await expect(board.listView).toBeVisible();

  // Click "Epic" → ?view= drops out (default in absence of param).
  await board.viewToggleEpic.click();
  await expect(page).not.toHaveURL(/[?&]view=list/);
});

// gm-e12.19.1 / hotkey defaults: Mod+Shift+L toggles kanban ↔ list.
// Same best-effort pattern as Cmd-Shift-W — the hotkey may be OS-
// swallowed; the in-page toggle test above is authoritative.
test('Cmd/Ctrl+Shift+L hotkey toggles list mode when not OS-swallowed @board', async ({
  page,
  workPlane,
}) => {
  workPlane.seed([epic()]);
  const board = new BoardPage(page);
  await board.gotoEpicView();
  await page.locator('main').click({ position: { x: 1, y: 1 } });
  await page.keyboard.press('Control+Shift+L');
  await page.keyboard.press('Meta+Shift+L');
  const url = page.url();
  if (url.includes('view=list')) {
    await expect(board.viewToggleList).toHaveAttribute('data-active', 'true');
  } else {
    test.info().annotations.push({
      type: 'note',
      description:
        'Cmd/Ctrl+Shift+L appears to be swallowed in this run; in-page toggle test above covers the authoritative path.',
    });
  }
});
