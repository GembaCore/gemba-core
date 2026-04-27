// specs/grid/saved-views.spec.ts — gm-5v8v.6
//
// Column presets (gm-e12.3.3): the Presets menu lists built-in
// (Default / Compact) plus user-saved entries kept in localStorage
// under PRESETS_STORAGE_KEY. Saving uses window.prompt; we override
// it via page.evaluate before the click so the spec is non-blocking.

import { test, expect } from '../../fixtures/server';
import { GridPage } from '../../pages/GridPage';
import * as build from '../../builders/workitem';

test.describe('Grid column presets @route', () => {
  test('built-in Default and Compact presets are listed', async ({ page, workPlane }) => {
    workPlane.seed([build.workItem({ id: 'gm-p-1' })]);

    const grid = new GridPage(page);
    await grid.goto();
    await grid.openPresetsMenu();

    await expect(page.getByTestId('grid-preset-default')).toBeVisible();
    await expect(page.getByTestId('grid-preset-compact')).toBeVisible();
  });

  test('applying Compact hides the labels column from the columns menu', async ({
    page,
    workPlane,
  }) => {
    workPlane.seed([build.workItem({ id: 'gm-p-2' })]);

    const grid = new GridPage(page);
    await grid.goto();

    // Default has all columns visible; sanity-check the labels box
    // is checked first so we know the toggle is meaningful.
    await grid.openColumnsMenu();
    await expect(page.getByTestId('grid-column-labels')).toBeChecked();
    // Close the columns menu by toggling so the presets menu
    // controls don't fight for click target.
    await grid.columnsToggle.click();

    await grid.applyPreset('compact');
    await grid.openColumnsMenu();
    await expect(page.getByTestId('grid-column-labels')).not.toBeChecked();
  });

  test('saving a user preset persists it in localStorage and shows it in the menu', async ({
    page,
    workPlane,
  }) => {
    workPlane.seed([build.workItem({ id: 'gm-p-3' })]);

    const grid = new GridPage(page);
    await grid.goto();

    // Stub window.prompt before clicking Save — the GridPage hands a
    // raw window.prompt to WorkItemGrid; the dialog is blocking when
    // not stubbed and would hang the test.
    await page.addInitScript(() => {
      // Marker so a flake in the override reads as "prompt missed",
      // not a generic hang.
      window.prompt = () => 'My personal preset';
    });
    // The init script registered for future navigations doesn't apply
    // to the page we already loaded; reload so the override takes hold
    // before WorkItemGrid mounts.
    await page.reload();
    await expect(grid.grid).toBeVisible();

    await grid.openPresetsMenu();
    await grid.presetsSave.click();
    // The save target writes to localStorage via the WorkItemGrid
    // helper. Re-open the menu (it auto-closes on save? — no, just
    // assert the new entry exists).
    const userPresetCards = page.locator('[data-testid^="grid-preset-user:"]');
    await expect(userPresetCards.first()).toBeVisible();
    await expect(userPresetCards.first()).toContainText('My personal preset');

    // The preset round-trips through localStorage: reload the page
    // and confirm the card is still there.
    await page.reload();
    await expect(grid.grid).toBeVisible();
    await grid.openPresetsMenu();
    await expect(page.locator('[data-testid^="grid-preset-user:"]').first()).toContainText(
      'My personal preset'
    );
  });

  test('deleting a user preset removes it from the menu', async ({ page, workPlane }) => {
    workPlane.seed([build.workItem({ id: 'gm-p-4' })]);

    // Pre-seed localStorage with a user preset so the spec doesn't
    // depend on the prompt path.
    await page.addInitScript(() => {
      const preset = {
        id: 'user:test:1',
        name: 'Doomed preset',
        visibility: { id: true, title: true, kind: true, state: true },
      };
      window.localStorage.setItem(
        'gemba.board.column-presets',
        JSON.stringify([preset])
      );
    });

    const grid = new GridPage(page);
    await grid.goto();
    await grid.openPresetsMenu();
    const card = page.getByTestId('grid-preset-user:test:1');
    await expect(card).toBeVisible();

    await page.getByTestId('grid-preset-delete-user:test:1').click();
    await expect(card).toBeHidden();
  });
});
