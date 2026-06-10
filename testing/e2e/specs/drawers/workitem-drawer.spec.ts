// specs/drawers/workitem-drawer.spec.ts — gm-root.22.5
//
// Was: WorkItemDrawer spec (gm-5v8v.8).
// Now: WorkItemDetail in the RHP (gm-root.22.5).
//
// Covers: open via ?bead= legacy deep-link (shim), open via ?rhp= codec,
// header (id / copy), tab navigation, dispatch button, error state,
// back button, card-click → RHP.

import { test, expect } from '../../fixtures/server';
import { WorkItemDetailPO } from '../../pages/WorkItemDetail';
import * as build from '../../builders/workitem';

test.describe('WorkItemDetail (RHP) @route', () => {
  test('opens via legacy /board?bead=ID — shim translates to ?rhp=workitem:ID', async ({
    page,
    workPlane,
  }) => {
    const id = 'gm-test-wi';
    workPlane.seed([build.workItem({ id, title: 'Hello bead' })]);

    const detail = new WorkItemDetailPO(page);
    await detail.openByLegacyDeepLink(id);

    await detail.expectOpenWith(id);
    // URL should have been rewritten — ?bead= gone, ?rhp= present.
    await expect(page).toHaveURL(/rhp=workitem/);
    await expect(page).not.toHaveURL(/bead=/);
  });

  test('opens via canonical /board?rhp=workitem:ID', async ({ page, workPlane }) => {
    const id = 'gm-rhp-direct';
    workPlane.seed([build.workItem({ id, title: 'Direct RHP open' })]);

    const detail = new WorkItemDetailPO(page);
    await detail.openByRhpDeepLink(id);

    await detail.expectOpenWith(id);
  });

  test('opens with id containing slashes (workspace-prefixed)', async ({
    page,
    workPlane,
  }) => {
    const id = 'gemba/gemba/gm-1';
    workPlane.seed([build.workItem({ id, title: 'Workspace-prefixed' })]);

    const detail = new WorkItemDetailPO(page);
    await detail.openByRhpDeepLink(id);

    await detail.expectOpenWith(id);
  });

  test('renders the tablist with the standard five tabs', async ({ page, workPlane }) => {
    const id = 'gm-test-tabs';
    workPlane.seed([build.workItem({ id })]);

    const detail = new WorkItemDetailPO(page);
    await detail.openByRhpDeepLink(id);

    await expect(detail.tabs).toBeVisible();
    for (const tab of ['description', 'edges', 'dod', 'sprint', 'activity'] as const) {
      await expect(detail.tab(tab)).toBeVisible();
    }
  });

  test('tab clicks update aria-selected', async ({ page, workPlane }) => {
    const id = 'gm-test-tab-switch';
    workPlane.seed([build.workItem({ id })]);

    const detail = new WorkItemDetailPO(page);
    await detail.openByRhpDeepLink(id);

    await detail.selectTab('dod');
    await detail.selectTab('edges');
    await detail.selectTab('description');
  });

  test('dispatch button is disabled for terminal beads', async ({ page, workPlane }) => {
    const id = 'gm-test-completed';
    workPlane.seed([
      build.workItem({ id, state_category: 'completed', status: 'closed' }),
    ]);

    const detail = new WorkItemDetailPO(page);
    await detail.openByRhpDeepLink(id);

    await expect(detail.dispatch).toBeDisabled();
  });

  test('error state renders when bead is not in the WorkPlane', async ({
    page,
    workPlane,
  }) => {
    workPlane.seed([]);

    const detail = new WorkItemDetailPO(page);
    await page.goto('/board?rhp=workitem:gm-missing');

    // RHP body is visible; error pane lives inside.
    await expect(detail.rhpBody).toBeVisible();
    await expect(page.getByTestId('workitem-detail-error')).toBeVisible();
  });

  test('back button restores the previous bead in the nav stack', async ({
    page,
    workPlane,
  }) => {
    const a = build.workItem({
      id: 'gm-stack-a',
      title: 'Source bead',
      relationships: [build.relationship('blocks', 'gm-stack-a', 'gm-stack-b')],
    });
    const b = build.workItem({ id: 'gm-stack-b', title: 'Target bead' });
    workPlane.seed([a, b]);

    const detail = new WorkItemDetailPO(page);
    await detail.openByRhpDeepLink('gm-stack-a');
    await detail.expectOpenWith('gm-stack-a');
    await expect(detail.backButton).toHaveCount(0);

    await detail.selectTab('edges');
    await page
      .getByTestId('relgroup-blocks')
      .getByRole('button', { name: 'gm-stack-b' })
      .click();
    await detail.expectOpenWith('gm-stack-b');
    await expect(detail.backButton).toBeVisible();

    await detail.backButton.click();
    await detail.expectOpenWith('gm-stack-a');
    await expect(detail.backButton).toHaveCount(0);
  });

  test('card click on the board opens the RHP workitem detail', async ({
    page,
    workPlane,
  }) => {
    const id = 'gm-card-click';
    workPlane.seed([build.workItem({ id, title: 'Card click target' })]);

    const detail = new WorkItemDetailPO(page);
    await page.goto('/board?layout=workitem');

    // Card carries data-work-item-id; click it.
    const card = page.locator(`[data-work-item-id="${id}"]`);
    await expect(card).toBeVisible();
    await card.click();

    await detail.expectOpenWith(id);
  });

  test('opens via the "o" hotkey on the focused card (gm-fqiw)', async ({
    page,
    workPlane,
  }) => {
    const id = 'gm-hot-o';
    workPlane.seed([build.workItem({ id, title: 'Hotkey o target' })]);

    const detail = new WorkItemDetailPO(page);
    await page.goto('/board?layout=workitem');
    const card = page.locator(`[data-work-item-id="${id}"]`);
    await expect(card).toBeVisible();
    await card.focus();
    await page.keyboard.press('o');
    await detail.expectOpenWith(id);
  });
});
