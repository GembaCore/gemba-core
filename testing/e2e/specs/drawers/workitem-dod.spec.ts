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

  test('synthesized DoD shows the "edit to override" banner (gm-xbqw)', async ({
    page,
    workPlane,
  }) => {
    const id = 'gm-dod-synth';
    workPlane.seed([
      build.workItem({
        id,
        // gm-native.11's dod.Synthesize stamps Version="synthesized-v1";
        // the SPA banner reads dod.version.startsWith('synthesized').
        dod: build.dod(['code pushed; CI green'], { version: 'synthesized-v1' }),
      }),
    ]);

    const drawer = new WorkItemDrawerPO(page);
    await drawer.openByDeepLink(id);
    await drawer.selectTab('dod');

    const banner = page.getByTestId('work-item-dod-synth-banner');
    await expect(banner).toBeVisible();
    await expect(banner).toHaveAttribute('data-state', 'synthesized');
    await expect(banner).toContainText('Synthesized');
    await expect(banner).toContainText('Edit');
  });

  test('missing DoD shows the "no DoD authored" banner (gm-xbqw)', async ({
    page,
    workPlane,
  }) => {
    const id = 'gm-dod-missing';
    workPlane.seed([build.workItem({ id, dod: undefined })]);

    const drawer = new WorkItemDrawerPO(page);
    await drawer.openByDeepLink(id);
    await drawer.selectTab('dod');

    const banner = page.getByTestId('work-item-dod-synth-banner');
    await expect(banner).toBeVisible();
    await expect(banner).toHaveAttribute('data-state', 'missing');
  });

  test('operator-authored DoD does NOT show the synth banner (gm-xbqw)', async ({
    page,
    workPlane,
  }) => {
    const id = 'gm-dod-authored';
    workPlane.seed([
      build.workItem({
        id,
        dod: build.dod(['operator wrote this'], { version: 'v1' }),
      }),
    ]);

    const drawer = new WorkItemDrawerPO(page);
    await drawer.openByDeepLink(id);
    await drawer.selectTab('dod');

    await expect(page.getByTestId('work-item-dod-synth-banner')).toHaveCount(0);
  });

  test.fixme('save round-trip persists DoD @deep', async () => {
    // gm-5v8v.2 — the fake-mode PATCH handler echoes the existing
    // item back without applying the patch, so it can't catch
    // wire-shape drift on the DoD payload.
  });
});
