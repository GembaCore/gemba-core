// helpers/waits.ts
//
// Tiny non-flaky wait helpers. Built out as specs need them; the
// smoke tier doesn't need anything beyond Playwright's auto-waiting.

import type { Page } from '@playwright/test';

// Wait until react-query has settled all in-flight queries. Useful
// for asserting "the page has finished its initial fetches" without
// relying on a specific spinner being absent.
export async function networkIdle(page: Page, timeoutMs = 5_000): Promise<void> {
  await page.waitForLoadState('networkidle', { timeout: timeoutMs });
}
