// specs/bootstrap/bootstrap.spec.ts — gm-uipx.7
//
// ui-spec §5.15 — Bootstrap wizard (`/bootstrap`).
//
// 4-step modal wizard: Source → Analysis → Plan review → Ratify.
// Modal-style step boundaries with nonce-confirmed final commit.
// Cancel from any step persists nothing — local state only, no
// backend writes until Step 4.

import { test, expect } from '../../fixtures/server';

test.describe('/bootstrap (ui-spec §5.15) @route', () => {
  test('Step 1 Source tiles: Jira / Beads / Source-code / Fresh', async ({ page }) => {
    await page.goto('/bootstrap');
    // Bare /bootstrap redirects to /bootstrap/source; the four tiles
    // render as buttons with stable testids.
    await expect.poll(() => page.url(), { timeout: 2_000 }).toContain('/bootstrap/source');
    await expect(page.getByTestId('bootstrap-source-tiles')).toBeVisible();
    await expect(page.getByTestId('bootstrap-source-tile-jira')).toBeVisible();
    await expect(page.getByTestId('bootstrap-source-tile-beads')).toBeVisible();
    await expect(page.getByTestId('bootstrap-source-tile-source-code')).toBeVisible();
    await expect(page.getByTestId('bootstrap-source-tile-fresh')).toBeVisible();

    // Click commits the source choice and advances to Step 2.
    await page.getByTestId('bootstrap-source-tile-fresh').click();
    await expect.poll(() => page.url(), { timeout: 2_000 }).toContain('/bootstrap/analysis');
  });

  test('Step 2 Analysis shows live progress strings', async ({ page }) => {
    await page.goto('/bootstrap/source');
    await page.getByTestId('bootstrap-source-tile-fresh').click();
    await expect(page.getByTestId('bootstrap-analysis-step')).toBeVisible();
    // The fake backend streams 3 frames; the SPA renders them in the log.
    await expect(page.getByTestId('bootstrap-analysis-line-1')).toBeVisible();
    await expect(page.getByTestId('bootstrap-analysis-line-3')).toBeVisible();
    await expect(page.getByTestId('bootstrap-analysis-line-1')).toContainText('Scanning');
    // Final frame's done=true unlocks the continue button.
    await expect(page.getByTestId('bootstrap-analysis-continue')).toBeEnabled();
  });

  test('Step 3 Plan review: side-by-side plan + consistency report', async ({ page }) => {
    await page.goto('/bootstrap/source');
    await page.getByTestId('bootstrap-source-tile-fresh').click();
    await expect(page.getByTestId('bootstrap-analysis-continue')).toBeEnabled();
    await page.getByTestId('bootstrap-analysis-continue').click();
    await expect(page.getByTestId('bootstrap-plan-step')).toBeVisible();
    await expect(page.getByTestId('bootstrap-plan-pane')).toBeVisible();
    await expect(page.getByTestId('bootstrap-report-pane')).toBeVisible();
    // Top-line PASS / WARN / FAIL summary visible.
    await expect(page.getByTestId('bootstrap-report-overall')).toBeVisible();
    await expect(page.getByTestId('bootstrap-report-count-pass')).toContainText('PASS');
    await expect(page.getByTestId('bootstrap-report-count-warn')).toContainText('WARN');
    await expect(page.getByTestId('bootstrap-report-count-fail')).toContainText('FAIL');
    // Per-finding drill-down toggle reveals detail.
    await page.getByTestId('bootstrap-report-toggle-f2').click();
    await expect(page.getByTestId('bootstrap-report-detail-f2')).toBeVisible();
  });

  test('Step 4 Ratify nonce-confirms commit of project config', async ({ page }) => {
    // Capture the commit POST so we can assert the nonce header is
    // present (X-GEMBA-Confirm — server-side nonce gate per
    // workItems.ts CONFIRM_HEADER).
    let commitConfirmHeader: string | undefined;
    await page.route('**/api/bootstrap/commit', async (route) => {
      const headers = route.request().headers();
      commitConfirmHeader = headers['x-gemba-confirm'];
      await route.fulfill({
        json: { project_path: '.gemba/project.toml', board_url: '/board' },
      });
    });

    await page.goto('/bootstrap/source');
    await page.getByTestId('bootstrap-source-tile-fresh').click();
    await expect(page.getByTestId('bootstrap-analysis-continue')).toBeEnabled();
    await page.getByTestId('bootstrap-analysis-continue').click();
    await page.getByTestId('bootstrap-plan-continue').click();
    await expect(page.getByTestId('bootstrap-ratify-step')).toBeVisible();
    await page.getByTestId('bootstrap-ratify-commit').click();
    // On commit success the SPA navigates to the server-supplied
    // board URL.
    await expect.poll(() => page.url(), { timeout: 5_000 }).toContain('/board');
    // The nonce header was attached to the commit POST.
    expect(commitConfirmHeader, 'commit POST must carry X-GEMBA-Confirm nonce').toBeTruthy();
    expect(commitConfirmHeader!.length).toBeGreaterThan(0);
  });

  test('Cancel from any step does not persist anything', async ({ page }) => {
    // Track whether the SPA hits the commit endpoint — cancel must
    // not trigger any /api/bootstrap/commit call.
    let commitCalls = 0;
    await page.route('**/api/bootstrap/commit', async (route) => {
      commitCalls += 1;
      await route.fulfill({
        json: { project_path: '.gemba/project.toml', board_url: '/board' },
      });
    });

    await page.goto('/bootstrap/source');
    await page.getByTestId('bootstrap-source-tile-fresh').click();
    await expect(page.getByTestId('bootstrap-analysis-continue')).toBeEnabled();
    // Cancel mid-flow at Step 2.
    await page.getByTestId('bootstrap-cancel').click();
    await expect.poll(() => page.url(), { timeout: 2_000 }).toContain('/board');
    expect(commitCalls).toBe(0);

    // Re-enter the wizard and confirm state was cleared.
    await page.goto('/bootstrap/source');
    await expect(page.getByTestId('bootstrap-source-tiles')).toBeVisible();
  });
});
