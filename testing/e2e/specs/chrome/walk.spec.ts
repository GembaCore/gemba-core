// specs/chrome/walk.spec.ts — gm-uipx.2
//
// Gemba walk surface (ui-spec §5.4). Lives in chrome/ because the
// active-walk banner is global chrome — it shows on every route
// once a walk is active. Spec covers the two-pane /walk page,
// R/M/X/D decision hotkeys, the banner, and the PM panel takeover.

import { test, expect } from '../../fixtures/server';

test.describe('Gemba walk @chrome', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/board');
    await page.evaluate(() => window.localStorage.clear());
    await page.reload();
  });

  test('two-pane layout: agenda 360px left, chat flex right', async ({ page }) => {
    await page.goto('/walk');
    const agenda = page.getByTestId('walk-agenda-pane');
    const chat = page.getByTestId('walk-chat-pane');
    await expect(agenda).toBeVisible();
    await expect(chat).toBeVisible();

    const agendaBox = await agenda.boundingBox();
    if (!agendaBox) throw new Error('agenda pane had no bounding box');
    expect(agendaBox.width).toBeGreaterThanOrEqual(355);
    expect(agendaBox.width).toBeLessThanOrEqual(365);
  });

  test('agenda items render with source-kind icon and the active card highlights', async ({ page }) => {
    await page.goto('/walk');
    // Seed agenda's first item is in the active lane.
    const active = page.locator('[data-testid^="walk-agenda-item-"][data-lane="active"]').first();
    await expect(active).toBeVisible();
    // Urgency badge surfaces. Multiple seed items have urgency
    // badges; first() picks one deterministically.
    await expect(page.getByTestId(/walk-agenda-item-.*-urgency/).first()).toBeVisible();
  });

  test('R hotkey ratifies the active item and auto-promotes the next', async ({ page }) => {
    await page.goto('/walk');
    const firstActive = page.locator('[data-testid^="walk-agenda-item-"][data-lane="active"]').first();
    const firstId = await firstActive.getAttribute('data-testid');

    await page.locator('[data-testid="walk-chat-pane"]').click();
    await page.keyboard.press('r');

    // Originally-active card lands in decided.
    await expect(page.locator(`[data-testid="${firstId}"]`)).toHaveAttribute('data-lane', 'decided');
    // A different card is now active (auto-promoted).
    const newActive = page.locator('[data-testid^="walk-agenda-item-"][data-lane="active"]').first();
    await expect(newActive).toBeVisible();
    expect(await newActive.getAttribute('data-testid')).not.toBe(firstId);
  });

  test('D hotkey defers the active item', async ({ page }) => {
    await page.goto('/walk');
    const firstActive = page.locator('[data-testid^="walk-agenda-item-"][data-lane="active"]').first();
    const firstId = await firstActive.getAttribute('data-testid');

    await page.locator('[data-testid="walk-chat-pane"]').click();
    await page.keyboard.press('d');

    await expect(page.locator(`[data-testid="${firstId}"]`)).toHaveAttribute('data-lane', 'deferred');
  });

  test('active-walk banner appears on /walk and on other routes', async ({ page }) => {
    await page.goto('/walk');
    await expect(page.getByTestId('walk-banner')).toBeVisible();
    await expect(page.getByTestId('walk-banner')).toContainText('Gemba walk active');

    // Banner persists when the operator navigates away.
    await page.goto('/board');
    await expect(page.getByTestId('walk-banner')).toBeVisible();
  });

  test('end-walk button on the banner clears the walk globally', async ({ page }) => {
    await page.goto('/walk');
    await expect(page.getByTestId('walk-banner')).toBeVisible();

    await page.goto('/board');
    await page.getByTestId('walk-banner-end').click();
    await expect(page.getByTestId('walk-banner')).toBeHidden();
  });

  test('banner reports decision progress as items are decided', async ({ page }) => {
    await page.goto('/walk');
    await expect(page.getByTestId('walk-banner')).toContainText('0 of');

    await page.locator('[data-testid="walk-chat-pane"]').click();
    await page.keyboard.press('r');
    await expect(page.getByTestId('walk-banner')).toContainText('1 of');
  });

  test('PM panel becomes the walk chat surface when the walk is active', async ({ page }) => {
    await page.goto('/walk');
    // Open the PM panel from the topbar.
    await page.getByTestId('pm-panel-toggle').click();
    // The walk takeover surface surfaces inside the PM panel.
    await expect(page.getByTestId('pm-panel-walk')).toBeVisible();
    // Regular conversation is replaced.
    await expect(page.getByTestId('pm-panel-conversation')).toBeHidden();
  });

  test('clicking a queued agenda item promotes it to active', async ({ page }) => {
    await page.goto('/walk');
    const queued = page.locator('[data-testid^="walk-agenda-item-"][data-lane="queued"]').first();
    const queuedId = await queued.getAttribute('data-testid');
    await queued.click();
    await expect(page.locator(`[data-testid="${queuedId}"]`)).toHaveAttribute('data-lane', 'active');
  });
});
