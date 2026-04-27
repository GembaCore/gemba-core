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
  // gm-e12.19.1 / gm-uipx.18: '2' goes straight to Board's list
  // layout + Backlog named view (the collapsed surface). The
  // legacy ?view=list&preset=backlog deep-link migrates on first
  // paint so existing keyboard muscle memory keeps working.
  { key: '2', expected: /\/board\?layout=list&view=backlog$/ },
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

  test('Mod+Shift+S routes to /sessions', async ({ page }) => {
    // gm-jvl8: the matcher now folds Mod+Shift+<letter> chords via
    // canonicalChord so the registered 'Mod+Shift+S' matches the
    // normalized 'Mod+S' from a Cmd-Shift-S keystroke.
    await page.keyboard.press('Meta+Shift+S');
    await expect(page).toHaveURL(/\/sessions$/);
  });
});
