// shared/spec.smoke.test.ts (gm-root.27.6) — Smoke test for the
// acceptance harness scaffolding.
//
// Skipped by default (Wave 1 not landed). Set
// GEMBA_ACCEPTANCE_SMOKE=1 to run a minimal end-to-end probe with
// a real bootstrap. The default-on assertions cover the API surface
// only (functions exist + accept the contracted shapes), which is
// enough to gate the scaffolding bead's DoD.

import { test, expect } from '@playwright/test';
import type {
  AgentRunnerFactory,
  Bootstrap,
  BootstrapOptions,
  EscalationInjector,
  ProjectHandle,
} from './contracts';
import { runAcceptance } from './spec';

const SMOKE = process.env.GEMBA_ACCEPTANCE_SMOKE === '1';

test.describe('acceptance harness smoke (gm-root.27.6)', () => {
  test('runAcceptance API surface', () => {
    // Static-shape probe — no runtime needed. If `runAcceptance`
    // ceases to be a function, or its signature regresses to a shape
    // that doesn't accept our DI bag, this test fails to compile.
    expect(typeof runAcceptance).toBe('function');

    // Compile-time conformance: the variant wrappers (`.12` native,
    // `.15` gastown) will hand us factory + bootstrap + injector
    // shaped exactly like the contracts file. If we accidentally
    // change the contract, this assignment fails to compile.
    const _factory: AgentRunnerFactory = {
      create: () => ({
        run: async () => undefined,
        recycle: async () => undefined,
        close: async () => undefined,
      }),
    };
    const _bootstrap: Bootstrap = async (_opts: BootstrapOptions): Promise<ProjectHandle> => ({
      projectDir: '/tmp/noop',
      gembaPort: 0,
      doltPort: 0,
      cleanup: async () => undefined,
    });
    const _injector: EscalationInjector = {
      inject: async () => ({ escalationID: 'noop' }),
    };
    void _factory;
    void _bootstrap;
    void _injector;
  });

  test('bootstrap → server-spawn → SPA load (real)', async ({ page }) => {
    test.skip(!SMOKE, 'gated on GEMBA_ACCEPTANCE_SMOKE=1');
    // A real run would import `.3`'s bootstrap helper. We can't here
    // because Wave 1 hasn't landed. When it does, swap the stub for
    // the real bootstrap — no code change to runAcceptance.
    const stubBootstrap: Bootstrap = async () => {
      throw new Error('Wave 1 .3 bootstrap not landed; rerun with GEMBA_ACCEPTANCE_SMOKE=0');
    };
    const stubFactory: AgentRunnerFactory = {
      create: () => ({
        run: async () => undefined,
        recycle: async () => undefined,
        close: async () => undefined,
      }),
    };
    const stubInjector: EscalationInjector = {
      inject: async () => ({ escalationID: 'noop' }),
    };
    await expect(
      runAcceptance({
        variant: 'native',
        runID: 'smoke',
        bootstrap: stubBootstrap,
        agentFactory: stubFactory,
        escalationInjector: stubInjector,
        page,
      }),
    ).rejects.toThrow(/Wave 1/);
  });
});
