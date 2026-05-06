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
import { spinRealServer, type AuthMode, type RealServer, type WorkspaceMode } from './realServer';

/**
 * Options for the per-test authServer factory (gm-5v8v.12.1). Mirrors
 * the parts of SpinOptions auth specs care about; everything else
 * defaults from the fixture.
 *
 * gm-5v8v.11.1: `mode` is plumbed here too so deep-mode specs in
 * specs/modes/** can drive the same factory rather than introducing a
 * parallel modeServer one. Naming stays `authServer` because every
 * surface a per-test gemba spawn cares about (auth, listen, mode,
 * yolo) is a knob on the same one server — keeping a single factory
 * keeps spec teardown predictable.
 */
export type AuthServerOptions = {
  auth?: AuthMode;
  listen?: string;
  dangerouslySkipPermissions?: boolean;
  expectBootFailure?: boolean;
  /**
   * Workspace mode (gm-5v8v.11.1). When set, the fixture writes
   * `.gemba/workspace.toml` with `mode = "<value>"` BEFORE spawning
   * gemba so future workspace-config readers see a stable mode at
   * boot. Specs that don't pass `mode` get the pre-gm-5v8v.11.1
   * layout (no .gemba/ in the tempdir).
   */
  mode?: WorkspaceMode;
  /** Start the per-test server in explicit --beads-read-only mode. */
  beadsReadOnly?: boolean;
  /** Extra serve argv to append for one-off deep server contracts. */
  serveArgs?: string[];
  /** Hook for seeding the isolated workspace before gemba serve starts. */
  beforeServe?: (workspaceDir: string, env: NodeJS.ProcessEnv) => void;
};
import { BdClient } from './bdClient';
import { createWorkPlane, type WorkPlaneStore } from './workplane';
import { createSessionStore, type SessionStore } from './sessionStore';
import { createEscalationStore, type EscalationStore } from './escalationStore';
import { createAgentStore, type AgentStore } from './agentStore';
import { createCapabilitiesPlane, type CapabilitiesPlane } from './capabilitiesPlane';
import { createPoolsPlane, type PoolConfigJSON, type PoolsPlane } from './poolsPlane';
import { createModeHandle, DEFAULT_MODE, type ModeHandle } from './modes';
import { createAuthHandle, type AuthHandle } from './auth';
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
   * Per-test in-memory Projects store. gm-root.18. Specs call
   * projectsStore.seed([...]) to populate the project list before
   * navigating; the fake dispatcher serves /api/v1/projects (GET) and
   * /api/v1/projects/switch (POST) from this store. Empty by default
   * so the picker renders its empty state in most tests.
   */
  projectsStore: ProjectsStore;
  /**
   * Per-test capabilities envelope (gm-5v8v.7/.8 follow-up). Specs
   * that need the SPA to see a non-empty WorkPlane / OrchestrationPlane
   * manifest call capabilitiesPlane.set({...}) or the typed
   * setWorkPlane / setOrchestrationPlane helpers BEFORE navigating.
   * The fake-backend dispatcher serves /api/capabilities from this.
   */
  capabilitiesPlane: CapabilitiesPlane;
  /**
   * Per-test pool dispatch editor state (gm-s47n.18). Drives
   * /api/pool-config (GET/PUT), /api/personas, and
   * /api/orchestration/state for the /settings/pools editor.
   */
  poolsPlane: PoolsPlane;
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
  /**
   * Auth handle (gm-5v8v.12). Specs in specs/auth/** call setBearer
   * / loginAs to drive the bearer-or-cookie middleware paths. Most
   * meaningful auth tests are @deep — fake mode covers UI-side
   * surfaces only. The SPA has no login form yet, so today's auth
   * specs are largely fixme contracts.
   */
  auth: AuthHandle;
  /**
   * Per-test factory for spinning a real `gemba serve` with custom
   * auth / listen / yolo options (gm-5v8v.12.1). Each call creates a
   * fresh isolated server; the fixture disposes them all at teardown.
   * Specs that need a token-mode server use this instead of the
   * worker-scoped realServer because each auth test wants its own
   * config (different token, different mode, expected boot failure).
   */
  authServer: (opts?: AuthServerOptions) => Promise<RealServer>;
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

  // baseURL override (gm-e5b / gm-j51y follow-up). In deep mode the
  // per-worker realServer listens on a random port, but the project-
  // level baseURL is the Vite dev server (localhost:5173). Without
  // this override every page.goto('/foo') would resolve against Vite,
  // whose /api/* proxy targets the default gemba port — not the per-
  // worker one — so every API call would 500. Threading baseURL
  // through as a fixture makes relative gotos resolve to the right
  // host. Fake mode keeps the project default (Vite serves the SPA;
  // /api requests are page.route()-intercepted before Vite's proxy
  // can see them).
  baseURL: async ({ baseURL, backend, realServer }, use) => {
    if (backend === 'real' && realServer) {
      await use(realServer.baseURL);
    } else {
      await use(baseURL);
    }
  },

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

  capabilitiesPlane: async ({}, use) => {
    const plane = createCapabilitiesPlane();
    await use(plane);
  },

  poolsPlane: async ({}, use) => {
    const plane = createPoolsPlane();
    await use(plane);
  },

  adaptorsState: async ({}, use) => {
    const state = createAdaptorsState();
    await use(state);
  },

  projectsStore: async ({}, use) => {
    const store = createProjectsStore();
    await use(store);
  },

  mode: async ({}, use) => {
    // Specs override the initial mode by setting WorkspaceMode on
    // test.use({ mode: createModeHandle('managed') }) in the spec
    // file's describe block, OR by mutating the handle inside a
    // beforeEach. Default mirrors ui-spec §6.2.
    const handle = createModeHandle(DEFAULT_MODE);
    await use(handle);
  },

  authServer: async ({}, use, testInfo) => {
    // gm-5v8v.12.1 — factory that spins a fresh isolated gemba serve
    // per-call. Specs call it inside a test for the exact auth /
    // listen / yolo combination they need; this fixture batches
    // teardown so we never leak processes when a spec fails midway.
    const spawned: RealServer[] = [];
    await use(async (opts: AuthServerOptions = {}) => {
      const server = await spinRealServer({
        workerIndex: testInfo.workerIndex,
        ...opts,
      });
      spawned.push(server);
      return server;
    });
    for (const s of spawned) {
      try { await s.dispose(); } catch { /* best-effort */ }
    }
  },

  auth: async ({}, use) => {
    // Per-test auth state. Anonymous by default; specs that drive
    // the token / cookie / loopback paths (specs/auth/**) call
    // setBearer / loginAs explicitly.
    const handle = createAuthHandle();
    await use(handle);
  },

  page: async (
    { page, backend, realServer, workPlane, sessionPlane, escalationPlane, agentPlane, adaptorsState, capabilitiesPlane, projectsStore, poolsPlane },
    use
  ) => {
    if (backend === 'fake') {
      await installFakeBackend(page, {
        workPlane,
        sessionPlane,
        escalationPlane,
        agentPlane,
        adaptorsState,
        capabilitiesPlane,
        projectsStore,
        poolsPlane,
      });
    } else {
      if (!realServer) {
        throw new Error('backend=real but realServer fixture is undefined');
      }
      // realServer.baseURL is now threaded through the `baseURL`
      // fixture above, so page.goto('/foo') resolves against the
      // per-worker gemba listener automatically. Nothing to do here.
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

// ── ProjectsStore fixture (gm-root.18) ──────────────────────────────────
//
// Drives /api/v1/projects (GET) and /api/v1/projects/switch (POST).
// Specs seed the project list before navigating; the picker renders
// against it without needing a real ~/.gemba directory.
// gm-root.17.14: FakeProject carries a `kind` classifier mirroring
// the server's ProjectKind type. Unset entries default to "complete"
// in the dispatcher so legacy seeds (no kind) keep working.
export type FakeProjectKind = 'complete' | 'needs_workspace' | 'needs_repo';

export type FakeProject = {
  name: string;
  path: string;
  active?: boolean;
  kind?: FakeProjectKind;
};

export type ProjectsStore = {
  seed(projects: FakeProject[]): void;
  list(): FakeProject[];
  setActive(name: string): void;
  getActive(): FakeProject | undefined;
  // gm-root.17.14: mark an entry complete after a successful bind.
  // Looks the entry up by beads_db_path (== entry.path in the seed).
  markCompleteByPath(path: string): FakeProject | undefined;
};

function createProjectsStore(): ProjectsStore {
  let projects: FakeProject[] = [];
  return {
    seed: (next) => {
      projects = next.map((p) => ({ ...p }));
    },
    list: () => projects,
    setActive: (name) => {
      projects = projects.map((p) => ({ ...p, active: p.name === name }));
    },
    getActive: () => projects.find((p) => p.active),
    markCompleteByPath: (path) => {
      let updated: FakeProject | undefined;
      projects = projects.map((p) => {
        if (p.path === path) {
          const next = { ...p, kind: 'complete' as const };
          updated = next;
          return next;
        }
        return p;
      });
      return updated;
    },
  };
}

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
  capabilitiesPlane: CapabilitiesPlane;
  projectsStore: ProjectsStore;
  poolsPlane: PoolsPlane;
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
  const { workPlane, sessionPlane, escalationPlane, agentPlane, adaptorsState, capabilitiesPlane, projectsStore, poolsPlane } = stores;
  const url = new URL(route.request().url());
  const path = url.pathname;
  const method = route.request().method();
  const json = (data: unknown) => route.fulfill({ json: data });
  const readOnly = () =>
    route.fulfill({
      status: 405,
      json: {
        error: 'read_only',
        message: 'beads-read-only mode blocks Beads mutations',
      },
    });
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
      if (isBeadsReadOnly(capabilitiesPlane)) return readOnly();
      // Mutations: echo the seeded item back so optimistic update
      // settles. A richer fake (apply patch to store) is a follow-up.
      const existing = workPlane.get(id);
      if (existing) {
        const body = parseBody(route.request().postData());
        const updated = workPlane.update(id, body as Partial<WorkItem>) ?? existing;
        if (capabilitiesPlane.get().beads_only) {
          const beforeState = existing.state_category;
          const afterState = updated.state_category;
          const action = beforeState !== afterState ? 'work_item.state_changed' : 'work_item.edited';
          workPlane.addHistory({
            event_id: `evt-fake-${workPlane.history().length + 1}`,
            occurred_at: new Date().toISOString(),
            actor: 'fake',
            mode: 'beads_only',
            action,
            entity: { type: updated.kind === 'epic' ? 'epic' : updated.kind === 'milestone' ? 'milestone' : 'bead', id: updated.id, title: updated.title },
            before: { state_category: beforeState },
            after: { state_category: afterState },
            summary: beforeState !== afterState
              ? `Moved "${updated.title}" from ${beforeState} to ${afterState}.`
              : `Edited "${updated.title}".`,
          });
        }
        return json(updated);
      }
      return notFound('session_not_found', `work item ${id} not found`);
    }
    if (route.request().method() === 'DELETE') {
      if (isBeadsReadOnly(capabilitiesPlane)) return readOnly();
      const deleted = workPlane.remove(id);
      if (!deleted) return notFound('session_not_found', `work item ${id} not found`);
      if (capabilitiesPlane.get().beads_only) {
        workPlane.addHistory({
          event_id: `evt-fake-${workPlane.history().length + 1}`,
          occurred_at: new Date().toISOString(),
          actor: 'fake',
          mode: 'beads_only',
          action: 'work_item.deleted',
          entity: { type: deleted.kind === 'epic' ? 'epic' : deleted.kind === 'milestone' ? 'milestone' : 'bead', id: deleted.id, title: deleted.title },
          before: { state_category: deleted.state_category, status: deleted.status },
          summary: `Deleted "${deleted.title}".`,
        });
      }
      return json(deleted);
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
      if (isBeadsReadOnly(capabilitiesPlane)) return readOnly();
      // Pretend the create succeeded with a synthetic id; specs that
      // assert on persistence tag themselves @deep so this branch
      // only runs in fake mode.
      const body = parseBody(route.request().postData()) as { item?: Partial<WorkItem> };
      const input = body.item ?? {};
      const fake: WorkItem = {
        id: `fake-${items.length + 1}`,
        kind: typeof input.kind === 'string' ? input.kind : 'task',
        title: typeof input.title === 'string' ? input.title : 'fake-created',
        status: typeof input.status === 'string' ? input.status : 'open',
        state_category: input.state_category ?? 'unstarted',
        labels: input.labels,
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
      };
      workPlane.add(fake);
      if (capabilitiesPlane.get().beads_only) {
        const entityType = fake.kind === 'epic' ? 'epic' : fake.kind === 'milestone' ? 'milestone' : fake.kind === 'decision' ? 'decision' : 'bead';
        workPlane.addHistory({
          event_id: `evt-fake-${workPlane.history().length + 1}`,
          occurred_at: new Date().toISOString(),
          actor: 'fake',
          mode: 'beads_only',
          action: entityType === 'milestone' ? 'milestone.created' : entityType === 'epic' ? 'epic.created' : entityType === 'decision' ? 'decision.created' : 'work_item.created',
          entity: { type: entityType, id: fake.id, title: fake.title },
          after: { state_category: fake.state_category },
          summary: `Created ${entityType} "${fake.title}".`,
        });
      }
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
    // The SPA scopes per-session via ?session_id=... (gm-e11.8.6).
    // The real Go server reads session_id and routes through
    // ListPendingRequests; the fake mirrors that contract.
    // escalationPlane.list(sessionId) internally filters by
    // e.assignment_id === sessionId — escalations are stored with
    // assignment_id as the owning-session field, but the HTTP param
    // name is session_id (matching the ListPendingRequests call path).
    const sessionId = url.searchParams.get('session_id') ?? undefined;
    const items = escalationPlane.list(sessionId);
    return json({ escalations: items, total: items.length });
  }

  if (isPath(path, '/api/agents')) {
    const items = agentPlane.list();
    return json({ agents: items, total: items.length });
  }

  // /api/consults — POST creates a consult (gm-e11.8.4 hand-off). Return a
  // canned ConsultSummary so the SPA's mutation resolves successfully.
  if (isPath(path, '/api/consults') && method === 'POST') {
    const body = parseBody(route.request().postData());
    return json({
      id: `consult-fake-${Date.now()}`,
      persona_id: typeof body.persona_id === 'string' ? body.persona_id : 'coach',
      skill_id: typeof body.skill_id === 'string' ? body.skill_id : 'escalation_handoff',
      workspace: typeof body.workspace === 'string' ? body.workspace : 'default',
      status: 'running',
      started_at: new Date().toISOString(),
      line_count: 0,
      line_error_count: 0,
    });
  }
  // GET /api/consults — return empty list.
  if (isPath(path, '/api/consults')) return json({ consults: [], total: 0 });

  // GET /api/v1/personas (gm-e11.8.7) — canned roster so the
  // EscalationsPage Hand-off modal renders a populated dropdown in
  // fake mode. Mirrors the personaSummary wire shape from
  // internal/server/personas.go.
  if (isPath(path, '/api/v1/personas')) {
    return json({
      personas: [
        {
          id: 'project-manager',
          name: 'Project Manager',
          role: 'Project Manager',
          variety: 'coach',
          scope: { kind: 'project' },
          description: 'Default fake-mode PM persona for hand-off tests.',
          skills: ['epic_order', 'escalation_handoff'],
        },
        {
          id: 'documentarian',
          name: 'Documentarian',
          role: 'Docs',
          variety: 'coach',
          scope: { kind: 'project' },
          description: 'Default fake-mode Docs persona for hand-off tests.',
          skills: ['escalation_handoff'],
        },
      ],
      total: 2,
    });
  }

  if (isPath(path, '/api/sprints')) return json({ items: [] });
  if (isPath(path, '/api/repositories')) {
    // gm-uipx.8: project-config Workspace-repos section. Fake mode
    // mirrors the real backend's empty + notice payload so the SPA
    // renders the helpful empty state.
    return json({
      repositories: [],
      notice: 'no .gemba/repositories/ directory in workspace; configure repositories there',
    });
  }
  // /api/bootstrap/* (gm-uipx.7). The /bootstrap wizard SPA hits
  // start → progress → plan → commit. Backend handlers are not yet
  // wired (filed as a follow-up); this fake dispatcher returns a
  // canned analysis run so the e2e suite proves the SPA flow end-to-
  // end without the backend. Real-mode coverage lands when the
  // backend bead does.
  if (path === '/api/bootstrap/start' && method === 'POST') {
    return json({ analysis_id: 'fake-analysis-1' });
  }
  if (path === '/api/bootstrap/progress') {
    // One-shot terminal frame: tests run against a "done" analysis so
    // the SPA's continue button enables immediately. Real-mode tests
    // exercise multi-frame streaming.
    return json({
      frames: [
        { seq: 1, line: 'Scanning 247 issues…', at: new Date().toISOString() },
        { seq: 2, line: 'Analyzing module structure…', at: new Date().toISOString() },
        {
          seq: 3,
          line: 'Generating epic decomposition…',
          at: new Date().toISOString(),
          done: true,
        },
      ],
    });
  }
  if (path === '/api/bootstrap/plan') {
    return json({
      analysis_id: 'fake-analysis-1',
      source: 'fresh',
      plan: [
        {
          id: 'epic-1',
          kind: 'epic',
          title: 'Onboard the team',
          detail: 'Initial configuration milestone.',
          children: [
            { id: 'milestone-1', kind: 'milestone', title: 'M1 — define values' },
            { id: 'milestone-2', kind: 'milestone', title: 'M2 — wire adaptors' },
          ],
        },
        { id: 'sprint-1', kind: 'sprint', title: 'Sprint 1' },
      ],
      findings: [
        { id: 'f1', level: 'pass', title: 'Project skeleton is well-formed' },
        {
          id: 'f2',
          level: 'warn',
          title: 'No owner set for Sprint 1',
          detail: 'Assign an owner via the Personas section after ratify.',
        },
      ],
    });
  }
  if (path === '/api/bootstrap/commit' && method === 'POST') {
    return json({ project_path: '.gemba/project.toml', board_url: '/board' });
  }

  // /api/v1/newproject/* (gm-root.17.3 — see docs/design/newproject.md).
  // The /onboard SPA route now runs deterministic setup before the
  // conversational Onboarder session. Fake mode returns canned
  // responses so route specs can prove the UI flow without requiring
  // a real LLM, gh, gt, GitNexus, or MCP binaries.
  if (path === '/api/v1/onboarding/setup' && method === 'POST') {
    const body = parseBody(route.request().postData());
    const projectName = typeof body.project_name === 'string' ? body.project_name : 'fake-new-project';
    const worktree = typeof body.worktree_path === 'string' ? body.worktree_path : `/tmp/fake-projects/${projectName}`;
    const sourceAnalysis =
      body.source_analysis_tool === 'none' ? 'skipped' : 'configured';
    return json({
      setup_id: 'fake-setup-1',
      project_path: worktree,
      frames: [
        {
          seq: 1,
          line: `Prepared deterministic setup for ${projectName}.`,
          level: 'info',
          done: true,
        },
        {
          seq: 2,
          line: 'Setup complete. The Onboarder can now coach milestones, epics, and beads with this context fixed.',
          level: 'info',
          done: true,
        },
      ],
      checks: {
        source_analysis: sourceAnalysis,
        gemba_mcp: 'verified',
      },
    });
  }
  if (path === '/api/v1/newproject/start' && method === 'POST') {
    return json({
      session_id: 'fake-newproject-1',
      state: emptyNewProjectState(),
      greeting:
        "Hi — what are we building? Tell me the rough shape (web app, library, ops tool, …) and I'll propose milestones, epics, and beads.",
    });
  }
  const npTurn = path.match(/^\/api\/v1\/newproject\/([^/]+)\/turn$/);
  if (npTurn && method === 'POST') {
    const body = parseBody(route.request().postData());
    const userMsg =
      typeof body.message === 'string' ? body.message : '';
    // Synthesize a deterministic plan tree on the first turn so the
    // route's empty-states transition into populated panes. Subsequent
    // turns mutate fields so e2e specs can exercise the diff badge.
    const next = nextFakeState(userMsg);
    const milestones = next.Milestones;
    const epicCount = milestones.reduce((a, m) => a + m.Epics.length, 0);
    return json({
      state: next,
      reply:
        milestones.length === 0
          ? "Okay — tell me a bit more about the project's shape and I'll start drafting milestones."
          : `Drafted ${milestones.length} milestone(s) and ${epicCount} epic(s). Edit anything in the preview pane and I'll fold your changes into the next turn.`,
      reply_id: `fake-reply-${Date.now()}`,
      reply_at: new Date().toISOString(),
    });
  }
  const npRatify = path.match(/^\/api\/v1\/newproject\/([^/]+)\/ratify$/);
  if (npRatify && method === 'POST') {
    // Parse the committed state so we can echo back accurate counts.
    // The body carries { state: FakeNewProjectState }; fall back to
    // defaults if parsing fails so the ratify response is always
    // well-formed.
    const body = parseBody(route.request().postData());
    const committedState =
      body.state && typeof body.state === 'object'
        ? (body.state as FakeNewProjectState)
        : null;
    const milestoneCount = committedState?.Milestones?.length ?? 1;
    const epicCount = committedState?.Milestones?.reduce(
      (a: number, m: FakeMilestone) => a + (m.Epics?.length ?? 0),
      0
    ) ?? 1;
    const projectName = committedState?.ProjectName || 'fake-new-project';
    return json({
      project_path: `/tmp/fake-projects/${projectName.replace(/\s+/g, '-')}`,
      // Per design (gm-root.17.7): the SPA no longer reads next_url
      // — the RatifyDoneScreen owns navigation. The field is kept in
      // the response for forward compatibility.
      project_name: projectName,
      milestone_count: milestoneCount,
      epic_count: epicCount,
    });
  }

  // Pool Dispatch editor (gm-s47n.18). The full editor lives at
  // /settings/pools and reads three endpoints; the spec seeds via
  // poolsPlane and asserts the round-trip via PUT echo.
  if (isPath(path, '/api/pool-config')) {
    if (method === 'PUT') {
      const body = parseBody(route.request().postData()) as unknown as PoolConfigJSON;
      const next = poolsPlane.applyPut(body);
      return json(next);
    }
    return json(poolsPlane.getConfig());
  }
  if (isPath(path, '/api/personas')) {
    return json({ personas: poolsPlane.getPersonas(), dir: '/.gemba/personas' });
  }
  if (isPath(path, '/api/orchestration/state')) {
    const s = poolsPlane.getOrchState();
    if (s) return json(s);
    return route.fulfill({
      status: 404,
      json: { error: 'orchestration_not_configured', message: 'no orchestration plane bound' },
    });
  }
  // gm-s47n.17 added a SchedulerConfig surface (gastown-only). The
  // pool editor reads it on mount when adaptor_id=gastown; an empty
  // shape is enough for fake-mode renders.
  if (isPath(path, '/api/orchestration/scheduler-config')) {
    return json({ max_polecats: 0 });
  }

  if (isPath(path, '/api/beads-history')) {
    return json({ mode: capabilitiesPlane.get().beads_only ? 'beads_only' : 'full', entries: workPlane.history(), malformed: 0 });
  }
  if (isPath(path, '/api/beads/health')) {
    const caps = capabilitiesPlane.get();
    const source = caps.beads_source ?? { kind: 'project-dir', label: 'fake-beads', detail: '/tmp/fake' };
    const adaptor = adaptorsState.get().find((a) => a.plane === 'work');
    return json({
      source,
      current_db: source.label ?? source.kind,
      remote_configured: source.kind === 'dolt-url',
      remote_kind: source.kind === 'dolt-url' ? 'Dolt URL' : 'Local worktree',
      remote_status_label: source.kind === 'dolt-url' ? 'Remote configured' : 'Local DB',
      adaptor,
      actions: [
        { id: 'refresh', label: 'Refresh health', description: 'Re-run the bound Beads adaptor health probe.' },
        { id: 'dolt-test', label: 'Test Dolt connection', description: 'Run bd dolt test in the Beads worktree.' },
      ],
    });
  }
  if (isPath(path, '/api/beads/health/actions')) {
    const caps = capabilitiesPlane.get();
    const source = caps.beads_source ?? { kind: 'project-dir', label: 'fake-beads', detail: '/tmp/fake' };
    const adaptor = adaptorsState.get().find((a) => a.plane === 'work');
    const body = parseBody(route.request().postData());
    return json({
      source,
      current_db: source.label ?? source.kind,
      remote_configured: source.kind === 'dolt-url',
      remote_kind: source.kind === 'dolt-url' ? 'Dolt URL' : 'Local worktree',
      remote_status_label: source.kind === 'dolt-url' ? 'Remote configured' : 'Local DB',
      adaptor,
      actions: [{ id: 'refresh', label: 'Refresh health', description: 'Re-run the bound Beads adaptor health probe.' }],
      last_action: {
        action: typeof body.action === 'string' ? body.action : 'refresh',
        ok: true,
        message: 'Health probe refreshed.',
      },
    });
  }
  if (isPath(path, '/api/capabilities')) return json(capabilitiesPlane.get());
  if (isPath(path, '/api/adaptors')) return json({ adaptors: adaptorsState.get() });
  if (isPath(path, '/api/health')) return json({ status: 'ok' });

  // /api/v1/projects (gm-root.18) — project picker list + switch.
  if (path === '/api/v1/projects/switch' && method === 'POST') {
    const body = parseBody(route.request().postData());
    const name = typeof body.name === 'string' ? body.name : undefined;
    const reqPath = typeof body.path === 'string' ? body.path : undefined;
    const projects = projectsStore.list();
    const target = projects.find(
      (p) => (name && p.name === name) || (reqPath && p.path === reqPath)
    );
    if (!target) {
      return route.fulfill({ status: 404, json: { error: 'project_not_found', message: 'not found' } });
    }
    projectsStore.setActive(target.name);
    return json({
      active: { ...target, active: true, kind: target.kind ?? 'complete' },
    });
  }
  // gm-root.17.14: POST /api/v1/projects/bind. Echoes the contract's
  // request body shape, marks the seeded entry complete, and returns
  // the now-complete ProjectEntry so the SPA can switch to it.
  if (path === '/api/v1/projects/bind' && method === 'POST') {
    const body = parseBody(route.request().postData());
    const beadsDBPath =
      typeof body.beads_db_path === 'string' ? body.beads_db_path : '';
    if (!beadsDBPath) {
      return route.fulfill({
        status: 400,
        json: { error: 'validation', message: 'beads_db_path is required' },
      });
    }
    const updated = projectsStore.markCompleteByPath(beadsDBPath);
    if (!updated) {
      return route.fulfill({
        status: 404,
        json: { error: 'project_not_found', message: 'project not found' },
      });
    }
    return json({ ...updated, kind: 'complete' });
  }
  if (isPath(path, '/api/v1/projects')) {
    const list = projectsStore
      .list()
      .map((p) => ({ ...p, kind: p.kind ?? 'complete' }));
    return json({ projects: list, total: list.length });
  }

  // Anything else under /api/* — the smoke tier hasn't pinned a
  // shape, so empty-object is enough for rendering.
  return json({});
}

