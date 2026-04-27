// specs/error/network-error.spec.ts — gm-5v8v.13
//
// ui-spec §6.5: API non-fatal errors surface as inline alerts that
// don't block the rest of the page; fatal load errors render a
// dedicated error pane with retry. We exercise the inline path here
// against the GridPage which already renders an error block when
// useFilteredWorkItems fails.

import { test, expect } from '../../fixtures/server';

test.describe('@error network-error inline alerts', () => {
  test('GridPage surfaces a fetch error inline without crashing the shell', async ({
    page,
    consoleErrors,
  }) => {
    // Make /api/work-items return 500 BEFORE the page boots. The fake
    // backend's default route() runs after this override, so this one
    // wins for matching paths.
    await page.route(
      (url) => new URL(url).pathname.startsWith('/api/work-items'),
      (route) => route.fulfill({ status: 500, json: { error: 'internal' } })
    );
    await page.goto('/board?layout=list&power=1');
    // The shell still rendered.
    await expect(page.locator('main')).toBeVisible();
    // The error block is visible inside the grid pane.
    await expect(page.getByTestId('board-list-error')).toBeVisible();
    // Network errors hitting react-query bubble up as console errors
    // by default — that's fine, but should not be a *page* error
    // (uncaught exception would show as pageerror in consoleErrors).
    const pageErrors = consoleErrors.filter(
      (e) => !e.includes('500') && !e.includes('work-items') && !e.includes('Failed to load')
    );
    expect(pageErrors).toEqual([]);
  });

  test('an aborted fetch surfaces as inline error, shell stays interactive', async ({ page }) => {
    await page.route(
      (url) => new URL(url).pathname.startsWith('/api/work-items'),
      (route) => route.abort('connectionrefused')
    );
    await page.goto('/board?layout=list&power=1');
    await expect(page.locator('main')).toBeVisible();
    await expect(page.getByTestId('board-list-error')).toBeVisible();
    // Shell is still interactive — sidebar links keep working.
    await page.getByRole('link', { name: 'Board' }).click();
    await expect(page).toHaveURL(/\/board/);
  });

  test('AdaptorBanner does NOT render on a transient /api/work-items failure', async ({
    page,
    adaptorsState,
  }) => {
    // Adaptor health and per-route failures are independent surfaces:
    // a 500 from /api/work-items must not falsely paint the global
    // degraded banner. ui-spec §6.5 puts the banner only on
    // adaptor-degraded, not on per-API errors.
    adaptorsState.set([{ name: 'beads', plane: 'work', healthy: true }]);
    await page.route(
      (url) => new URL(url).pathname.startsWith('/api/work-items'),
      (route) => route.fulfill({ status: 503, json: { error: 'temporary' } })
    );
    await page.goto('/board?layout=list&power=1');
    await page.waitForLoadState('networkidle');
    await expect(page.getByTestId('adaptor-banner')).toHaveCount(0);
  });
});
