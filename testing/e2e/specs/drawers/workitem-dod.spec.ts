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

  test('edit mode lets the operator add / remove / reorder criteria', async ({
    page,
    workPlane,
  }) => {
    const id = 'gm-test-dod-edit';
    workPlane.seed([
      build.workItem({
        id,
        dod: build.dod(['First', 'Second', 'Third']),
      }),
    ]);

    const drawer = new WorkItemDrawerPO(page);
    await drawer.openByDeepLink(id);
    await drawer.selectTab('dod');

    // Enter edit mode and confirm the criteria list mounts the
    // per-row controls.
    await page.getByTestId('work-item-dod-edit').click();
    await expect(page.getByTestId('work-item-dod-editing')).toBeVisible();
    await expect(page.getByTestId('work-item-dod-criterion-0')).toBeVisible();
    await expect(page.getByTestId('work-item-dod-criterion-1')).toBeVisible();
    await expect(page.getByTestId('work-item-dod-criterion-2')).toBeVisible();

    // Add a new criterion → row 3 mounts.
    await page.getByTestId('work-item-dod-add-criterion').click();
    await expect(page.getByTestId('work-item-dod-criterion-3')).toBeVisible();

    // Remove row 0 → the prior row 3 collapses into row 2.
    await page.getByTestId('work-item-dod-criterion-0-remove').click();
    await expect(page.getByTestId('work-item-dod-criterion-3')).toHaveCount(0);
    await expect(page.getByTestId('work-item-dod-criterion-0')).toBeVisible();

    // Reorder: top of list cannot move further up; the down arrow
    // works on row 0 (now showing what was originally "Second").
    await expect(page.getByTestId('work-item-dod-criterion-0-up')).toBeDisabled();
    await page.getByTestId('work-item-dod-criterion-0-down').click();
  });

  test.fixme(
    'synthesized DoD shows "operator-authored" call-to-action',
    async () => {
      // The bead names a synthesized vs operator-authored banner;
      // the SPA today only renders the informational DoDBanner
      // (gating-vs-documentation). When the SPA grows the synth path,
      // this fixme lifts.
    }
  );

  test.fixme('save round-trip persists DoD @deep', async () => {
    // gm-5v8v.2 — the fake-mode PATCH handler echoes the existing
    // item back without applying the patch, so it can't catch
    // wire-shape drift on the DoD payload.
  });
});
