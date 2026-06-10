// playwright.config.ts (gm-root.27.26)
//
// Acceptance-test runner config. Two projects:
//   - acceptance-native  — default; the variant wrapper boots gemba
//     serve via bootstrapProject and uses the native orchestration
//     adapter.
//   - acceptance-gastown — opt-in via GEMBA_ACCEPTANCE_RUN_GASTOWN=1
//     (the variant wrapper itself test.skip()s if unset).
//
// The variant specs are named `spec.ts` (not `*.spec.ts`) so they
// were invisible to Playwright's default testMatch. Each project
// here uses an explicit testMatch pointing at its variant directory.

import { defineConfig, devices } from '@playwright/test';

const isCI = !!process.env.CI;
const timeoutMs = process.env.GEMBA_ACCEPTANCE_REAL_AGENTS === '1'
  ? 90 * 60 * 1000
  : 30 * 60 * 1000;

export default defineConfig({
  // Each variant exercises a real gemba server lifecycle inside the
  // test (via bootstrapProject). Don't fork: keep one worker per
  // project so the shared dev Dolt server (when in deep-mode) isn't
  // contended.
  fullyParallel: false,
  workers: 1,
  forbidOnly: isCI,
  retries: 0,
  timeout: timeoutMs,
  expect: { timeout: 30_000 },
  reporter: [
    ['list'],
    ['html', { outputFolder: 'reports/playwright', open: 'never' }],
  ],
  use: {
    ...devices['Desktop Chrome'],
    headless: true,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',
  },
  projects: [
    {
      name: 'acceptance-native',
      testMatch: ['variants/native/spec.ts'],
      metadata: {
        bead: 'gm-root.27.12',
        variant: 'native',
        agentMode: process.env.GEMBA_ACCEPTANCE_REAL_AGENTS === '1' ? 'real' : 'mock',
      },
    },
    {
      name: 'acceptance-gastown',
      testMatch: ['variants/gastown/spec.ts'],
      metadata: {
        bead: 'gm-root.27.15',
        variant: 'gastown',
        agentMode: process.env.GEMBA_ACCEPTANCE_REAL_AGENTS === '1' ? 'real' : 'mock',
        note:
          'gated on GEMBA_ACCEPTANCE_RUN_GASTOWN=1; the variant test.skip()s when unset',
      },
    },
    // gm-root.27.36 demo mode — same spec, but with on-screen
    // captions, slowMo cursor, video always on, 1080p capture.
    // Activates GEMBA_ACCEPTANCE_DEMO_MODE=1 inside spec.ts. Aim
    // for ~30s of usable footage; operator hand-trims in post.
    {
      name: 'acceptance-native-demo',
      testMatch: ['variants/native/spec.ts'],
      use: {
        viewport: { width: 1920, height: 1080 },
        launchOptions: { slowMo: 250 },
        video: { mode: 'on', size: { width: 1920, height: 1080 } },
        screenshot: 'on',
      },
      metadata: {
        bead: 'gm-root.27.36',
        variant: 'native',
        mode: 'demo',
        note: 'sets GEMBA_ACCEPTANCE_DEMO_MODE=1; produces 1080p video with captions',
      },
    },
  ],
});
