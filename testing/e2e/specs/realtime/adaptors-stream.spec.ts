// specs/realtime/adaptors-stream.spec.ts
//
// Tier: realtime. Owner: gm-5v8v.10. Subject: AdaptorBanner +
// /api/adaptors health surfaces.
//
// AdaptorBanner is intentionally reactive now: it does not poll or
// subscribe to /api/adaptors/stream. Instead, client write failures
// emit a local operation-failed event, and the banner performs one
// fresh /api/adaptors?refresh=1 heartbeat. Fake mode seeds that
// heartbeat response; deep mode keeps a direct wire check for the
// live /api/adaptors and /api/adaptors/stream endpoints.

import type { Page } from '@playwright/test';
import { test, expect } from '../../fixtures/server';

test.describe('@realtime AdaptorBanner', () => {
  test('renders when an adaptor is degraded (fake)', async ({
    page,
    adaptorsState,
  }) => {
    test.skip(test.info().project.metadata?.backend === 'real',
      'fake-mode-only: seeds adaptorsState before navigation');

    adaptorsState.set([
      { name: 'beads', plane: 'work', healthy: false, reason: 'bd CLI not on PATH' },
    ]);
    await page.goto('/board');
    await dispatchAdaptorFailure(page);
    const banner = page.getByTestId('adaptor-banner');
    await expect(banner).toBeVisible();
    await expect(banner).toContainText('beads');
    await expect(banner).toContainText('bd CLI not on PATH');
  });

  test('hidden when every adaptor is healthy (fake)', async ({
    page,
    adaptorsState,
  }) => {
    test.skip(test.info().project.metadata?.backend === 'real',
      'fake-mode-only: seeds adaptorsState before navigation');

    adaptorsState.set([{ name: 'beads', plane: 'work', healthy: true }]);
    await page.goto('/board');
    await dispatchAdaptorFailure(page);
    await expect(page.getByTestId('adaptor-banner')).toHaveCount(0);
  });

  test('@deep /api/adaptors returns 200 with adaptors array', async ({ serverInfo }) => {
    test.skip(serverInfo.backend !== 'real', 'deep-only: real /api/adaptors');

    const res = await fetch(`${serverInfo.baseURL}/api/adaptors`);
    expect(res.status).toBe(200);
    const body = (await res.json()) as { adaptors: unknown[] };
    expect(Array.isArray(body.adaptors)).toBe(true);
  });

  test('@deep /api/adaptors/stream emits at least one SSE frame', async ({ serverInfo }) => {
    test.skip(serverInfo.backend !== 'real', 'deep-only: real SSE stream');

    // Use raw fetch instead of EventSource so the test doesn't need
    // a browser context just to assert the stream serves content.
    const res = await fetch(`${serverInfo.baseURL}/api/adaptors/stream`);
    expect(res.status).toBe(200);
    expect(res.headers.get('content-type')).toContain('text/event-stream');
    const reader = res.body!.getReader();
    const decoder = new TextDecoder();
    let buf = '';
    const deadline = Date.now() + 10_000;
    while (Date.now() < deadline) {
      const { value, done } = await reader.read();
      if (done) break;
      buf += decoder.decode(value, { stream: true });
      // Look for any data frame OR an SSE comment heartbeat — both
      // prove the connection is alive and serving the right content.
      if (buf.includes('data:') || buf.includes(': ')) break;
    }
    await reader.cancel().catch(() => {});
    expect(buf).toMatch(/(data:|: )/);
  });
});

async function dispatchAdaptorFailure(page: Page) {
  await page.evaluate(() => {
    window.dispatchEvent(new CustomEvent('gemba:adaptor-operation-failed', {
      detail: { status: 503, code: 'adaptor_degraded', message: 'probe', url: '/api/work-items' },
    }));
  });
}
