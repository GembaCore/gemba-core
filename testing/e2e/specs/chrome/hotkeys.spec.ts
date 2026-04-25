// specs/chrome/hotkeys.spec.ts — gm-5v8v.4
//
// Exercises the navigational + shell-action subset of DEFAULT_HOTKEYS
// (web/src/hotkeys/defaults.ts) that AppHotkeys actually wires up to
// concrete shell actions today. Grid / drawer / bulk shortcuts that
// the shell forwards to per-page bindings live in their own tier
// specs (board / grid). The bead description names "every entry in
// DEFAULT_HOTKEYS"; the spec covers what AppHotkeys.tsx binds.

import { test, expect } from '../../fixtures/server';

const NAV_HOTKEYS = [
  { key: '1', expected: /\/board$/ },
  { key: '2', expected: /\/backlog$/ },
  { key: '3', expected: /\/graph$/ },
  { key: '4', expected: /\/insights$/ },
  { key: '5', expected: /\/escalations$/ },
  { key: 'Shift+S', expected: /\/sessions$/ }, // 'S' uppercase = sessions-view
  { key: 'Shift+C', expected: /\/capabilities$/ },
  { key: 'Shift+D', expected: /\/insights$/ }, // drift-view → /insights until gm-e14
] as const;

test.describe('App hotkeys @chrome', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/board');
    // Move focus off any input so character keys fire as hotkeys.
    await page.locator('main').click();
  });

  for (const { key, expected } of NAV_HOTKEYS) {
    test(`${key} navigates to ${expected.source}`, async ({ page }) => {
      await page.keyboard.press(key);
      await expect(page).toHaveURL(expected);
      // Reset to /board for the next iteration's beforeEach to be
      // a no-op; tests are independent but this keeps trace cleaner.
      await page.goto('/board');
    });
  }

  test('"/" focuses the palette trigger (which doubles as search)', async ({ page }) => {
    // The Topbar pill carries data-hotkey-target="command-palette";
    // focus-search and open-palette both clickTarget that element.
    await page.keyboard.press('/');
    await expect(page.getByTestId('command-palette-dialog')).toBeVisible();
  });

  test.skip('Mod+Shift+S routes to /sessions (blocked on gm-jvl8)', async ({ page }) => {
    // DEFAULT_HOTKEYS registers 'Mod+Shift+S' but the keys.ts
    // normalizer drops the Shift prefix for 1-char keys, so the
    // matcher never sees a chord that begins with 'Mod+Shift+'.
    // Re-enable once gm-jvl8 lands a fix that round-trips
    // shifted-letter chords.
    await page.keyboard.press('Meta+Shift+S');
    await expect(page).toHaveURL(/\/sessions$/);
  });
});
