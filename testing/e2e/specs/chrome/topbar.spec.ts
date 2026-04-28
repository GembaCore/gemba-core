// specs/chrome/topbar.spec.ts — gm-isxs (refresh after gm-uipx.13)
//
// Topbar surface: workspace switcher pill, command-palette trigger
// (with ⌘K hint), PM-panel toggle, theme toggle, user menu. The
// in-flight / parallelism pill (GlobalInFlightCounter) renders only
// when at least one parallel-capable session is active, so the spec
// asserts on its container slot rather than a forced presence — the
// fake backend has no sessions, so the pill itself is intentionally
// absent. A live-data backend exercises the pill's render in
// realtime-fake.

import { test, expect } from '../../fixtures/server';

test.describe('Topbar @chrome', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/board');
  });

  test('renders workspace switcher button with a label', async ({ page }) => {
    // Workspace label is server-fed via /api/config (BeadsSource.label).
    // Under fake mode the catchall returns {}, so the SPA paints the
    // placeholder ellipsis "…" — assert the *element* is visible and
    // carries non-empty text rather than pinning a specific string,
    // which churns when the fake backend gains real /api/config
    // support. The data-testid is the SPA's stable handle.
    const switcher = page.locator('[data-hotkey-target="workspace-switcher"]');
    await expect(switcher).toBeVisible();
    await expect(switcher).toHaveAttribute('data-testid', 'workspace-label');
    // Non-empty text — either '…' (placeholder) or a real workspace name.
    await expect(switcher).not.toHaveText('');
  });

  test('renders the command-palette trigger with ⌘K hint', async ({ page }) => {
    const trigger = page.locator('[data-hotkey-target="command-palette"]');
    await expect(trigger).toBeVisible();
    await expect(trigger).toHaveAttribute('aria-label', 'Open command palette');
    await expect(trigger.locator('kbd')).toContainText('⌘K');
  });

  test('clicking the palette trigger opens the dialog', async ({ page }) => {
    await page.locator('[data-hotkey-target="command-palette"]').click();
    await expect(page.getByTestId('command-palette-dialog')).toBeVisible();
  });

  test('PM panel toggle button is reachable by aria-label', async ({ page }) => {
    // gm-uipx.13: the PM panel toggle moved off the topbar entry
    // path (a chevron handle now lives on the right edge), but the
    // topbar button is retained as a back-compat affordance.
    await expect(page.getByRole('button', { name: 'Toggle PM panel' })).toBeVisible();
  });

  test('theme toggle is reachable by aria-label', async ({ page }) => {
    await expect(page.getByRole('button', { name: 'Toggle theme' })).toBeVisible();
  });

  test('user menu button is reachable by aria-label', async ({ page }) => {
    await expect(page.getByRole('button', { name: 'User menu' })).toBeVisible();
  });

  test('parallelism pill is absent when no sessions are in flight', async ({ page }) => {
    // gm-root.16.2: GlobalInFlightCounter renders nothing when
    // total in-flight is zero. The fake backend has no sessions
    // so the pill should be hidden — a quick contract check that
    // the empty-state branch wins.
    await expect(page.getByTestId('global-in-flight')).toHaveCount(0);
  });
});