function isBeadsReadOnly(capabilitiesPlane: CapabilitiesPlane): boolean {
  const caps = capabilitiesPlane.get();
  return Boolean(caps.beads_read_only || caps.work_plane?.read_only);
}

// Matches `${prefix}`, `${prefix}/...`, or `${prefix}?...`.
function isPath(path: string, prefix: string): boolean {
  return path === prefix || path.startsWith(`${prefix}/`) || path.startsWith(`${prefix}?`);
}

// parseBody safely decodes a request's postData. Specs poke the fake
// dispatcher with arbitrary JSON; non-JSON or empty bodies fall back
// to an empty object so the dispatcher can keep its shape-agnostic
// switch tidy.
// FakeBead / FakeEpic / FakeMilestone / FakeNewProjectState mirror
// the wire shapes in web/src/api/newproject.ts. Duplicated here so
// the fixture stays free of a cross-package import on the SPA's
// types.
interface FakeBead {
  Title: string;
  Description: string;
  Type: string;
  Acceptance: string;
  Labels: string[];
  Priority: number;
  Estimate: number;
  Skills: string[];
  DesignNotes: string;
  Notes: string;
  DependsOnRefs: string[];
  BlocksRefs: string[];
}
interface FakeEpic {
  Title: string;
  Description: string;
  Acceptance: string;
  Labels: string[];
  Priority: number;
  Estimate: number;
  Skills: string[];
  DesignNotes: string;
  Notes: string;
  Beads: FakeBead[];
}
interface FakeMilestone {
  Title: string;
  Description: string;
  Acceptance: string;
  Labels: string[];
  Priority: number;
  Estimate: number;
  Skills: string[];
  DesignNotes: string;
  Notes: string;
  Epics: FakeEpic[];
}
interface FakeNewProjectState {
  ProjectName: string;
  Description: string;
  TechStack: string[];
  Architecture: string;
  Milestones: FakeMilestone[];
  DraftProjectMD: string;
  Turn: number;
  LastChange: { path: string; kind: string; summary: string };
}

