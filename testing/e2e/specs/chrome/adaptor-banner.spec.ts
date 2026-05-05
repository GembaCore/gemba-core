// specs/chrome/adaptor-banner.spec.ts — gm-5v8v.4
//
// AdaptorBanner (web/src/components/AdaptorBanner.tsx) renders only
// when an adaptor operation fails and the follow-up
// /api/adaptors?refresh=1 heartbeat also reports unhealthy. The banner
// deliberately does not poll or subscribe to the adaptor SSE stream.

import { test, expect } from '../../fixtures/server';

const REFRESH = '**/api/adaptors?refresh=1';
const SNAPSHOT = '**/api/adaptors';

async function fireAdaptorFailure(page: import('@playwright/test').Page) {
  await page.evaluate(() => {
    window.dispatchEvent(new CustomEvent('gemba:adaptor-operation-failed', {
      detail: { status: 503, code: 'adaptor_degraded', message: 'probe', url: '/api/work-items' },
    }));
  });
}

test.describe('AdaptorBanner @chrome', () => {
  test('healthy adaptors leave the banner hidden', async ({ page }) => {
    await page.route(REFRESH, (route) => {
      void route.fulfill({
        json: { instance_id: 'boot-1', adaptors: [{ name: 'beads', plane: 'work', healthy: true }] },
      });
    });
    await page.route(SNAPSHOT, (route) => {
      void route.fulfill({
        json: { instance_id: 'boot-1', adaptors: [{ name: 'beads', plane: 'work', healthy: true }] },
      });
    });

    await page.goto('/board');
    await fireAdaptorFailure(page);
    await page.waitForTimeout(100);
    await expect(page.getByTestId('adaptor-banner')).toHaveCount(0);
  });

  test('a degraded adaptor surfaces the banner with name + reason', async ({ page }) => {
    await page.route(REFRESH, (route) => {
      void route.fulfill({
        json: {
          instance_id: 'boot-1',
          adaptors: [
            { name: 'beads', plane: 'work', healthy: false, reason: 'dolt unreachable' },
          ],
        },
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
    await fireAdaptorFailure(page);
    const banner = page.getByTestId('adaptor-banner');
    await expect(banner).toBeVisible();
    await expect(banner).toContainText('Adaptor degraded');
    await expect(banner).toContainText('beads');
    await expect(banner).toContainText('dolt unreachable');
  });

  test('recovery — a follow-up healthy frame removes the banner', async ({ page }) => {
    let heartbeatCalls = 0;
    await page.route(REFRESH, (route) => {
      heartbeatCalls += 1;
      const healthy = heartbeatCalls > 1;
      const adaptors = healthy
        ? [{ name: 'beads', plane: 'work', healthy: true }]
        : [{ name: 'beads', plane: 'work', healthy: false, reason: 'temporary' }];
      void route.fulfill({
        json: { instance_id: 'boot-1', adaptors },
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
    await fireAdaptorFailure(page);
    const banner = page.getByTestId('adaptor-banner');
    await expect(banner).toBeVisible();

    await fireAdaptorFailure(page);
    await expect.poll(() => heartbeatCalls, { timeout: 5_000 }).toBeGreaterThanOrEqual(2);
    await expect(banner).toHaveCount(0);
  });
});
