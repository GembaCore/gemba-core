// specs/smoke/instance-id.spec.ts
//
// Tier: smoke. Covers the instance_id full-reload guard from gm-6m60.
// Capabilities are startup-immutable on the server: every response
// from /api/capabilities and /api/adaptors (snapshot + SSE) carries
// the per-process boot stamp. The SPA stashes the first id it sees
// and triggers a hard reload the first time a later id differs.
//
// The bead description names "/api/version" but the surface is
// actually /api/capabilities plus the reactive /api/adaptors?refresh=1
// heartbeat that runs after an adaptor operation fails.
//
// Tags: @smoke. Backend-agnostic — this is a behavioral assertion
// about the SPA's guard logic, exercised through the reactive heartbeat
// surface.

import { test, expect } from '../../fixtures/server';

test('instance_id mismatch on /api/adaptors triggers a full SPA reload @smoke', async ({
  page,
}) => {
  // The fake fixture installs a catch-all /api dispatcher; per-test
  // route handlers added afterwards are matched first (Playwright
  // matches in reverse registration order). We override the two
  // surfaces that carry instance_id with a counter so the second
  // delivery flips the id.

  let heartbeatCalls = 0;

  await page.route('**/api/adaptors?refresh=1', (route) => {
    heartbeatCalls += 1;
    void route.fulfill({ json: { adaptors: [], instance_id: 'boot-B' } });
  });

  await page.route('**/api/adaptors', (route) => {
    void route.fulfill({ json: { adaptors: [], instance_id: 'boot-A' } });
  });

  await page.route('**/api/capabilities', (route) => {
    // Capabilities is fetched once on boot (staleTime: Infinity);
    // mismatch comes from the SSE stream, not from this surface.
    void route.fulfill({ json: { instance_id: 'boot-A' } });
  });

  await page.goto('/board');
  await page.waitForLoadState('domcontentloaded');

  // Plant a sentinel that survives only as long as this document
  // does. A full-page reload boots a fresh JS realm, so the symbol
  // disappears. This is a more reliable reload signal than counting
  // framenavigated events (the dev server can fire spurious ones).
  await page.evaluate(() => {
    (window as unknown as { __preReload?: boolean }).__preReload = true;
  });

  await page.evaluate(() => {
    window.dispatchEvent(new CustomEvent('gemba:adaptor-operation-failed', {
      detail: { status: 503, code: 'adaptor_degraded', message: 'probe', url: '/api/work-items' },
    }));
  });

  await expect.poll(() => heartbeatCalls, { timeout: 5_000 }).toBeGreaterThanOrEqual(1);

  // The reload guard fires synchronously inside observeInstanceId;
  // the actual navigation flushes on the next tick. Poll for the
  // sentinel to disappear, which only happens across a full reload.
  // Mid-poll evaluate() can race with the navigation tearing down
  // the execution context — that error is itself proof of reload,
  // so treat it the same as "sentinel gone".
  await expect
    .poll(
      async () =>
        page
          .evaluate(() => {
            const w = window as unknown as { __preReload?: boolean };
            return w.__preReload === true;
          })
          .catch(() => false),
      { timeout: 5_000 },
    )
    .toBe(false);
});

test('stable instance_id does not trigger a reload @smoke', async ({ page }) => {
  // Counterpart assertion: when every frame carries the same id,
  // the guard stays quiet. This pins the "no spurious reload"
  // half of the contract.

  let heartbeatCalls = 0;
  await page.route('**/api/adaptors?refresh=1', (route) => {
    heartbeatCalls += 1;
    void route.fulfill({ json: { adaptors: [], instance_id: 'boot-stable' } });
  });

  await page.route('**/api/adaptors', (route) => {
    void route.fulfill({ json: { adaptors: [], instance_id: 'boot-stable' } });
  });

  await page.route('**/api/capabilities', (route) => {
    void route.fulfill({ json: { instance_id: 'boot-stable' } });
  });

  await page.goto('/board');
  await page.waitForLoadState('domcontentloaded');
  await page.evaluate(() => {
    (window as unknown as { __preReload?: boolean }).__preReload = true;
  });

  await page.evaluate(() => {
    window.dispatchEvent(new CustomEvent('gemba:adaptor-operation-failed', {
      detail: { status: 503, code: 'adaptor_degraded', message: 'probe', url: '/api/work-items' },
    }));
  });

  // Wait for the heartbeat path so we know the guard saw a later id.
  await expect.poll(() => heartbeatCalls, { timeout: 5_000 }).toBeGreaterThanOrEqual(1);

  // Give the guard a brief settle window. The sentinel must still
  // be intact — a reload would have wiped it.
  await page.waitForTimeout(500);
  const stillAlive = await page.evaluate(() => {
    const w = window as unknown as { __preReload?: boolean };
    return w.__preReload === true;
  });
  expect(stillAlive).toBe(true);
});
