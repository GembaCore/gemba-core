// specs/chrome/palette.spec.ts — gm-5v8v.4
//
// Global command palette (gm-e12.6). Cmd+K, the 'p' hotkey, and the
// Topbar pill all open the same dialog; cmdk filters items as the
// user types; Escape and clicking a result both close it.

import type { Page } from '@playwright/test';
import { test, expect } from '../../fixtures/server';

async function openPaletteFromTrigger(page: Page) {
  await page.locator('[data-hotkey-target="command-palette"]').click();
  await expect(page.getByTestId('command-palette-dialog')).toBeVisible();
}

test.describe('Command palette @chrome', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/board');
  });

  test('Mod+K opens the palette', async ({ page }) => {
    const dialog = page.getByTestId('command-palette-dialog');
    await expect(dialog).toBeHidden();

    // PaletteContext treats metaKey OR ctrlKey as the modifier. Use
    // Ctrl here because GitHub's Linux runner can drop Meta key
    // chords in headless Chromium.
    await page.keyboard.press('Control+K');
    await expect(dialog).toBeVisible();
  });

  test('the "p" hotkey opens the palette', async ({ page }) => {
    // 'p' fires the open-palette default hotkey, which clicks the
    // Topbar trigger via data-hotkey-target dispatch. Click somewhere
    // neutral first so no input has focus (hotkeys ignore typing).
    await page.locator('main').click();
    await page.keyboard.press('p');
    await expect(page.getByTestId('command-palette-dialog')).toBeVisible();
  });

  test('Escape closes the palette', async ({ page }) => {
    await openPaletteFromTrigger(page);
    const dialog = page.getByTestId('command-palette-dialog');
    await page.keyboard.press('Escape');
    await expect(dialog).toBeHidden();
  });

  test('Navigate group lists every primary route', async ({ page }) => {
    await openPaletteFromTrigger(page);
    const dialog = page.getByTestId('command-palette-dialog');

    // Backlog and Grid are learned aliases for Refine's table.
    for (const label of [
      'Board',
      'Backlog',
      'Grid',
      'Dependency graph',
      'Sessions',
      'Escalations',
      'Capabilities',
      'Insights',
    ]) {
      await expect(dialog.getByRole('option', { name: new RegExp(`^${label}$`) })).toBeVisible();
    }
  });

  test('selecting a Navigate item routes there and closes the palette', async ({ page }) => {
    await page.goto('/board');
    await openPaletteFromTrigger(page);
    const dialog = page.getByTestId('command-palette-dialog');

    await dialog.getByRole('option', { name: /^Backlog$/ }).click();

    await expect(page).toHaveURL(/\/refine$/);
    await expect(dialog).toBeHidden();
  });

  test('typing filters items via cmdk substring match', async ({ page }) => {
    await openPaletteFromTrigger(page);
    const dialog = page.getByTestId('command-palette-dialog');
    const input = page.getByTestId('command-palette-input');

    await input.fill('graph');

    // The Dependency-graph row matches; the unrelated Backlog row
    // gets filtered out. cmdk does the matching client-side off the
    // pre-loaded item set so this assertion is purely UI.
    await expect(
      dialog.getByRole('option', { name: /Dependency graph/ }),
    ).toBeVisible();
    await expect(dialog.getByRole('option', { name: /^Backlog$/ })).toHaveCount(0);
  });

  test('Cmd+K when open closes the palette', async ({ page }) => {
    const dialog = page.getByTestId('command-palette-dialog');
    await page.keyboard.press('Control+K');
    await expect(dialog).toBeVisible();
    await page.keyboard.press('Control+K');
    await expect(dialog).toBeHidden();
  });
});
