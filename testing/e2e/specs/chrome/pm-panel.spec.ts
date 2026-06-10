// specs/chrome/pm-panel.spec.ts — gm-isxs (refresh after gm-96l)
//
// PM panel (ui-spec §2.4). Originally shipped as a bottom drawer
// (gm-uipx.12); gm-96l flipped it to a 320px right-edge panel
// with a permanent chevron handle that's visible whether the
// panel is collapsed or expanded. The Topbar toggle button is
// retained as a back-compat affordance.
//
// Lives in chrome/ because the panel is global chrome (mounted at
// the AppShell layer, not per-route). The route doesn't matter so
// this suite lands on /board.

import { test, expect } from '../../fixtures/server';

test.describe('PM panel @chrome', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/board');
    // Pristine localStorage so the open-state default is reproducible
    // across runs.
    await page.evaluate(() => window.localStorage.clear());
    await page.reload();
  });

  test('chevron handle opens the panel and the close button collapses it', async ({ page }) => {
    // gm-96l: the chevron handle is the always-visible right-edge
    // entry point. It's a button with data-testid="pm-panel-chevron"
    // that toggles the panel. When the panel is open the panel covers
    // the topbar toggle, so collapse rides on the in-panel close
    // button (or the chevron, which slides left to the panel edge).
    const chevron = page.getByTestId('pm-panel-chevron');
    await expect(chevron).toBeVisible();
    await expect(page.getByTestId('pm-panel')).toBeHidden();

    await chevron.click();
    await expect(page.getByTestId('pm-panel')).toBeVisible();
    await expect(chevron).toHaveAttribute('aria-expanded', 'true');

    await page.getByTestId('pm-panel-close').click();
    await expect(page.getByTestId('pm-panel')).toBeHidden();
    await expect(chevron).toHaveAttribute('aria-expanded', 'false');
  });

  test('Cmd+Shift+K opens the panel', async ({ page }) => {
    await expect(page.getByTestId('pm-panel')).toBeHidden();
    // Press the chord from anywhere on the page; the hotkey is
    // global so the active-element doesn't matter as long as it's
    // not eating the keystroke.
    await page.locator('main').click();
    await page.keyboard.press('Meta+Shift+K');
    await expect(page.getByTestId('pm-panel')).toBeVisible();
    // gm-96l: openAndFocus also bumps focusSeq so the textarea
    // claims focus. The unit test in PmPanel.test.tsx pins that
    // contract in isolation. In the full SPA the palette listens
    // on the same Mod+K root chord (gm-jvl8 fold) and races the
    // focus claim, so e2e asserts only the open contract — focus
    // is owned by the unit tier.
  });

  test('Escape collapses the panel', async ({ page }) => {
    await page.getByTestId('pm-panel-chevron').click();
    await expect(page.getByTestId('pm-panel')).toBeVisible();

    // Move focus off the panel input — Escape on a focused textarea
    // could be eaten by the browser's edit chrome.
    await page.locator('main').click();
    await page.keyboard.press('Escape');
    await expect(page.getByTestId('pm-panel')).toBeHidden();
  });

  test('Topbar toggle button still opens the panel (back-compat)', async ({ page }) => {
    const toggle = page.getByTestId('pm-panel-toggle');
    await expect(toggle).toBeVisible();
    await expect(page.getByTestId('pm-panel')).toBeHidden();

    await toggle.click();
    await expect(page.getByTestId('pm-panel')).toBeVisible();
    await expect(toggle).toHaveAttribute('aria-pressed', 'true');
  });

  test('header surfaces persona dropdown and cost display', async ({ page }) => {
    await page.getByTestId('pm-panel-chevron').click();
    const header = page.getByTestId('pm-panel-header');
    await expect(header).toBeVisible();
    await expect(header.getByTestId('pm-panel-persona')).toBeVisible();
    await expect(header.getByTestId('pm-panel-cost')).toContainText('this session');
  });

  test('view-aware quick actions render on /board', async ({ page }) => {
    // gm-96l: /board surfaces a small row of plan-staging verbs at
    // the top of the panel body. The first action ('Recommend
    // order') routes to /sprints; the others seed the input.
    await page.getByTestId('pm-panel-chevron').click();
    await expect(page.getByTestId('pm-panel-quick-actions')).toBeVisible();
    await expect(page.getByTestId('pm-panel-quick-recommend-order')).toBeVisible();
    await expect(page.getByTestId('pm-panel-quick-what-remains')).toBeVisible();
    await expect(page.getByTestId('pm-panel-quick-trim-to-budget')).toBeVisible();
  });

  test('view-aware quick actions render on /escalations (Triage)', async ({ page }) => {
    // gm-96l: /escalations swaps the plan-staging row for a single
    // 'Triage' action. Sanity check that the quick-action surface
    // is route-aware (not just a hardcoded /board strip).
    await page.goto('/escalations');
    await page.getByTestId('pm-panel-chevron').click();
    await expect(page.getByTestId('pm-panel-quick-actions')).toBeVisible();
    await expect(page.getByTestId('pm-panel-quick-triage')).toBeVisible();
  });

  test('panel is global — visible from list+power layout as well as default Board', async ({ page }) => {
    await page.getByTestId('pm-panel-chevron').click();
    await expect(page.getByTestId('pm-panel')).toBeVisible();

    // gm-uipx.17: /grid folded into /board?layout=list&power=1; the
    // panel must still ride along on every Board layout.
    await page.goto('/board?layout=list&power=1');
    await expect(page.getByTestId('pm-panel')).toBeVisible();
  });
});
