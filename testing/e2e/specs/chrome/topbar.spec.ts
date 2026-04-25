// specs/chrome/topbar.spec.ts — gm-5v8v.4
//
// Topbar surface: workspace switcher pill, command-palette trigger
// (with ⌘K hint), theme toggle, user menu. The bead description also
// names a mode pill / phase pill / budget gauge / search trigger /
// PM panel toggle — none of those exist in Topbar.tsx today, so the
// spec asserts the surface that actually ships and leaves the rest
// to a follow-up bead when the components land.

import { test, expect } from '../../fixtures/server';

test.describe('Topbar @chrome', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/board');
  });

  test('renders workspace switcher with default label', async ({ page }) => {
    const switcher = page.locator('[data-hotkey-target="workspace-switcher"]');
    await expect(switcher).toBeVisible();
    await expect(switcher).toContainText('default');
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

  test('theme toggle is reachable by aria-label', async ({ page }) => {
    await expect(page.getByRole('button', { name: 'Toggle theme' })).toBeVisible();
  });

  test('user menu button is reachable by aria-label', async ({ page }) => {
    await expect(page.getByRole('button', { name: 'User menu' })).toBeVisible();
  });
});
