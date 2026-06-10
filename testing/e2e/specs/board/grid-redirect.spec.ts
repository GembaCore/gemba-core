// specs/board/grid-redirect.spec.ts — gm-uipx.17
//
// /grid is now a legacy alias for /refine, where dense table work
// lives. This spec asserts the redirect lands cleanly.

import { test, expect } from '../../fixtures/server';

test.describe('@chrome /grid redirect (gm-uipx.17)', () => {
  test('/grid redirects to /refine', async ({ page }) => {
    await page.goto('/grid');
    await expect(page).toHaveURL(/\/refine$/);
  });
});
