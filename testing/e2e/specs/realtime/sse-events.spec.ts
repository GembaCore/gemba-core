// specs/realtime/sse-events.spec.ts
//
// Tier: realtime. Owner: gm-5v8v.10. Subject: /events SSE end-to-end.
//
// The full chain: bd write → WorkPlane.Subscribe → events.Hub fan-out
// → /events SSE → SPA EventSource → react-query invalidation → UI
// reflects the change without a reload.
//
// All specs are @deep — testing the SSE fan-out under fake mode would
// require simulating a streaming response, which doesn't catch the
// bugs that matter (server-side fan-out, hub topology, EventSource
// auto-reconnect). The SPA's consumer logic is unit-tested already in
// web/src/data/__tests__/sse.test.ts.
//
// Note on the trigger: in this fixture's deep mode, gemba serve shells
// to bd for writes — bd writes from a SEPARATE process (the
// BdClient) don't surface in gemba's in-process WorkPlane emitter.
// Production closes that gap with the post-commit git hook
// (gm-e4.3.3) which POSTs /api/workitems/notify; the fixture's
// `bd init --skip-hooks` doesn't install that hook, so we drive the
// event chain via an explicit notify call after each bd write.
// workitem-notify.spec.ts asserts the notify endpoint itself; this
// file asserts the downstream SSE → SPA invalidation that depends on it.

import { test, expect } from '../../fixtures/server';

test.describe('@deep @realtime /events SSE → SPA invalidation', () => {
  test.skip(({ backend }) => backend !== 'real', 'deep-only spec');

  test('GET /events returns 200 text/event-stream', async ({ serverInfo }) => {
    const res = await fetch(`${serverInfo.baseURL}/events`);
    expect(res.status).toBe(200);
    expect(res.headers.get('content-type')).toContain('text/event-stream');
    await res.body?.cancel().catch(() => {});
  });

  test('POST /api/workitems/notify emits a workitem.* event on /events within 5s', async ({
    bd,
    serverInfo,
  }) => {
    // Open the SSE stream BEFORE the notify so we don't race the
    // event publication. The connection itself is enough — events
    // emitted after this point land in the response stream.
    const ac = new AbortController();
    const res = await fetch(`${serverInfo.baseURL}/events`, { signal: ac.signal });
    expect(res.status).toBe(200);
    const reader = res.body!.getReader();
    const decoder = new TextDecoder();

    // bd create + notify drives the event chain end to end.
    const { id } = await bd.create({ title: 'sse-events smoke bead', type: 'task' });
    const notifyRes = await fetch(`${serverInfo.baseURL}/api/workitems/notify`, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ work_item_id: id, source: 'e2e-sse-events' }),
    });
    expect(notifyRes.status).toBe(200);

    let buf = '';
    let sawWorkitemEvent = false;
    const deadline = Date.now() + 5_000;
    while (Date.now() < deadline) {
      const { value, done } = await reader.read();
      if (done) break;
      buf += decoder.decode(value, { stream: true });
      // The /events handler stamps the kind on the SSE event: line
      // (per web/src/data/sse.ts). Match either the event line or a
      // workitem kind embedded in the JSON payload.
      if (/event:\s*workitem\./.test(buf) || /"kind":"workitem\.[^"]+"/.test(buf)) {
        sawWorkitemEvent = true;
        break;
      }
    }
    ac.abort();
    expect(
      sawWorkitemEvent,
      `no workitem.* event seen on /events within 5s. buf=${buf.slice(0, 800)}`
    ).toBe(true);
  });

  test('EventSource in browser context receives workitem.* event from notify', async ({
    page,
    bd,
    serverInfo,
  }) => {
    // Open the SPA so the page is on the same origin as /events.
    await page.goto(`${serverInfo.baseURL}/board`);
    await expect(page.locator('main')).toBeVisible();

    // Open a fresh EventSource inside the browser context and capture
    // the first workitem.* event it sees. This isolates the assertion
    // to the SSE wire — if it works here, the SPA's react-query
    // invalidation is downstream wiring (covered by web/src/data
    // unit tests).
    const eventPromise = page.evaluate<{ kind: string; raw: string } | { error: string }>(
      () =>
        new Promise((resolve) => {
          const es = new EventSource('/events');
          const seen: string[] = [];
          let resolved = false;
          const finish = (kind: string, raw: string) => {
            if (resolved) return;
            resolved = true;
            es.close();
            resolve({ kind, raw });
          };
          // Browsers only fire typed listeners for matching event:
          // names; the catch-all 'message' listener fires only for
          // default-typed frames. Register both.
          for (const k of ['workitem.created', 'workitem.updated', 'workitem.closed']) {
            es.addEventListener(k, (ev) => finish(k, (ev as MessageEvent).data));
          }
          es.addEventListener('message', (ev) => {
            seen.push((ev as MessageEvent).data);
            try {
              const parsed = JSON.parse((ev as MessageEvent).data) as { kind?: string };
              if (parsed.kind?.startsWith('workitem.')) {
                finish(parsed.kind, (ev as MessageEvent).data);
              }
            } catch {
              /* ignore */
            }
          });
          setTimeout(() => {
            if (!resolved) {
              resolved = true;
              es.close();
              resolve({ error: `timeout — saw ${seen.length} non-workitem frames` });
            }
          }, 10_000);
        })
    );

    // Trigger the event.
    const { id } = await bd.create({ title: 'sse-events EventSource probe', type: 'task' });
    const notifyRes = await fetch(`${serverInfo.baseURL}/api/workitems/notify`, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ work_item_id: id, source: 'e2e-sse-events' }),
    });
    expect(notifyRes.status).toBe(200);

    const result = await eventPromise;
    expect(result, JSON.stringify(result)).toHaveProperty('kind');
    if ('kind' in result) {
      expect(result.kind).toMatch(/^workitem\./);
    }
  });
});
