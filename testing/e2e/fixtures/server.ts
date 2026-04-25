// fixtures/server.ts
//
// The backend axis. Every spec runs against one of:
//
//   'fake' — Playwright `page.route()` intercepts /api/** and /events
//            with canned in-memory responses. Sub-second resets,
//            parallelizes freely, no Go binary, no Dolt. This is the
//            default and the only mode that ships in gm-5v8v.1.
//
//   'real' — Real `gemba serve` + Dolt + bd. Implementation lives in
//            gm-5v8v.2 (deep-mode backend infra). Until that lands,
//            requesting backend=real throws so deep-tier projects
//            can't accidentally green on faked data.
//
// Specs that assert on backend behavior (writes, SSE round-trips,
// auth) tag themselves @deep so the deep-* projects pick them up.
// Specs that only render the SPA shell stay backend-agnostic.

import { test as base, expect, type Page, type Route } from '@playwright/test';

export type Backend = 'fake' | 'real';

const ENV_BACKEND = (process.env.GEMBA_E2E_BACKEND as Backend | undefined) ?? 'fake';

type Fixtures = {
  backend: Backend;
  // Captured console errors + page errors for the current test. The
  // smoke spec asserts this is empty; richer specs filter by message.
  consoleErrors: string[];
};

export const test = base.extend<Fixtures>({
  backend: [ENV_BACKEND, { option: true }],

  page: async ({ page, backend }, use) => {
    if (backend === 'fake') {
      await installFakeBackend(page);
    } else {
      // Real-backend wiring lands in gm-5v8v.2. Until then, fail
      // loudly so we don't silently fall through to the fake stubs.
      throw new Error(
        `backend='real' is not implemented yet — see gm-5v8v.2 (deep-mode backend infra)`
      );
    }
    await use(page);
  },

  consoleErrors: async ({ page }, use) => {
    const errors: string[] = [];
    page.on('console', (msg) => {
      if (msg.type() === 'error') errors.push(msg.text());
    });
    page.on('pageerror', (err) => errors.push(String(err)));
    await use(errors);
  },
});

export { expect };

// ── Fake backend impl ──────────────────────────────────────────────
//
// The SPA hits a small set of endpoints on first paint. We fulfil each
// with the empty shape the typed clients in web/src/api expect, so
// every page renders its empty-state without a network error.

async function installFakeBackend(page: Page): Promise<void> {
  // Single dispatcher keyed off URL.pathname. We do NOT use glob /
  // regex matchers because globs and regexes match the full URL
  // string — `**/api/**` would also catch Vite's dev-time module
  // URLs like `/src/api/workItems.ts` and hand back JSON for what
  // the browser expected to be a JS module. A pathname predicate
  // and explicit dispatch keeps every match anchored at the path
  // root and removes route-ordering ambiguity.
  await page.route(
    (url) => new URL(url).pathname.startsWith('/api/') || matchesEvents(new URL(url).pathname),
    (route) => dispatch(route)
  );
}

function matchesEvents(p: string): boolean {
  return p === '/events';
}

function dispatch(route: Route): unknown {
  const path = new URL(route.request().url()).pathname;
  const json = (data: unknown) => route.fulfill({ json: data });
  const sse = (body: string) =>
    route.fulfill({
      status: 200,
      headers: { 'content-type': 'text/event-stream', 'cache-control': 'no-cache' },
      body,
    });

  // SSE channels — return a one-shot frame and end. The SPA's
  // EventSource auto-reconnects; auto-reconnect on a route() handler
  // looks like another request, which we serve identically. Nothing
  // pushes on this idle channel, so listeners stay subscribed without
  // surfacing console errors.
  if (path === '/events') return sse(': fake-backend idle\n\n');
  if (path === '/api/adaptors/stream') return sse('data: {"adaptors":[]}\n\n');

  // Empty-state JSON for the typed clients in web/src/api.
  if (isPath(path, '/api/work-items')) return json({ items: [], total: 0 });
  if (isPath(path, '/api/sessions')) return json({ items: [] });
  if (isPath(path, '/api/escalations')) return json({ items: [] });
  if (isPath(path, '/api/sprints')) return json({ items: [] });
  if (isPath(path, '/api/agents')) return json({ items: [] });
  if (isPath(path, '/api/capabilities')) return json({});
  if (isPath(path, '/api/adaptors')) return json({ adaptors: [] });

  // Anything else under /api/* — the smoke tier hasn't pinned a
  // shape, so empty-object is enough for rendering.
  return json({});
}

// Matches `${prefix}`, `${prefix}/...`, or `${prefix}?...`.
function isPath(path: string, prefix: string): boolean {
  return path === prefix || path.startsWith(`${prefix}/`) || path.startsWith(`${prefix}?`);
}
