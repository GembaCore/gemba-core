// fixtures/server.ts
//
// The backend axis. Every spec runs against one of:
//
//   'fake' — Playwright `page.route()` intercepts /api/** and /events
//            with canned in-memory responses. Sub-second resets,
//            parallelizes freely, no Go binary, no Dolt. Default.
//
//   'real' — Real `gemba serve` + bd + per-worker tempdir-isolated
//            embedded Dolt. One server per worker (worker-scoped
//            fixture); tests reset bead state between runs via the
//            BdClient handle. Implementation in fixtures/realServer.ts
//            (gm-5v8v.2).
//
// Specs that assert on backend behavior tag themselves @deep so the
// deep-* projects pick them up. Specs that only render the SPA shell
// stay backend-agnostic and run identically against either backend.

import { test as base, expect, type Page, type Route } from '@playwright/test';
import { spinRealServer, type RealServer } from './realServer';
import { BdClient } from './bdClient';

export type Backend = 'fake' | 'real';

const ENV_BACKEND = (process.env.GEMBA_E2E_BACKEND as Backend | undefined) ?? 'fake';

type WorkerFixtures = {
  /** The active backend for this worker. Set by the project's `use`. */
  backend: Backend;
  /**
   * Real-server handle, lazily initialized when the worker is on the
   * 'real' backend. Disposed at worker teardown. `undefined` when the
   * worker is on the 'fake' backend.
   */
  realServer: RealServer | undefined;
};

type TestFixtures = {
  /** Captured console errors + page errors for the current test. */
  consoleErrors: string[];
  /**
   * Server info exposed to specs. baseURL points at the active server
   * (real or fake), beadsDir is non-empty only in 'real' mode.
   */
  serverInfo: { baseURL: string; backend: Backend; beadsDir?: string };
  /**
   * BdClient handle — only meaningful in 'real' mode. Tests can use
   * this to seed beads via the bd CLI. Throws if accessed under 'fake'.
   */
  bd: BdClient;
};

export const test = base.extend<TestFixtures, WorkerFixtures>({
  // ── Worker scope ─────────────────────────────────────────────────
  backend: [ENV_BACKEND, { option: true, scope: 'worker' }],

  realServer: [
    async ({ backend }, use, workerInfo) => {
      if (backend !== 'real') {
        await use(undefined);
        return;
      }
      const server = await spinRealServer({ workerIndex: workerInfo.workerIndex });
      try {
        await use(server);
      } finally {
        await server.dispose();
      }
    },
    { scope: 'worker' },
  ],

  // ── Test scope ───────────────────────────────────────────────────
  page: async ({ page, backend, realServer }, use) => {
    if (backend === 'fake') {
      await installFakeBackend(page);
    } else {
      if (!realServer) {
        throw new Error('backend=real but realServer fixture is undefined');
      }
      // Override baseURL for this test's page navigations. The fake
      // mode uses Playwright's project-level baseURL; the real mode
      // points at the per-worker gemba listener.
      await page.goto(realServer.baseURL);
    }
    await use(page);
  },

  serverInfo: async ({ backend, realServer, baseURL }, use) => {
    if (backend === 'real') {
      if (!realServer) throw new Error('backend=real but realServer is undefined');
      await use({ baseURL: realServer.baseURL, backend, beadsDir: realServer.beadsDir });
    } else {
      await use({ baseURL: baseURL ?? 'http://localhost:5173', backend });
    }
  },

  bd: async ({ backend, realServer }, use) => {
    if (backend !== 'real' || !realServer) {
      throw new Error(
        'bd fixture is only available in deep mode (backend=real). ' +
          'Tag the spec @deep and run under a *-deep project.'
      );
    }
    // Reset bead state between tests so specs are independent.
    const client = new BdClient({ beadsDir: realServer.beadsDir });
    try {
      await use(client);
    } finally {
      await client.resetAll().catch(() => {/* best-effort cleanup */});
    }
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
  if (isPath(path, '/api/health')) return json({ status: 'ok' });

  // Anything else under /api/* — the smoke tier hasn't pinned a
  // shape, so empty-object is enough for rendering.
  return json({});
}

// Matches `${prefix}`, `${prefix}/...`, or `${prefix}?...`.
function isPath(path: string, prefix: string): boolean {
  return path === prefix || path.startsWith(`${prefix}/`) || path.startsWith(`${prefix}?`);
}
