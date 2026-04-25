// specs/drawers/workitem-dod.spec.ts — gm-5v8v.8 (in progress)
//
// DoD tab: synthesized vs operator-authored banner; criteria list;
// reorder / remove / add; edit-save / cancel. The drawer is large
// and the DoD slice is its own concern — keeping these in their
// own spec keeps the file under the per-test budget.

import { test, expect } from '../../fixtures/server';
import { WorkItemDrawerPO } from '../../pages/WorkItemDrawer';
import * as build from '../../builders/workitem';

test.describe('WorkItemDrawer DoD tab @route', () => {
  test('renders informational banner when DoD tab is active', async ({ page, workPlane }) => {
    const id = 'gm-test-dod';
    workPlane.seed([
      build.workItem({
        id,
        dod: build.dod(['Tests pass', 'Linter clean']),
      }),
    ]);

    const drawer = new WorkItemDrawerPO(page);
    await drawer.openByDeepLink(id);
    await drawer.selectTab('dod');

    await expect(drawer.dodBanner).toBeVisible();
    await expect(page.getByTestId('work-item-dod-edit')).toBeVisible();
  });

  test.fixme('synthesized DoD shows "operator-authored" call-to-action', async () => {
    // Surface details unverified — needs a one-pass alongside the
    // synthesizedDoD() helper in the SPA.
  });

  test.fixme('add / remove / reorder criteria via inline editor', async () => {
    // Flip into edit mode then drive the per-criterion buttons with
    // their work-item-dod-criterion-N-up / -down / -remove testids.
  });

  test.fixme('save round-trip persists DoD @deep', async () => {
    // gm-5v8v.2.
  });
});
