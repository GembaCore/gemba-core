// specs/board/grid-redirect.spec.ts — gm-uipx.17
//
// /grid was folded into /board?layout=list&power=1 (the WorkItemGrid
// stays the same; the chrome around it moved). The legacy /grid path
// is preserved as a permanent client-side redirect so existing
// bookmarks resolve. This spec asserts the redirect lands cleanly.

import { test, expect } from '../../fixtures/server';

test.describe('@chrome /grid redirect (gm-uipx.17)', () => {
  test('/grid redirects to /board?layout=list&power=1', async ({ page }) => {
    await page.goto('/grid');
    await expect(page).toHaveURL(/\/board\?layout=list&power=1$/);
  });
});
