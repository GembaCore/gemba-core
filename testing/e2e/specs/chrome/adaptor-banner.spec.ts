// specs/chrome/adaptor-banner.spec.ts — gm-5v8v.4
//
// AdaptorBanner (web/src/components/AdaptorBanner.tsx) renders only
// when at least one adaptor reports unhealthy. The signal arrives via
// the /api/adaptors/stream SSE pump (with a /api/adaptors snapshot
// fallback). This spec drives both halves through fixture overrides
// — registering more-specific page.route handlers that win over the
// fake-fixture catch-all.

import { test, expect } from '../../fixtures/server';

const STREAM = '**/api/adaptors/stream';
const SNAPSHOT = '**/api/adaptors';

test.describe('AdaptorBanner @chrome', () => {
  test('healthy adaptors leave the banner hidden', async ({ page }) => {
    await page.route(STREAM, (route) => {
      void route.fulfill({
        status: 200,
        headers: { 'content-type': 'text/event-stream', 'cache-control': 'no-cache' },
        body:
          'retry: 100\n' +
          `data: ${JSON.stringify({ instance_id: 'boot-1', adaptors: [{ name: 'beads', plane: 'work', healthy: true }] })}\n\n`,
      });
    });
    await page.route(SNAPSHOT, (route) => {
      void route.fulfill({
        json: { instance_id: 'boot-1', adaptors: [{ name: 'beads', plane: 'work', healthy: true }] },
      });
    });

    await page.goto('/board');
    // Give the SSE pump a moment to deliver the first frame; the
    // banner condition is "at least one degraded", so a healthy
    // payload renders nothing.
    await page.waitForTimeout(300);
    await expect(page.getByTestId('adaptor-banner')).toHaveCount(0);
  });

  test('a degraded adaptor surfaces the banner with name + reason', async ({ page }) => {
    await page.route(STREAM, (route) => {
      void route.fulfill({
        status: 200,
        headers: { 'content-type': 'text/event-stream', 'cache-control': 'no-cache' },
        body:
          'retry: 100\n' +
          `data: ${JSON.stringify({
            instance_id: 'boot-1',
            adaptors: [
              { name: 'beads', plane: 'work', healthy: false, reason: 'dolt unreachable' },
            ],
          })}\n\n`,
      });
    });
    await page.route(SNAPSHOT, (route) => {
      void route.fulfill({
        json: {
          instance_id: 'boot-1',
          adaptors: [
            { name: 'beads', plane: 'work', healthy: false, reason: 'dolt unreachable' },
          ],
        },
      });
    });

    await page.goto('/board');
    const banner = page.getByTestId('adaptor-banner');
    await expect(banner).toBeVisible();
    await expect(banner).toContainText('Adaptor degraded');
    await expect(banner).toContainText('beads');
    await expect(banner).toContainText('dolt unreachable');
  });

  test('recovery — a follow-up healthy frame removes the banner', async ({ page }) => {
    // First SSE call delivers degraded; on EventSource reconnect
    // (retry: 100ms) the second call delivers healthy. The banner
    // should disappear once the cache refreshes.
    let streamCalls = 0;
    await page.route(STREAM, (route) => {
      streamCalls += 1;
      const healthy = streamCalls > 1;
      const adaptors = healthy
        ? [{ name: 'beads', plane: 'work', healthy: true }]
        : [{ name: 'beads', plane: 'work', healthy: false, reason: 'temporary' }];
      void route.fulfill({
        status: 200,
        headers: { 'content-type': 'text/event-stream', 'cache-control': 'no-cache' },
        body:
          'retry: 100\n' +
          `data: ${JSON.stringify({ instance_id: 'boot-1', adaptors })}\n\n`,
      });
    });
    await page.route(SNAPSHOT, (route) => {
      // Snapshot fallback keeps the same instance_id so the reload
      // guard doesn't fire mid-test (gm-6m60).
      void route.fulfill({
        json: {
          instance_id: 'boot-1',
          adaptors: [{ name: 'beads', plane: 'work', healthy: false, reason: 'temporary' }],
        },
      });
    });

    await page.goto('/board');
    const banner = page.getByTestId('adaptor-banner');
    await expect(banner).toBeVisible();

    // Wait for the SSE to reconnect at least once with the healthy
    // payload, then confirm the banner is gone.
    await expect.poll(() => streamCalls, { timeout: 5_000 }).toBeGreaterThanOrEqual(2);
    await expect(banner).toHaveCount(0);
  });
});
