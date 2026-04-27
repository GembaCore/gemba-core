// specs/chrome/pm-panel.spec.ts — gm-uipx.12
//
// PM panel (ui-spec §2.4) — a global bottom drawer summoned by the
// Topbar pm-panel-toggle button or Cmd-Shift-P. Persists open state
// across reloads; Escape collapses without losing conversation;
// header has persona dropdown + cost display + close button.
//
// Lives in chrome/ because the panel is global chrome (mounted at
// the AppShell layer, not per-route). The route doesn't matter so
// this suite lands on /board.

import { test, expect } from '../../fixtures/server';

test.describe('PM panel @chrome', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/board');
    // Pristine localStorage so the open-state default is reproducible
    // across runs. Topbar Toggle is the source of truth for v1.
    await page.evaluate(() => window.localStorage.clear());
    await page.reload();
  });

  test('Topbar toggle button opens and closes the panel', async ({ page }) => {
    const toggle = page.getByTestId('pm-panel-toggle');
    await expect(toggle).toBeVisible();
    await expect(page.getByTestId('pm-panel')).toBeHidden();

    await toggle.click();
    await expect(page.getByTestId('pm-panel')).toBeVisible();
    await expect(toggle).toHaveAttribute('aria-pressed', 'true');

    await toggle.click();
    await expect(page.getByTestId('pm-panel')).toBeHidden();
    await expect(toggle).toHaveAttribute('aria-pressed', 'false');
  });

  test('open state persists across page reloads', async ({ page }) => {
    await page.getByTestId('pm-panel-toggle').click();
    await expect(page.getByTestId('pm-panel')).toBeVisible();

    await page.reload();
    await expect(page.getByTestId('pm-panel')).toBeVisible();
  });

  test('Escape collapses the panel', async ({ page }) => {
    await page.getByTestId('pm-panel-toggle').click();
    await expect(page.getByTestId('pm-panel')).toBeVisible();

    await page.locator('main').click();
    await page.keyboard.press('Escape');
    await expect(page.getByTestId('pm-panel')).toBeHidden();
  });

  test('close button collapses the panel', async ({ page }) => {
    await page.getByTestId('pm-panel-toggle').click();
    await expect(page.getByTestId('pm-panel')).toBeVisible();

    await page.getByTestId('pm-panel-close').click();
    await expect(page.getByTestId('pm-panel')).toBeHidden();
  });

  test('header surfaces persona dropdown and cost display', async ({ page }) => {
    await page.getByTestId('pm-panel-toggle').click();
    const header = page.getByTestId('pm-panel-header');
    await expect(header).toBeVisible();
    await expect(header.getByTestId('pm-panel-persona')).toBeVisible();
    await expect(header.getByTestId('pm-panel-cost')).toContainText('this session');
  });

  test('empty conversation shows quick-action prompts', async ({ page }) => {
    await page.getByTestId('pm-panel-toggle').click();
    await expect(page.getByTestId('pm-panel-empty')).toBeVisible();
    await expect(
      page.getByTestId('pm-panel-quick-plan-next-sprint')
    ).toBeVisible();
  });

  test('panel is global — visible from list+power layout as well as default Board', async ({ page }) => {
    await page.getByTestId('pm-panel-toggle').click();
    await expect(page.getByTestId('pm-panel')).toBeVisible();

    // gm-uipx.17: /grid folded into /board?layout=list&power=1; the
    // panel must still ride along on every Board layout.
    await page.goto('/board?layout=list&power=1');
    await expect(page.getByTestId('pm-panel')).toBeVisible();
  });
});
