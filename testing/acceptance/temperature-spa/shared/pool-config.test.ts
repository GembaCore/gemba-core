// shared/pool-config.test.ts (gm-root.27.7) — Pool config helper
// surface tests.
//
// Playwright component-mount isn't wired into this workspace's test
// infrastructure, so the expressive coverage (full editor flows
// against a mounted PoolsPage) lives in the Playwright spec
// `testing/e2e/specs/settings/pools.spec.ts` (gm-s47n.18). This
// file's job is the lightweight DoD piece: assert the helper exists
// with the shape Wave 2 promised, and exercise the
// selector-not-found error path with a stub Page.

import { test, expect } from '@playwright/test';
import { configurePool, type ConfigurePoolOpts } from './pool-config';

test.describe('configurePool API surface (gm-root.27.7)', () => {
  test('helper signature is callable with native opts', () => {
    expect(typeof configurePool).toBe('function');
    // Compile-time conformance — these objects must satisfy
    // ConfigurePoolOpts. If the helper shape regresses, TS fails the
    // assignment.
    const _native: ConfigurePoolOpts = {
      variant: 'native',
      scope: 'local',
      persona: 'acceptance-engineer',
      size: 1,
      floor: 0.5,
    };
    const _gastown: ConfigurePoolOpts = {
      variant: 'gastown',
      scope: 'acceptance-rig',
      persona: 'acceptance-engineer',
      size: 1,
      floor: 0.5,
    };
    void _native;
    void _gastown;
  });

  test('selector-not-found error surfaces clear message', async () => {
    // We construct a minimal Page-like that throws on every
    // waitForSelector call. The helper should bubble that timeout up
    // (Playwright's own surface), which we observe via rejection.
    const fakePage = {
      waitForSelector: async () => {
        throw new Error('TimeoutError: locator [data-testid="pools-page"] not found');
      },
    } as unknown as import('@playwright/test').Page;

    await expect(
      configurePool(fakePage, {
        variant: 'native',
        scope: 'local',
        persona: 'acceptance-engineer',
        size: 1,
        floor: 0.5,
      }),
    ).rejects.toThrow(/pools-page/);
  });
});
