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

  // ── Pending — enumerated for visibility, owned by sibling beads ────
  pending('chrome-fake',     { tier: 'chrome',      backend: 'fake', bead: 'gm-5v8v.4'  }),
  pending('route-fake',      { tier: 'route',       backend: 'fake', bead: 'gm-5v8v.5/6/7/8/9' }),
  pending('realtime-fake',   { tier: 'realtime',    backend: 'fake', bead: 'gm-5v8v.10' }),
  pending('modes-fake',      { tier: 'modes',       backend: 'fake', bead: 'gm-5v8v.11' }),
  pending('error-fake',      { tier: 'error',       backend: 'fake', bead: 'gm-5v8v.13' }),

  pending('smoke-deep',      { tier: 'smoke',       backend: 'real', bead: 'gm-5v8v.3'  }),
  pending('chrome-deep',     { tier: 'chrome',      backend: 'real', bead: 'gm-5v8v.4'  }),
  pending('route-deep',      { tier: 'route',       backend: 'real', bead: 'gm-5v8v.5/6/7/8/9' }),
  pending('realtime-deep',   { tier: 'realtime',    backend: 'real', bead: 'gm-5v8v.10' }),
  pending('modes-deep',      { tier: 'modes',       backend: 'real', bead: 'gm-5v8v.11' }),
  pending('error-deep',      { tier: 'error',       backend: 'real', bead: 'gm-5v8v.13' }),
  pending('integration-deep',{ tier: 'integration', backend: 'real', bead: 'gm-5v8v.15' }),
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
