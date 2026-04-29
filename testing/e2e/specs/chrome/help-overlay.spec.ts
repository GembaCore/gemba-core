// specs/chrome/help-overlay.spec.ts — gm-5v8v.4
//
// HelpOverlay (web/src/hotkeys/HelpOverlay.tsx) is the discovery
// surface: '?' opens a modal listing every hotkey grouped by
// category. Esc closes; clicking the backdrop closes; the Esc-button
// inside the dialog closes.

import { test, expect } from '../../fixtures/server';

test.describe('Help overlay @chrome', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/board');
    await page.locator('main').click();
  });

  test('? opens the keyboard-shortcut overlay', async ({ page }) => {
    await page.keyboard.press('Shift+?');
    const dialog = page.getByRole('dialog', { name: 'Keyboard shortcuts' });
    await expect(dialog).toBeVisible();
  });

  test('overlay groups by category and lists hotkey rows', async ({ page }) => {
    await page.keyboard.press('Shift+?');
    const dialog = page.getByRole('dialog', { name: 'Keyboard shortcuts' });

    // CATEGORY_LABEL from HelpOverlay.tsx — at least one section per
    // populated category. The overlay omits an empty group, but
    // DEFAULT_HOTKEYS currently fills every category. gm-root.22.8
    // shortened the 'drawer' category label from "Drawer & panel"
    // to "Panel" when the legacy drawers were retired in favor of
    // the unified RHP detail-tab surface.
    for (const label of ['Navigation', 'Selection', 'Bulk', 'Create', 'Views', 'Panel', 'Help']) {
      await expect(dialog.getByRole('heading', { name: label })).toBeVisible();
    }

    // Sanity-check a specific known binding renders so a future
    // refactor that drops a hotkey doesn't slip through unnoticed.
    await expect(dialog.getByText('Open command palette')).toBeVisible();
    await expect(dialog.getByText('Show all shortcuts')).toBeVisible();
  });

  test('Escape closes the overlay', async ({ page }) => {
    await page.keyboard.press('Shift+?');
    const dialog = page.getByRole('dialog', { name: 'Keyboard shortcuts' });
    await expect(dialog).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(dialog).toBeHidden();
  });

  test('clicking the backdrop closes the overlay', async ({ page }) => {
    await page.keyboard.press('Shift+?');
    const dialog = page.getByRole('dialog', { name: 'Keyboard shortcuts' });
    await expect(dialog).toBeVisible();
    // Backdrop receives the click outside the dialog content; the
    // overlay's onClick on the outermost div closes when the target
    // is the backdrop itself.
    const box = await dialog.boundingBox();
    if (!box) throw new Error('overlay had no bounding box');
    await page.mouse.click(box.x + 5, box.y + 5);
    await expect(dialog).toBeHidden();
  });
});
