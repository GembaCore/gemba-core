// testing/acceptance/temperature-spa/shared/runner/factory.ts
//
// gm-root.27.1 — AgentRunner factory.
//
// Single switch between MockAgentRunner (CI default; deterministic;
// no API tokens) and the real-claude wrapper (opt-in via env flag;
// real spawn through the native adapter).
//
// Per D15 §1, real-claude support is deferred — landing a stable
// factory wrapper that errors with a clear message when opted-in
// today is the right shape. The wrapper file (real-claude.ts) is
// stubbed below to throw NotImplementedError so the contract is
// honored; real implementation comes in a follow-up bead.
//
// References:
//   - D15 docs/design/acceptance-temperature-spa.md §1, §11.3
//   - D16 docs/design/acceptance-temperature-spa.md §4.4 (factory)
//   - shared/contracts.ts (AgentRunner / AgentRunnerFactory)
//   - shared/runner/mock.ts (the mock implementation)

import type {
  AgentRunner,
  AgentRunnerFactory as AgentRunnerFactoryShape,
} from '../contracts';
import { MockAgentRunner } from './mock';

export type AgentMode = 'mock' | 'real';

/**
 * Decide which agent mode the harness should use. Reads the
 * GEMBA_ACCEPTANCE_REAL_AGENTS env flag; default 'mock'.
 */
export function selectAgentMode(env: NodeJS.ProcessEnv = process.env): AgentMode {
  return env.GEMBA_ACCEPTANCE_REAL_AGENTS === '1' ? 'real' : 'mock';
}

/**
 * Construct the factory for a run. The factory's create() returns a
 * fresh AgentRunner per pool member.
 *
 * The harness builds env at variant-spec time with the bootstrapped
 * project's gemba port, project dir, etc. The factory threads those
 * through to the runner constructor.
 */
export function makeAgentRunnerFactory(opts: {
  baseURL: string;
  projectDir: string;
  /** Optional logger threaded into runner.log. */
  log?: (msg: string) => void;
  /** Override the agent mode. Defaults to selectAgentMode(). */
  mode?: AgentMode;
}): AgentRunnerFactoryShape {
  const mode = opts.mode ?? selectAgentMode();
  return {
    create(_envOverride: Record<string, string>): AgentRunner {
      if (mode === 'real') {
        // Per D15 §11.3, real-claude is opt-in but not yet
        // stabilized — wrap with a clear error so the operator
        // sees the gap immediately rather than a runtime crash
        // later.
        return makeNotYetImplementedRealRunner();
      }
      return new MockAgentRunner({
        baseURL: opts.baseURL,
        projectDir: opts.projectDir,
        log: opts.log,
      });
    },
  };
}

function makeNotYetImplementedRealRunner(): AgentRunner {
  const message =
    'Real-claude AgentRunner is not yet implemented. ' +
    'Drop GEMBA_ACCEPTANCE_REAL_AGENTS to use MockAgentRunner. ' +
    'Tracked under D15 §11.3 (deferred).';
  return {
    async run() {
      throw new Error(message);
    },
    async recycle() {
      throw new Error(message);
    },
    async close() {
      // close is the only no-op so a half-constructed test still
      // tears down cleanly.
    },
  };
}
