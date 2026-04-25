import { defineConfig, devices, type Project } from '@playwright/test';

// Gemba e2e — see README.md and gm-5v8v (epic).
//
// Project taxonomy: tier × backend.
//
//   tiers:    smoke / chrome / route / realtime / modes / error / integration
//   backends: fake (page.route()-intercepted /api + /events; sub-second resets)
//             deep (real `gemba serve` + Dolt + bd; serialized; nightly)
//
// Only smoke-fake actively runs specs. The remaining projects are
// enumerated up-front so the matrix is visible, but they `testIgnore`
// everything until their child bead lands. Children plug specs in
// by replacing the testMatch / testIgnore on their project.

const baseURL = process.env.GEMBA_E2E_BASE_URL ?? 'http://localhost:5173';

const isCI = !!process.env.CI;

// Placeholder project — enumerated in the matrix but ignores all specs
// until its sibling bead implements the tier. Each child bead in the
// gm-5v8v family replaces the `testMatch` for its project.
function pending(name: string, meta: { tier: string; backend: 'fake' | 'real'; bead: string }): Project {
  return {
    name,
    testIgnore: ['**/*'],
    use: { ...devices['Desktop Chrome'] },
    metadata: { ...meta, status: 'pending' },
  };
}

const projects: Project[] = [
  // ── Active ─────────────────────────────────────────────────────────
  // smoke-fake is the only project running specs as of gm-5v8v.1. It
  // proves the routes load with the fake-backend fixture installed.
  {
    name: 'smoke-fake',
    testMatch: ['smoke/**/*.spec.ts'],
    use: {
      ...devices['Desktop Chrome'],
      // The fake backend fixture reads this header indirectly via
      // process.env; carrying it on requests also lets a future real
      // server distinguish e2e traffic in logs.
      extraHTTPHeaders: { 'x-gemba-e2e': 'fake' },
    },
    metadata: { tier: 'smoke', backend: 'fake', bead: 'gm-5v8v.1', status: 'active' },
    timeout: 30_000,
  },

  // chrome-fake — Sidebar / Topbar / Palette / Hotkeys / HelpOverlay
  // / AdaptorBanner. Activated by gm-5v8v.4. Pure SPA-shell assertions
  // — none of the chrome specs need a real backend, so there is no
  // chrome-deep counterpart yet (the real-backend variant of
  // AdaptorBanner integration belongs in gm-5v8v.10 realtime).
  {
    name: 'chrome-fake',
    testMatch: ['chrome/**/*.spec.ts'],
    use: {
      ...devices['Desktop Chrome'],
      extraHTTPHeaders: { 'x-gemba-e2e': 'fake' },
    },
    metadata: { tier: 'chrome', backend: 'fake', bead: 'gm-5v8v.4', status: 'active' },
    timeout: 30_000,
  },

  // route-fake is the multi-bead route tier slot. Each child bead
  // (.5 board, .6 grid, .7 graph, .8 drawers, .9 sessions) plugs its
  // spec directory in here.
  {
    name: 'route-fake',
    testMatch: [
      'board/**/*.spec.ts',
      'drawers/**/*.spec.ts',
      'graph/**/*.spec.ts',
      'grid/**/*.spec.ts',
      'sessions/**/*.spec.ts',
    ],
    use: {
      ...devices['Desktop Chrome'],
      extraHTTPHeaders: { 'x-gemba-e2e': 'fake' },
    },
    metadata: { tier: 'route', backend: 'fake', bead: 'gm-5v8v.5,6,7,8,9', status: 'active' },
    timeout: 30_000,
  },
  // realtime-fake: AdaptorBanner + adaptors-stream specs (gm-5v8v.10).
  // The SSE consumer chain is exercised in @deep mode against a real
  // server — fake mode covers the snapshot fallback paths.
  {
    name: 'realtime-fake',
    testMatch: ['realtime/**/*.spec.ts'],
    use: {
      ...devices['Desktop Chrome'],
      extraHTTPHeaders: { 'x-gemba-e2e': 'fake' },
    },
    metadata: { tier: 'realtime', backend: 'fake', bead: 'gm-5v8v.10', status: 'active' },
    timeout: 30_000,
  },
  // modes-fake (gm-5v8v.11) — every spec is currently fixme'd because
  // the SPA hasn't shipped workspace-mode-gated UX (no Toaster, no
  // typing-guard, no mode-gated confirmation dialog). The project is
  // active so the contract files surface in playwright reports as
  // skipped rather than vanishing inside testIgnore. Each fixme
  // references the SPA surface that would unblock it.
  {
    name: 'modes-fake',
    testMatch: ['modes/**/*.spec.ts'],
    use: {
      ...devices['Desktop Chrome'],
      extraHTTPHeaders: { 'x-gemba-e2e': 'fake' },
    },
    metadata: { tier: 'modes', backend: 'fake', bead: 'gm-5v8v.11', status: 'active (all fixme)' },
    timeout: 30_000,
  },
  // auth-fake (gm-5v8v.12) — the bead notes auth is a backend
  // concern; the meaningful tests are @deep. Fake mode covers UI-side
  // surfaces only (LoginPage, useConfig hook), neither of which the
  // SPA has shipped today. The project is active so the contract
  // fixmes surface in reports.
  {
    name: 'auth-fake',
    testMatch: ['auth/**/*.spec.ts'],
    use: {
      ...devices['Desktop Chrome'],
      extraHTTPHeaders: { 'x-gemba-e2e': 'fake' },
    },
    metadata: { tier: 'auth', backend: 'fake', bead: 'gm-5v8v.12', status: 'active (all fixme)' },
    timeout: 30_000,
  },
  // error-fake: 404 / network-error / pending. Inline alerts on per-
  // route fetch failures, NotFoundPage for unknown routes, banner
  // independence from per-API errors. gm-5v8v.13.
  {
    name: 'error-fake',
    testMatch: ['error/**/*.spec.ts'],
    use: {
      ...devices['Desktop Chrome'],
      extraHTTPHeaders: { 'x-gemba-e2e': 'fake' },
    },
    metadata: { tier: 'error', backend: 'fake', bead: 'gm-5v8v.13', status: 'active' },
    timeout: 30_000,
  },
  // pending-fake: living specs for ui-spec §5 surfaces that haven't
  // shipped yet (gm-5v8v.14). Every test in this tier is test.skip'd
  // with a tracking note; the project exists so the runner enumerates
  // them in reports rather than the gaps being invisible. When a SPA
  // surface lands, un-skip the corresponding spec file.
  {
    name: 'pending-fake',
    testMatch: ['pending/**/*.spec.ts'],
    use: {
      ...devices['Desktop Chrome'],
      extraHTTPHeaders: { 'x-gemba-e2e': 'fake' },
    },
    metadata: { tier: 'pending', backend: 'fake', bead: 'gm-5v8v.14', status: 'living-spec catalog' },
    timeout: 30_000,
  },

  // smoke-deep is GATED until gm-h4n closes.
  //
  // gm-5v8v.2 landed the deep-mode backend infra and gm-5v8v.3
  // expanded smoke-deep to the full route set. The shared problem:
  // realServer.ts runs `bd init --prefix e2etest --non-interactive
  // --quiet --skip-agents --skip-hooks` inside a tempdir, and bd
  // init does NOT isolate from the shared :3307 Dolt server when
  // there is no existing .beads in cwd (gm-h4n). Every run silently
  // re-poisons the gemba workspace's issue_prefix and can drop rows
  // sibling rigs depend on.
  //
  // The right fix lives in bd (proper isolation in bd init when
  // run outside an existing .beads). Until that ships, this project
  // is pending and the e2e CI lane runs only fake-backed projects.
  // Set GEMBA_E2E_RUN_DEEP=1 to opt in locally for a one-off check
  // — accept the pollution risk, restore from a sibling rig if it
  // bites you.
  ...(process.env.GEMBA_E2E_RUN_DEEP
    ? [
        {
          name: 'smoke-deep',
          testMatch: ['smoke/**/*.spec.ts'],
          use: {
            ...devices['Desktop Chrome'],
            backend: 'real' as const,
            extraHTTPHeaders: { 'x-gemba-e2e': 'real' },
          },
          metadata: {
            tier: 'smoke',
            backend: 'real',
            bead: 'gm-5v8v.3',
            status: 'opt-in (gm-h4n)',
          },
          timeout: 60_000,
          fullyParallel: false,
        } satisfies Project,
      ]
    : [pending('smoke-deep', { tier: 'smoke', backend: 'real', bead: 'gm-h4n' })]),

  // chrome-deep was a pending() placeholder. Chrome specs (sidebar /
  // topbar / palette / hotkeys / help-overlay) are pure SPA-shell —
  // none assert on real backend behavior, so a real-backend variant
  // would just re-run the same fake assertions slower. The
  // AdaptorBanner integration that *would* matter here is already
  // covered by realtime-deep (gm-5v8v.10). Removed per gm-5v8v.18 —
  // matrix-honesty over placeholder coverage.

  // route-deep: drag → PATCH → session POST (board), JSONL import
  // round-trip (grid), bd-create surfaces in drawer/dispatch
  // (drawers/sessions). Each tier's specs already carry @deep tags
  // for the load-bearing assertions. gm-5v8v.18.
  ...(process.env.GEMBA_E2E_RUN_DEEP
    ? [
        {
          name: 'route-deep',
          testMatch: [
            'board/**/*.spec.ts',
            'drawers/**/*.spec.ts',
            'grid/**/*.spec.ts',
            'sessions/**/*.spec.ts',
          ],
          grep: /@deep/,
          use: {
            ...devices['Desktop Chrome'],
            backend: 'real' as const,
            extraHTTPHeaders: { 'x-gemba-e2e': 'real' },
          },
          metadata: {
            tier: 'route',
            backend: 'real',
            bead: 'gm-5v8v.5/6/8/9',
            status: 'opt-in (gm-h4n)',
          },
          timeout: 60_000,
          fullyParallel: false,
        } satisfies Project,
      ]
    : [pending('route-deep', { tier: 'route', backend: 'real', bead: 'gm-5v8v.5/6/8/9' })]),
  // realtime-deep: full /events SSE end-to-end + POST /api/workitems/notify
  // dedupe (gm-5v8v.10). bd write → events.Hub → /events SSE → SPA
  // react-query invalidation. Tagged @deep so the dispatcher only
  // picks up the real-backend specs.
  //
  // GATED behind GEMBA_E2E_RUN_DEEP — matches smoke-deep convention.
  // realServer.ts's tempdir bd init has a HOME-isolation guard
  // (gm-h4n) that prevents shared :3307 leakage in this fixture, but
  // until bd itself defends against fall-through (the upstream fix),
  // the e2e CI lane stays on fake-only by default. Set
  // GEMBA_E2E_RUN_DEEP=1 to opt in locally.
  ...(process.env.GEMBA_E2E_RUN_DEEP
    ? [
        {
          name: 'realtime-deep',
          testMatch: ['realtime/**/*.spec.ts'],
          grep: /@deep/,
          use: {
            ...devices['Desktop Chrome'],
            backend: 'real' as const,
            extraHTTPHeaders: { 'x-gemba-e2e': 'real' },
          },
          metadata: {
            tier: 'realtime',
            backend: 'real',
            bead: 'gm-5v8v.10',
            status: 'opt-in (gm-h4n)',
          },
          timeout: 60_000,
          fullyParallel: false,
        } satisfies Project,
      ]
    : [pending('realtime-deep', { tier: 'realtime', backend: 'real', bead: 'gm-5v8v.10' })]),
  // modes-deep: workspace mode confirmation UX against a real server's
  // X-GEMBA-Confirm nonce + audit trail. modes/* specs carry @deep
  // tags for the load-bearing mode-respect assertions. gm-5v8v.18.
  ...(process.env.GEMBA_E2E_RUN_DEEP
    ? [
        {
          name: 'modes-deep',
          testMatch: ['modes/**/*.spec.ts'],
          grep: /@deep/,
          use: {
            ...devices['Desktop Chrome'],
            backend: 'real' as const,
            extraHTTPHeaders: { 'x-gemba-e2e': 'real' },
          },
          metadata: {
            tier: 'modes',
            backend: 'real',
            bead: 'gm-5v8v.11',
            status: 'opt-in (gm-h4n)',
          },
          timeout: 60_000,
          fullyParallel: false,
        } satisfies Project,
      ]
    : [pending('modes-deep', { tier: 'modes', backend: 'real', bead: 'gm-5v8v.11' })]),
  // auth-deep: token / cookie / middleware enforcement. Auth is
  // backend-only by definition — fake-mode auth specs cover the
  // login-form UI shape, deep-mode covers the bearer/cookie
  // contract that lives in chi auth middleware. gm-5v8v.18.
  ...(process.env.GEMBA_E2E_RUN_DEEP
    ? [
        {
          name: 'auth-deep',
          testMatch: ['auth/**/*.spec.ts'],
          grep: /@deep/,
          use: {
            ...devices['Desktop Chrome'],
            backend: 'real' as const,
            extraHTTPHeaders: { 'x-gemba-e2e': 'real' },
          },
          metadata: {
            tier: 'auth',
            backend: 'real',
            bead: 'gm-5v8v.12',
            status: 'opt-in (gm-h4n)',
          },
          timeout: 60_000,
          fullyParallel: false,
        } satisfies Project,
      ]
    : [pending('auth-deep', { tier: 'auth', backend: 'real', bead: 'gm-5v8v.12' })]),
  // error-deep: real-server JSON envelope contract for /api/* +
  // /events/* (gm-b2 / gm-xke). Tagged @deep so only the load-bearing
  // server-contract specs run here. Same gm-h4n gating as the rest of
  // the deep matrix.
  ...(process.env.GEMBA_E2E_RUN_DEEP
    ? [
        {
          name: 'error-deep',
          testMatch: ['error/**/*.spec.ts'],
          grep: /@deep/,
          use: {
            ...devices['Desktop Chrome'],
            backend: 'real' as const,
            extraHTTPHeaders: { 'x-gemba-e2e': 'real' },
          },
          metadata: {
            tier: 'error',
            backend: 'real',
            bead: 'gm-5v8v.13',
            status: 'opt-in (gm-h4n)',
          },
          timeout: 60_000,
          fullyParallel: false,
        } satisfies Project,
      ]
    : [pending('error-deep', { tier: 'error', backend: 'real', bead: 'gm-5v8v.13' })]),
  // integration-deep — the migrated dispatch-chain spec (gm-5v8v.15).
  // Same opt-in gate as smoke-deep: GEMBA_E2E_RUN_DEEP=1 flips it on.
  // The CI nightly lane sets that flag; PR lanes don't run integration
  // tests because each one boots a real gemba serve + spawns a pane.
  ...(process.env.GEMBA_E2E_RUN_DEEP
    ? [
        {
          name: 'integration-deep',
          testMatch: ['integration/**/*.spec.ts'],
          use: {
            ...devices['Desktop Chrome'],
            backend: 'real' as const,
            extraHTTPHeaders: { 'x-gemba-e2e': 'real' },
          },
          metadata: {
            tier: 'integration',
            backend: 'real',
            bead: 'gm-5v8v.15',
            status: 'opt-in (gm-h4n)',
          },
          // Dispatch chain end-to-end (drag → PATCH → session POST →
          // tmux spawn → optional agent evidence) needs a generous
          // budget; the agent half can take 3 minutes by itself.
          timeout: 240_000,
          fullyParallel: false,
        } satisfies Project,
      ]
    : [pending('integration-deep', { tier: 'integration', backend: 'real', bead: 'gm-h4n' })]),
];

export default defineConfig({
  testDir: './specs',
  fullyParallel: true,
  forbidOnly: isCI,
  retries: isCI ? 1 : 0,
  workers: isCI ? 4 : undefined,
  reporter: isCI
    ? [['list'], ['html', { open: 'never', outputFolder: 'playwright-report' }], ['github']]
    : [['list'], ['html', { open: 'never', outputFolder: 'playwright-report' }]],
  outputDir: 'test-results',
  use: {
    baseURL,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',
  },
  // Boot the SPA dev server unless the operator points us at an
  // already-running instance via GEMBA_E2E_BASE_URL.
  webServer: process.env.GEMBA_E2E_NO_WEBSERVER
    ? undefined
    : {
        command: 'pnpm --filter gemba-web dev',
        cwd: '../../',
        url: baseURL,
        reuseExistingServer: !isCI,
        stdout: 'pipe',
        stderr: 'pipe',
        timeout: 60_000,
      },
  projects,
});
