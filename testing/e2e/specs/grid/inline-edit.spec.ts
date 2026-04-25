// specs/grid/inline-edit.spec.ts (gm-5v8v.6, gm-5v8v.6.1).
//
// Tier: route. Inline cell editing on /grid landed in gm-5v8v.6.1 —
// title / priority / state cells become editors on click. Esc cancels
// without firing PATCH; Enter / change commits via the existing
// updateWorkItem helper, which mints a fresh X-GEMBA-Confirm nonce
// per call.

import { expect, test } from '../../fixtures/server';
import { workItem } from '../../builders/workitem';

test.describe('WorkItemGrid inline edit @route', () => {
  test('click-to-edit activates a cell editor', async ({ page, workPlane }) => {
    workPlane.seed([workItem({ id: 'gm-1', title: 'first' })]);
    await page.goto('/grid');
    await expect(page.getByTestId('work-item-grid')).toBeVisible();
    // No editor yet.
    await expect(page.getByTestId('grid-cell-editor-title')).toHaveCount(0);
    await page.getByTestId('grid-cell-gm-1-title').click();
    await expect(page.getByTestId('grid-cell-editor-title')).toBeVisible();
    await expect(page.getByTestId('grid-cell-editor-title')).toHaveValue('first');
  });

  test('Escape cancels the in-flight edit without firing PATCH', async ({
    page,
    workPlane,
  }) => {
    workPlane.seed([workItem({ id: 'gm-1', title: 'unchanged' })]);
    await page.goto('/grid');
    await expect(page.getByTestId('work-item-grid')).toBeVisible();
    await page.getByTestId('grid-cell-gm-1-title').click();

    // Watch for any PATCH against this id; if Esc lets one through
    // we'll observe it and the test fails on the assertion below.
    let sawPatch = false;
    page.on('request', (req) => {
      if (req.method() === 'PATCH' && req.url().endsWith('/api/work-items/gm-1')) {
        sawPatch = true;
      }
    });

    const editor = page.getByTestId('grid-cell-editor-title');
    await editor.fill('about to bail');
    await editor.press('Escape');
    await expect(page.getByTestId('grid-cell-editor-title')).toHaveCount(0);
    // Give the network a moment to be sure no PATCH is in flight.
    await page.waitForTimeout(150);
    expect(sawPatch).toBe(false);
  });

  test('Enter commits and fires PATCH /api/work-items/:id with a fresh nonce', async ({
    page,
    workPlane,
  }) => {
    workPlane.seed([workItem({ id: 'gm-1', title: 'first' })]);
    await page.goto('/grid');
    await expect(page.getByTestId('work-item-grid')).toBeVisible();
    await page.getByTestId('grid-cell-gm-1-title').click();

    const [patchReq] = await Promise.all([
      page.waitForRequest(
        (req) => req.method() === 'PATCH' && req.url().endsWith('/api/work-items/gm-1')
      ),
      (async () => {
        const editor = page.getByTestId('grid-cell-editor-title');
        await editor.fill('renamed');
        await editor.press('Enter');
      })(),
    ]);
    expect(patchReq.postDataJSON()).toEqual({ title: 'renamed' });
    // Per-call nonce uniqueness is the api/workItems helper's
    // contract; pin its presence here so a refactor that drops it
    // breaks the contract loudly.
    expect(patchReq.headers()['x-gemba-confirm']).toBeTruthy();
  });

  test('priority cell PATCHes priority on Enter', async ({ page, workPlane }) => {
    workPlane.seed([workItem({ id: 'gm-1', priority: 1 })]);
    await page.goto('/grid');
    await expect(page.getByTestId('work-item-grid')).toBeVisible();
    await page.getByTestId('grid-cell-gm-1-priority').click();

    const [patchReq] = await Promise.all([
      page.waitForRequest(
        (req) => req.method() === 'PATCH' && req.url().endsWith('/api/work-items/gm-1')
      ),
      (async () => {
        const editor = page.getByTestId('grid-cell-editor-priority');
        await editor.fill('3');
        await editor.press('Enter');
      })(),
    ]);
    expect(patchReq.postDataJSON()).toEqual({ priority: 3 });
  });

  test('state cell select PATCHes state_category on change', async ({ page, workPlane }) => {
    workPlane.seed([workItem({ id: 'gm-1', state_category: 'unstarted' })]);
    await page.goto('/grid');
    await expect(page.getByTestId('work-item-grid')).toBeVisible();
    await page.getByTestId('grid-cell-gm-1-state').click();

    const [patchReq] = await Promise.all([
      page.waitForRequest(
        (req) => req.method() === 'PATCH' && req.url().endsWith('/api/work-items/gm-1')
      ),
      page.getByTestId('grid-cell-editor-state').selectOption('started'),
    ]);
    expect(patchReq.postDataJSON()).toEqual({ state_category: 'started' });
  });
});
