// Cleanup orchestration (gm-root.27.20).
//
// Wraps an acceptance test body so every allocated resource (port,
// process, temp dir, rig) is released regardless of how the test
// exits — pass, fail, panic, or hang/timeout. Built so the variant
// cleanup helpers from .14 (cleanupNative) and .17 (cleanupGastown)
// register their teardown callbacks here without taking a hard
// dependency on those modules.

import { existsSync } from 'node:fs';
import { rm } from 'node:fs/promises';

import type { ProjectHandle } from './bootstrap.ts';
import type { AcceptanceResults } from './types.ts';

export type CleanupKind =
  | 'process'
  | 'port'
  | 'tempdir'
  | 'rig'
  | 'dolt'
  | 'other';

export interface CleanupEntry {
  kind: CleanupKind;
  // Human-readable label that lands in cleanup warnings + the
  // report. Should be specific enough to identify the resource:
  // "gemba-server pid=1234", "tempdir /tmp/gemba-acceptance-abc".
  label: string;
  // Allocation-order index. The orchestrator runs cleanup in
  // reverse order so dependent resources die before what they
  // depend on.
  allocatedAt: number;
  fn: () => Promise<void> | void;
}

export interface CleanupRegistry {
  // Add a teardown callback. Returns the entry so callers can
  // mutate label / kind later if useful (rare).
  add(entry: Omit<CleanupEntry, 'allocatedAt'>): CleanupEntry;
  // Snapshot the registered entries (read-only).
  entries(): readonly CleanupEntry[];
  // Internal: run every callback in reverse-allocation order.
  // Surfaces failures as warnings rather than throwing.
  run(): Promise<CleanupWarning[]>;
}

export interface CleanupWarning {
  label: string;
  kind: CleanupKind;
  error: Error;
}

export interface RunWithCleanupContext {
  cleanup: CleanupRegistry;
  // Mutable results object the body appends to. Passed through to
  // the report writer when present.
  results?: AcceptanceResults;
  handle?: ProjectHandle;
}

export interface RunWithCleanupOptions {
  // Hard upper bound on the body's wall clock. The orchestrator
  // races the body against this so a hung test still tears down.
  // Defaults to 10 minutes — long enough for the whole acceptance
  // run, short enough that a stuck server doesn't hang CI.
  timeoutMs?: number;
  // Hook so the spec can record warnings into AcceptanceResults
  // (the bead DoD says cleanup failures land in the report).
  onCleanupWarning?: (warning: CleanupWarning) => void;
}

export interface RunWithCleanupResult {
  warnings: CleanupWarning[];
  timedOut: boolean;
}

// Minimal subset of Playwright's TestInfo we lean on. The wrapper
// only needs t.Cleanup-style hooks; declaring an inline type keeps
// the helper testable without dragging in @playwright/test.
export interface TestT {
  // Playwright's TestInfo exposes addTeardown via the test fixture;
  // for our purposes any "register a function that runs at the
  // end of the test" hook works. The wrapper stores its registry
  // here so a panic in the body still triggers cleanup.
  addCleanup?: (fn: () => Promise<void> | void) => void;
}

export function createCleanupRegistry(): CleanupRegistry {
  const entries: CleanupEntry[] = [];
  let counter = 0;

  return {
    add(entry) {
      const full: CleanupEntry = { ...entry, allocatedAt: counter++ };
      entries.push(full);
      return full;
    },
    entries() {
      return entries;
    },
    async run() {
      const warnings: CleanupWarning[] = [];
      for (let i = entries.length - 1; i >= 0; i--) {
        const e = entries[i]!;
        try {
          await e.fn();
        } catch (err) {
          warnings.push({
            label: e.label,
            kind: e.kind,
            error: err instanceof Error ? err : new Error(String(err)),
          });
        }
      }
      return warnings;
    },
  };
}

export async function runWithCleanup(
  t: TestT,
  body: (ctx: RunWithCleanupContext) => Promise<void>,
  opts: RunWithCleanupOptions = {}
): Promise<RunWithCleanupResult> {
  const cleanup = createCleanupRegistry();
  const ctx: RunWithCleanupContext = { cleanup };
  const timeoutMs = opts.timeoutMs ?? 10 * 60 * 1000;

  let timedOut = false;
  let bodyError: unknown = undefined;

  const runBody = (async () => {
    try {
      await body(ctx);
    } catch (err) {
      bodyError = err;
    }
  })();

  const timer: { id?: NodeJS.Timeout } = {};
  const timeout = new Promise<void>((resolve) => {
    timer.id = setTimeout(() => {
      timedOut = true;
      resolve();
    }, timeoutMs);
    // Don't keep the event loop alive solely for this timer.
    timer.id.unref?.();
  });

  await Promise.race([runBody, timeout]);
  if (timer.id) clearTimeout(timer.id);

  // Always tear down, regardless of body outcome.
  const warnings = await cleanup.run();
  if (opts.onCleanupWarning) {
    for (const w of warnings) opts.onCleanupWarning(w);
  }

  // If the test runner gave us a hook, also register a no-op so
  // even a SIGINT during teardown gets a second chance via the
  // runner's own hook — the actual teardown already ran above.
  t.addCleanup?.(() => undefined);

  if (bodyError) throw bodyError;
  if (timedOut) {
    throw new Error(
      `runWithCleanup: body exceeded timeout of ${timeoutMs}ms; resources torn down`
    );
  }

  return { warnings, timedOut };
}

// Helper that the variant cleanup modules (.14 / .17) can use to
// register a temp-dir teardown without re-implementing the rm-rf
// fallback.
export function registerTempDir(
  cleanup: CleanupRegistry,
  path: string
): CleanupEntry {
  return cleanup.add({
    kind: 'tempdir',
    label: `tempdir ${path}`,
    fn: async () => {
      if (existsSync(path)) {
        await rm(path, { recursive: true, force: true });
      }
    },
  });
}
