// Wave 1 contract stubs for the temperature-spa acceptance test.
//
// Wave 2 (`gm-root.27.6`–`.11`) ships ahead of Wave 1 (`.1`–`.5`).
// This file declares the TypeScript shapes Wave 2 expects from Wave 1
// surfaces so the spec body compiles and ships cleanly. The Wave 1
// owners — `.1` AgentRunner factory, `.3` bootstrap helper, `.5`
// escalation injector — must conform to these interfaces exactly.
//
// Cross-references:
//   • D15 design (docs/design/acceptance-temperature-spa.md) §4 (`.1`/`.2`),
//     §10 (`.3`), §11.6 (`.5`).
//
// If Wave 1 finds a contract here that is wrong, the right move is
// to update this file FIRST (in coordination with Wave 2 owners) and
// then conform — never silently diverge.

// ─── .1 — Headless agent supervisor (AgentRunner factory) ─────────

/**
 * AgentRunner is the per-pool-member loop the headless harness drives.
 * The acceptance harness creates one factory per run; the factory
 * returns either MockAgentRunner (CI default) or a real-claude wrapper
 * when `GEMBA_ACCEPTANCE_REAL_AGENTS=1` is set.
 */
export interface AgentRunner {
  /**
   * Run the agent against the given bead. Resolves when the bead is
   * closed and the runner has emitted `gemba-state bead-done`.
   * Implementations MUST be idempotent against re-entry — the spec
   * may retry on race.
   */
  run(beadID: string): Promise<void>;

  /**
   * Recycle the underlying session (the same path `RecycleSession`
   * exercises). Resolves once the runner is ready to claim the next
   * bead. Real-claude path may be a multi-second operation; mock is
   * effectively instant.
   */
  recycle(): Promise<void>;

  /**
   * Tear down the runner. Idempotent. Called at end of test or on
   * fixture failure.
   */
  close(): Promise<void>;
}

export interface AgentRunnerFactory {
  /**
   * Create a fresh runner bound to the given environment. The harness
   * passes the project's gemba/dolt port + variant flags through `env`.
   * Per D16, the factory branch (mock vs real-claude) is chosen here.
   */
  create(env: Record<string, string>): AgentRunner;
}

// ─── .3 — Ephemeral Dolt + project bootstrap helper ───────────────

/**
 * ProjectHandle is what the bootstrap helper returns: the temp project
 * dir + the random ports the harness should target. Cleanup is exposed
 * directly; the spec wires it to Playwright `test.afterAll`.
 */
export interface ProjectHandle {
  /** Absolute path to the bootstrapped project directory. */
  projectDir: string;
  /** Random port the harness will spawn `gemba serve` on. */
  gembaPort: number;
  /** Random port the project's ephemeral Dolt server is listening on. */
  doltPort: number;
  /** Tear-down callback. Removes temp dir, frees ports. Idempotent. */
  cleanup: () => Promise<void>;
}

export interface BootstrapOptions {
  variant: 'native' | 'gastown';
  /** Stable run identifier (ulid). Used in temp dir name + report. */
  runID: string;
}

export type Bootstrap = (opts: BootstrapOptions) => Promise<ProjectHandle>;

// ─── .5 — Synthetic escalation injector ───────────────────────────

/**
 * EscalationInjector mints a synthetic blocking escalation against an
 * existing bead, exercising the triage path without requiring a real
 * agent failure. Implementation lives in the gemba server (likely via
 * a debug-only `POST /api/escalations/synthetic` endpoint) — the
 * spec body only sees this client-side shape.
 */
export interface EscalationInjector {
  inject(opts: {
    beadID: string;
    kind: 'hitl_approval';
    urgency: 'blocking';
  }): Promise<{ escalationID: string }>;
}

// ─── Bug-filing helper (Wave 4 territory; .19) ────────────────────

/**
 * Stub for the bug-filing helper landed by `gm-root.27.19`. Wave 2
 * accepts this on the step context so failure paths can call it; until
 * `.19` lands, callers may pass a no-op that just logs.
 */
export interface BugFiler {
  fileBugBead(opts: {
    title: string;
    severity: 'CRITICAL' | 'HIGH' | 'MEDIUM' | 'LOW';
    body: string;
  }): Promise<{ beadID: string } | null>;
}
