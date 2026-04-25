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
import { createWorkPlane, type WorkPlaneStore } from './workplane';
import { createSessionStore, type SessionStore } from './sessionStore';
import { createEscalationStore, type EscalationStore } from './escalationStore';
import { createAgentStore, type AgentStore } from './agentStore';
import { createModeHandle, DEFAULT_MODE, type ModeHandle } from './modes';
import type { WorkItem } from '../../../web/src/types/core.gen';

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
  /**
   * Per-test in-memory WorkPlane. Specs call workPlane.seed([...]) to
   * populate items; the fake-backend dispatcher reads from this same
   * instance so /api/work-items + /api/work-items/:id reflect what the
   * spec asked for. Only meaningful in 'fake' mode — in 'real' mode
   * tests use the `bd` fixture to seed via the bd CLI instead.
   */
  workPlane: WorkPlaneStore;
  /**
   * Per-test in-memory Sessions store. gm-5v8v.9. Same shape pattern
   * as workPlane; the fake dispatcher reads from this for
   * /api/sessions list + /api/sessions/:id detail/end. Only meaningful
   * in fake mode.
   */
  sessionPlane: SessionStore;
  /**
   * Per-test in-memory Escalations store. gm-5v8v.9. Drives
   * /api/escalations list + /api/escalations/:id/respond writeback.
   */
  escalationPlane: EscalationStore;
  /** Per-test in-memory Agents roster store. gm-5v8v.9. */
  agentPlane: AgentStore;
  /**
   * Per-test adaptor health state. gm-5v8v.10. Specs (mostly under
   * specs/realtime/) call adaptorsState.set([{...degraded}]) BEFORE
   * navigating; the fake-backend dispatcher serves both /api/adaptors
   * and the /api/adaptors/stream initial frame from this state so the
   * SPA paints the AdaptorBanner without needing live SSE pushes.
   */
  adaptorsState: AdaptorsState;
  /**
   * Workspace mode (unsupervised / supervised / managed) for the
   * current test (gm-5v8v.11). Defaults to 'supervised' per ui-spec
   * §6.2. Specs override by setting `mode` on test.use(): see
   * specs/modes/**.spec.ts. Currently a fixture without an active
   * SPA consumer — the SPA hasn't grown mode-gated confirmation UX
   * yet, so all modes/* tests are fixme'd against that surface.
   */
  mode: ModeHandle;
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
  workPlane: async ({}, use) => {
    const store = createWorkPlane();
    await use(store);
  },

  sessionPlane: async ({}, use) => {
    const store = createSessionStore();
    await use(store);
  },

  escalationPlane: async ({}, use) => {
    const store = createEscalationStore();
    await use(store);
  },

  agentPlane: async ({}, use) => {
    const store = createAgentStore();
    await use(store);
  },

  adaptorsState: async ({}, use) => {
    const state = createAdaptorsState();
    await use(state);
  },

  mode: async ({}, use) => {
    // Specs override the initial mode by setting WorkspaceMode on
    // test.use({ mode: createModeHandle('managed') }) in the spec
    // file's describe block, OR by mutating the handle inside a
    // beforeEach. Default mirrors ui-spec §6.2.
    const handle = createModeHandle(DEFAULT_MODE);
    await use(handle);
  },

  page: async (
    { page, backend, realServer, workPlane, sessionPlane, escalationPlane, agentPlane, adaptorsState },
    use
  ) => {
    if (backend === 'fake') {
      await installFakeBackend(page, {
        workPlane,
        sessionPlane,
        escalationPlane,
        agentPlane,
        adaptorsState,
      });
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
    // Inherit realServer.env so HOME/BEADS_DIR isolation (gm-h4n)
    // applies to every bd command this client spawns.
    const client = new BdClient({
      beadsDir: realServer.beadsDir,
      env: realServer.env,
    });
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

// ── Adaptors-state fixture (gm-5v8v.10) ────────────────────────────
//
// AdaptorBanner reads /api/adaptors via SSE with a /api/adaptors snapshot
// fallback. To exercise the banner under fake mode the spec needs a way
// to seed the response; this small store is shared between the dispatcher
// and the spec, mutated by the spec before it navigates.

export type AdaptorStatus = {
  name: string;
  plane: 'work' | 'orchestration';
  healthy: boolean;
  reason?: string;
};

export type AdaptorsState = {
  set(entries: AdaptorStatus[]): void;
  get(): AdaptorStatus[];
};

function createAdaptorsState(): AdaptorsState {
  let entries: AdaptorStatus[] = [];
  return {
    set: (next) => {
      entries = [...next];
    },
    get: () => entries,
  };
}

interface FakeStores {
  workPlane: WorkPlaneStore;
  sessionPlane: SessionStore;
  escalationPlane: EscalationStore;
  agentPlane: AgentStore;
  adaptorsState: AdaptorsState;
}

async function installFakeBackend(page: Page, stores: FakeStores): Promise<void> {
  // Single dispatcher keyed off URL.pathname. We do NOT use glob /
  // regex matchers because globs and regexes match the full URL
  // string — `**/api/**` would also catch Vite's dev-time module
  // URLs like `/src/api/workItems.ts` and hand back JSON for what
  // the browser expected to be a JS module. A pathname predicate
  // and explicit dispatch keeps every match anchored at the path
  // root and removes route-ordering ambiguity.
  await page.route(
    (url) => new URL(url).pathname.startsWith('/api/') || matchesEvents(new URL(url).pathname),
    (route) => dispatch(route, stores)
  );
}

function matchesEvents(p: string): boolean {
  return p === '/events';
}

function dispatch(route: Route, stores: FakeStores): unknown {
  const { workPlane, sessionPlane, escalationPlane, agentPlane, adaptorsState } = stores;
  const url = new URL(route.request().url());
  const path = url.pathname;
  const method = route.request().method();
  const json = (data: unknown) => route.fulfill({ json: data });
  const notFound = (code: string, message: string) =>
    route.fulfill({ status: 404, json: { error: code, message } });
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
  if (path === '/api/adaptors/stream') {
    // Single initial frame mirroring whatever the spec seeded into
    // adaptorsState. The browser auto-reconnects on close; each
    // reconnect re-reads the (possibly-mutated) state.
    const payload = JSON.stringify({ adaptors: adaptorsState.get() });
    return sse(`data: ${payload}\n\n`);
  }

  // /api/work-items/:id — drawer reads happen through this surface.
  // Match this BEFORE the list endpoint so /api/work-items/gm-1 isn't
  // treated as an items query.
  const itemMatch = path.match(/^\/api\/work-items\/([^/]+)$/);
  if (itemMatch) {
    const id = decodeURIComponent(itemMatch[1] ?? '');
    if (route.request().method() === 'PATCH') {
      // Mutations: echo the seeded item back so optimistic update
      // settles. A richer fake (apply patch to store) is a follow-up.
      const existing = workPlane.get(id);
      if (existing) return json(existing);
      return notFound('session_not_found', `work item ${id} not found`);
    }
    const wi = workPlane.get(id);
    if (wi) return json(wi);
    return notFound('session_not_found', `work item ${id} not found`);
  }

  if (isPath(path, '/api/work-items')) {
    let items = workPlane.list();
    // Honor state_category / kind query params so view-driven filters
    // (gm-5v8v.6.3) actually narrow under fake mode. Empty params
    // mean "no filter" — match the SPA's omit-when-empty contract.
    const stateCats = url.searchParams.getAll('state_category');
    const kinds = url.searchParams.getAll('kind');
    if (stateCats.length > 0) {
      items = items.filter((it) => stateCats.includes(it.state_category));
    }
    if (kinds.length > 0) {
      items = items.filter((it) => kinds.includes(it.kind));
    }
    if (route.request().method() === 'POST') {
      // Pretend the create succeeded with a synthetic id; specs that
      // assert on persistence tag themselves @deep so this branch
      // only runs in fake mode.
      const fake: WorkItem = {
        id: `fake-${items.length + 1}`,
        kind: 'task',
        title: 'fake-created',
        status: 'open',
        state_category: 'unstarted',
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
      };
      workPlane.add(fake);
      return json(fake);
    }
    return json({ items, total: items.length });
  }

  // /api/sessions/:id and /api/sessions/:id/peek (gm-5v8v.9). The
  // detail-or-end pattern has to match BEFORE the list endpoint so a
  // path like /api/sessions/sess-1 isn't treated as a list query.
  const peekMatch = path.match(/^\/api\/sessions\/([^/]+)\/peek$/);
  if (peekMatch) {
    const id = decodeURIComponent(peekMatch[1] ?? '');
    const sess = sessionPlane.get(id);
    if (!sess) return notFound('session_not_found', `session ${id} not found`);
    return json({
      session_id: id,
      status: sess.status,
      transcript_tail: '',
      heartbeat: sess.last_heartbeat ?? null,
    });
  }
  const sessionMatch = path.match(/^\/api\/sessions\/([^/]+)$/);
  if (sessionMatch) {
    const id = decodeURIComponent(sessionMatch[1] ?? '');
    if (method === 'DELETE') {
      // End: drop from the store and echo a 200. Real server returns
      // 200 + the materialized session in its terminal state; we
      // simulate that by handing back a synthetic terminal record.
      const before = sessionPlane.get(id);
      sessionPlane.remove(id);
      if (!before) return notFound('session_not_found', `session ${id} not found`);
      return json({
        ...before,
        status: 'completed',
        ended_at: new Date().toISOString(),
      });
    }
    const sess = sessionPlane.get(id);
    if (sess) return json(sess);
    return notFound('session_not_found', `session ${id} not found`);
  }

  if (isPath(path, '/api/sessions')) {
    if (method === 'POST') {
      // Mint a fake session id and persist it. The body shape is
      // {bead_id, agent_type, title?, workspace?} — we echo enough of
      // it back into provider_metadata that the SessionsPage row
      // renders meaningfully.
      const body = parseBody(route.request().postData());
      const id = `sess-fake-${sessionPlane.list().length + 1}`;
      const now = new Date().toISOString();
      const session = {
        id,
        assignment_id: typeof body.bead_id === 'string' ? body.bead_id : 'gm-fake',
        agent_id: typeof body.agent_type === 'string' ? body.agent_type : 'agent',
        status: 'initializing' as const,
        started_at: now,
        provider_metadata: {
          bead_id: body.bead_id,
          agent_type: body.agent_type,
          worktree: '/tmp/fake-worktree',
        },
      };
      sessionPlane.add(session);
      return json(session);
    }
    const items = sessionPlane.list();
    return json({ sessions: items, total: items.length });
  }

  // /api/escalations/:id/respond (gm-native.13)
  const respondMatch = path.match(/^\/api\/escalations\/([^/]+)\/respond$/);
  if (respondMatch && method === 'POST') {
    const id = decodeURIComponent(respondMatch[1] ?? '');
    const body = parseBody(route.request().postData());
    escalationPlane.resolve(id, body);
    return json({ id, state: 'resolved' });
  }
  if (isPath(path, '/api/escalations')) {
    // The SPA scopes per-session via ?assignment_id=... so the fake
    // mirrors that filter when present.
    const assignmentId = url.searchParams.get('assignment_id') ?? undefined;
    const items = escalationPlane.list(assignmentId);
    return json({ escalations: items, total: items.length });
  }

  if (isPath(path, '/api/agents')) {
    const items = agentPlane.list();
    return json({ agents: items, total: items.length });
  }
  if (isPath(path, '/api/sprints')) return json({ items: [] });
  if (isPath(path, '/api/capabilities')) return json({});
  if (isPath(path, '/api/adaptors')) return json({ adaptors: adaptorsState.get() });
  if (isPath(path, '/api/health')) return json({ status: 'ok' });

  // Anything else under /api/* — the smoke tier hasn't pinned a
  // shape, so empty-object is enough for rendering.
  return json({});
}

// Matches `${prefix}`, `${prefix}/...`, or `${prefix}?...`.
function isPath(path: string, prefix: string): boolean {
  return path === prefix || path.startsWith(`${prefix}/`) || path.startsWith(`${prefix}?`);
}

// parseBody safely decodes a request's postData. Specs poke the fake
// dispatcher with arbitrary JSON; non-JSON or empty bodies fall back
// to an empty object so the dispatcher can keep its shape-agnostic
// switch tidy.
function parseBody(raw: string | null): Record<string, unknown> {
  if (!raw) return {};
  try {
    const v = JSON.parse(raw);
    return typeof v === 'object' && v !== null ? (v as Record<string, unknown>) : {};
  } catch {
    return {};
  }
}
