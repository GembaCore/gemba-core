// specs/realtime/adaptors-stream.spec.ts
//
// Tier: realtime. Owner: gm-5v8v.10. Subject: AdaptorBanner +
// /api/adaptors/stream.
//
// AdaptorBanner subscribes to the SSE stream at /api/adaptors/stream
// (gm-b1) with a /api/adaptors snapshot fallback. We exercise both
// halves: in fake mode, seed degraded adaptor data into the
// adaptorsState fixture and assert the banner paints; in @deep mode,
// hit the live endpoint and assert it returns valid JSON over the SSE
// transport (a healthy adaptor → no banner; the dynamic flip-to-degraded
// case requires killing bd mid-flight, which is an integration-tier
// follow-up).

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
    // Settle the SSE/snapshot dance before asserting absence.
    await page.waitForLoadState('networkidle');
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
