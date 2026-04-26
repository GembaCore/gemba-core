// specs/chrome/palette.spec.ts — gm-5v8v.4
//
// Global command palette (gm-e12.6). Cmd+K, the 'p' hotkey, and the
// Topbar pill all open the same dialog; cmdk filters items as the
// user types; Escape and clicking a result both close it.

import { test, expect } from '../../fixtures/server';

test.describe('Command palette @chrome', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/board');
  });

  test('Mod+K opens the palette', async ({ page }) => {
    const dialog = page.getByTestId('command-palette-dialog');
    await expect(dialog).toBeHidden();

    // Playwright honors `Meta+K` on macOS; PaletteContext treats
    // metaKey OR ctrlKey as the modifier so this works on every OS.
    await page.keyboard.press('Meta+K');
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
    await page.keyboard.press('Meta+K');
    const dialog = page.getByTestId('command-palette-dialog');
    await expect(dialog).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(dialog).toBeHidden();
  });

  test('Navigate group lists every primary route', async ({ page }) => {
    await page.keyboard.press('Meta+K');
    const dialog = page.getByTestId('command-palette-dialog');
    await expect(dialog).toBeVisible();

    for (const label of [
      'Board',
      'Backlog',
      'Grid',
      'Dependency graph',
      'Agents',
      'Sessions',
      'Escalations',
      'Capabilities',
      'Insights',
    ]) {
      await expect(dialog.getByRole('option', { name: new RegExp(`^${label}$`) })).toBeVisible();
    }
  });

  test('selecting a Navigate item routes there and closes the palette', async ({ page }) => {
    await page.goto('/grid');
    await page.keyboard.press('Meta+K');
    const dialog = page.getByTestId('command-palette-dialog');

    await dialog.getByRole('option', { name: /^Backlog$/ }).click();

    // gm-e12.19.1: Backlog palette item routes to Board's list mode +
    // Backlog preset (the collapsed surface).
    await expect(page).toHaveURL(/\/board\?view=list&preset=backlog$/);
    await expect(dialog).toBeHidden();
  });

  test('typing filters items via cmdk substring match', async ({ page }) => {
    await page.keyboard.press('Meta+K');
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
    await page.keyboard.press('Meta+K');
    await expect(dialog).toBeVisible();
    await page.keyboard.press('Meta+K');
    await expect(dialog).toBeHidden();
  });
});