// emptyNewProjectState mirrors EMPTY_STATE in web/src/api/newproject.ts.
// Kept as a function (not a const) so each /start call hands back a
// fresh object rather than a shared reference.
function emptyNewProjectState(): FakeNewProjectState {
  return {
    ProjectName: '',
    Description: '',
    TechStack: [],
    Architecture: '',
    Milestones: [],
    DraftProjectMD: '',
    Turn: 0,
    LastChange: { path: '', kind: '', summary: '' },
  };
}

// nextFakeState seeds a deterministic plan tree on first contact so
// the /new route's empty-states transition into populated panes
// without the backend wired. Subsequent turns mutate the description
// so the diff badge has something to surface.
function nextFakeState(userMsg: string): FakeNewProjectState {
  const desc = userMsg || 'A new project.';
  return {
    ProjectName: 'fake-new-project',
    Description: desc,
    TechStack: ['typescript', 'go'],
    Architecture: 'Single-binary backend + SPA.',
    DraftProjectMD: `# fake-new-project\n\n${desc}\n`,
    Turn: 1,
    LastChange: {
      path: '',
      kind: 'edited',
      summary: 'Drafted initial milestones from the conversation.',
    },
    Milestones: [
      {
        Title: 'M1 — bootstrap',
        Description: 'Stand up the workspace skeleton.',
        Acceptance: 'Workspace renders and has a Board.',
        Labels: [],
        Priority: 1,
        Estimate: 0,
        Skills: [],
        DesignNotes: '',
        Notes: '',
        Epics: [
          {
            Title: 'Set up repo',
            Description: 'git init + .gemba/',
            Acceptance: '',
            Labels: [],
            Priority: 1,
            Estimate: 0,
            Skills: [],
            DesignNotes: '',
            Notes: '',
            Beads: [
              {
                Title: 'Create initial commit',
                Description: '',
                Type: 'task',
                Acceptance: '',
                Labels: [],
                Priority: 2,
                Estimate: 0,
                Skills: [],
                DesignNotes: '',
                Notes: '',
                DependsOnRefs: [],
                BlocksRefs: [],
              },
            ],
          },
        ],
      },
    ],
  };
}

function parseBody(raw: string | null): Record<string, unknown> {
  if (!raw) return {};
  try {
    const v = JSON.parse(raw);
    return typeof v === 'object' && v !== null ? (v as Record<string, unknown>) : {};
  } catch {
    return {};
  }
}
