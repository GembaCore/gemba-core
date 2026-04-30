// shared/spec.ts (gm-root.27.6) — Playwright spec body for the
// temperature-spa acceptance test, shared between native and gastown
// variants.
//
// Per D15 §10, this file owns the test orchestration:
//   • spawn `gemba serve` against the bootstrapped project
//   • expose navigation + polling helpers as a shared context object
//   • call `runM1Step` / triage / `runM2Step` / `runM3Step` in order
//   • tear the server back down via Playwright `test.afterAll`
//
// Wave 1 (`.1`–`.5`) is stubbed via the contracts in `./contracts.ts`.
// We accept those shapes as DI parameters; we never construct them.
//
// The exported `runAcceptance(opts)` is the function each variant
// (`gm-root.27.12` native, `.15` gastown) wraps. The variant wrapper
// builds the AgentRunnerFactory + Bootstrap + EscalationInjector and
// hands them to us.

import { spawn, type ChildProcess } from 'node:child_process';
import { existsSync } from 'node:fs';
import path from 'node:path';
import { setTimeout as sleep } from 'node:timers/promises';
import type { Page } from '@playwright/test';

import type {
  AgentRunnerFactory,
  Bootstrap,
  BugFiler,
  EscalationInjector,
  ProjectHandle,
} from './contracts';
import { configurePool } from './pool-config';
import { runM1Step } from './steps/m1';
import { runM2Step } from './steps/m2';
import { runM3Step } from './steps/m3';
import { runTriageStep } from './steps/triage';

// ─── Types ────────────────────────────────────────────────────────

export interface RunAcceptanceOpts {
  variant: 'native' | 'gastown';
  /** Test run identifier (ulid). Threaded through bootstrap + report. */
  runID: string;
  /** Wave 1 bootstrap helper (`.3`). Yields a ProjectHandle. */
  bootstrap: Bootstrap;
  /** Wave 1 agent factory (`.1`). Constructed once per run. */
  agentFactory: AgentRunnerFactory;
  /** Wave 1 escalation injector (`.5`). Used by the triage step. */
  escalationInjector: EscalationInjector;
  /** Playwright page handle (drives the SPA). */
  page: Page;
  /**
   * Optional bug-filing helper (Wave 4, `.19`). If absent, failures
   * are logged with structured detail and the test continues to fail
   * via Playwright assertions.
   */
  bugFiler?: BugFiler;
  /** Optional rig name override for gastown variant. Defaults to acceptance-{runID}. */
  rigName?: string;
}

export interface AcceptanceReport {
  variant: 'native' | 'gastown';
  runID: string;
  startedAt: string;
  finishedAt: string;
  milestones: Record<'M1' | 'M2' | 'M3' | 'triage', {
    ok: boolean;
    durationMs: number;
    error?: string;
  }>;
}

/** Public shape passed into each step. */
export interface SharedContext {
  variant: 'native' | 'gastown';
  page: Page;
  projectDir: string;
  gembaPort: number;
  doltPort: number;
  /** Navigate to a SPA route by absolute path. */
  goto: (route: '/board' | '/settings/pools' | '/escalations' | string) => Promise<void>;
  /** Poll `/api/workitems/{id}` until the bead is closed or timeout. */
  waitForBeadClosed: (beadID: string, timeoutMs: number) => Promise<void>;
  /** Recurse children of a milestone; gate on all closed. */
  waitForAllBeadsClosed: (milestoneID: string, timeoutMs: number) => Promise<void>;
  /** Kill `gemba serve` and relaunch with the same flags (post-pool save). */
  restartServer: () => Promise<void>;
  /**
   * Import beads from a JSONL file. Tries `bd import <path>` from the
   * project dir; an alternate REST path can be wired here once a bulk
   * endpoint exists.
   */
  importBeads: (jsonlPath: string) => Promise<void>;
  /** Wave 1 escalation injector (`.5`). */
  escalationInjector: EscalationInjector;
  /** Wave 4 bug-filing helper (`.19`). Always defined; falls back to logger. */
  fileBugBead: BugFiler['fileBugBead'];
}

// ─── Public entry point ───────────────────────────────────────────

/**
 * runAcceptance is the shared spec body. Called from the variant
 * wrappers under a Playwright test. Both variants share the same
 * milestone progression — only the bootstrap + pool scope differ.
 */
export async function runAcceptance(opts: RunAcceptanceOpts): Promise<AcceptanceReport> {
  const startedAt = new Date().toISOString();
  const handle = await opts.bootstrap({ variant: opts.variant, runID: opts.runID });

  const ctx: SharedContext & { _server: ServerHandle | null; _handle: ProjectHandle } = {
    variant: opts.variant,
    page: opts.page,
    projectDir: handle.projectDir,
    gembaPort: handle.gembaPort,
    doltPort: handle.doltPort,
    goto: (route) => navigate(opts.page, handle.gembaPort, route),
    waitForBeadClosed: (id, t) => waitForBeadClosed(handle.gembaPort, id, t),
    waitForAllBeadsClosed: (mid, t) => waitForAllBeadsClosed(handle.gembaPort, mid, t),
    restartServer: async () => {
      if (ctx._server) await ctx._server.kill();
      ctx._server = await spawnGembaServe(handle, opts.variant);
    },
    importBeads: (jsonlPath) => importBeadsCLI(handle.projectDir, jsonlPath),
    escalationInjector: opts.escalationInjector,
    fileBugBead: opts.bugFiler?.fileBugBead.bind(opts.bugFiler) ?? defaultBugFiler,
    _server: null,
    _handle: handle,
  };

  const milestones: AcceptanceReport['milestones'] = {
    M1: { ok: false, durationMs: 0 },
    M2: { ok: false, durationMs: 0 },
    M3: { ok: false, durationMs: 0 },
    triage: { ok: false, durationMs: 0 },
  };

  try {
    // 1. Boot the server.
    ctx._server = await spawnGembaServe(handle, opts.variant);
    await waitForServerReady(handle.gembaPort, 30_000);

    // 2. Configure pool via SPA. Note: configurePool does NOT trigger
    //    the restart itself; we do that here so the new pool.toml is
    //    re-read on the next gemba serve start.
    await ctx.goto('/settings/pools');
    const scope = opts.variant === 'native' ? 'local' : (opts.rigName ?? `acceptance-${opts.runID}`);
    await configurePool(opts.page, {
      variant: opts.variant,
      scope,
      persona: 'acceptance-engineer',
      size: 1,
      floor: 0.5,
    });
    await ctx.restartServer();
    await waitForServerReady(handle.gembaPort, 30_000);

    // 3. Step through the milestones.
    milestones.M1 = await timed(() => runM1Step(ctx));
    milestones.M2 = await timed(() => runM2Step(ctx));
    milestones.triage = await timed(() => runTriageStep(ctx));
    milestones.M3 = await timed(() => runM3Step(ctx));
  } finally {
    if (ctx._server) {
      await ctx._server.kill().catch(() => undefined);
    }
    await handle.cleanup().catch(() => undefined);
  }

  return {
    variant: opts.variant,
    runID: opts.runID,
    startedAt,
    finishedAt: new Date().toISOString(),
    milestones,
  };
}

// ─── Server lifecycle ─────────────────────────────────────────────

interface ServerHandle {
  proc: ChildProcess;
  kill: () => Promise<void>;
}

async function spawnGembaServe(
  handle: ProjectHandle,
  variant: 'native' | 'gastown',
): Promise<ServerHandle> {
  // Resolve the gemba binary the same way the e2e fixture does — prefer
  // a workspace-local build, fall back to PATH.
  const gembaBin = process.env.GEMBA_ACCEPTANCE_BIN ?? 'gemba';
  const args = [
    'serve',
    '--port',
    String(handle.gembaPort),
    `--orchestration=${variant}`,
  ];
  const proc = spawn(gembaBin, args, {
    cwd: handle.projectDir,
    env: {
      ...process.env,
      // Pin the project's Dolt port so bd inside the project hits it.
      GEMBA_DOLT_PORT: String(handle.doltPort),
      // Force MockAgentRunner unless the operator has opted into real
      // claude (real-vs-mock factory contract per D16 §4.4).
      GEMBA_ACCEPTANCE_REAL_AGENTS: process.env.GEMBA_ACCEPTANCE_REAL_AGENTS ?? '0',
    },
    stdio: ['ignore', 'pipe', 'pipe'],
  });
  // Surface server logs on test failure (Playwright captures stdout/stderr).
  proc.stdout?.on('data', (b: Buffer) => process.stdout.write(`[gemba] ${b.toString()}`));
  proc.stderr?.on('data', (b: Buffer) => process.stderr.write(`[gemba!] ${b.toString()}`));

  return {
    proc,
    kill: async () => {
      if (proc.killed || proc.exitCode != null) return;
      proc.kill('SIGTERM');
      // Give it 5s to flush. If still alive, escalate.
      const exited = await new Promise<boolean>((resolve) => {
        const t = setTimeout(() => resolve(false), 5_000);
        proc.once('exit', () => {
          clearTimeout(t);
          resolve(true);
        });
      });
      if (!exited) proc.kill('SIGKILL');
    },
  };
}

async function waitForServerReady(port: number, timeoutMs: number): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      const res = await fetch(`http://127.0.0.1:${port}/api/workspace`);
      if (res.ok) return;
    } catch {
      // not ready
    }
    await sleep(250);
  }
  throw new Error(`gemba serve did not become ready within ${timeoutMs}ms on port ${port}`);
}

// ─── Navigation helpers ───────────────────────────────────────────

async function navigate(page: Page, port: number, route: string): Promise<void> {
  const url = `http://127.0.0.1:${port}${route.startsWith('/') ? route : `/${route}`}`;
  await page.goto(url, { waitUntil: 'domcontentloaded' });
}

// ─── Bead-state polling ───────────────────────────────────────────

async function waitForBeadClosed(port: number, beadID: string, timeoutMs: number): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  let last: string | undefined;
  while (Date.now() < deadline) {
    try {
      const res = await fetch(`http://127.0.0.1:${port}/api/workitems/${encodeURIComponent(beadID)}`);
      if (res.ok) {
        const json = (await res.json()) as { state_category?: string; status?: string };
        last = `${json.state_category}/${json.status}`;
        if (json.state_category === 'completed') return;
      }
    } catch (err) {
      last = `fetch error: ${String((err as Error).message)}`;
    }
    await sleep(2_000);
  }
  throw new Error(
    `bead ${beadID} did not reach state_category=completed within ${timeoutMs}ms (last: ${last ?? 'never observed'})`,
  );
}

async function waitForAllBeadsClosed(
  port: number,
  milestoneID: string,
  timeoutMs: number,
): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  // Recurse milestone -> epics -> tasks; gate on every node completed.
  while (Date.now() < deadline) {
    try {
      const open = await collectOpenDescendants(port, milestoneID);
      if (open.length === 0) return;
    } catch (err) {
      // transient — keep polling
      void err;
    }
    await sleep(2_000);
  }
  throw new Error(`milestone ${milestoneID} did not fully close within ${timeoutMs}ms`);
}

async function collectOpenDescendants(port: number, parentID: string): Promise<string[]> {
  const open: string[] = [];
  const queue: string[] = [parentID];
  const seen = new Set<string>();
  while (queue.length > 0) {
    const id = queue.shift()!;
    if (seen.has(id)) continue;
    seen.add(id);
    const res = await fetch(`http://127.0.0.1:${port}/api/workitems/${encodeURIComponent(id)}`);
    if (!res.ok) {
      // Treat as still-pending if the server isn't ready yet.
      open.push(id);
      continue;
    }
    const node = (await res.json()) as { state_category?: string };
    if (node.state_category !== 'completed') open.push(id);

    const childRes = await fetch(
      `http://127.0.0.1:${port}/api/workitems?parent_id=${encodeURIComponent(id)}`,
    );
    if (childRes.ok) {
      const json = (await childRes.json()) as { items?: Array<{ id: string }>; workitems?: Array<{ id: string }> };
      const items = json.items ?? json.workitems ?? [];
      for (const c of items) queue.push(c.id);
    }
  }
  return open;
}

// ─── bd import shim ───────────────────────────────────────────────

async function importBeadsCLI(projectDir: string, jsonlPath: string): Promise<void> {
  // Resolve the jsonl path absolutely — callers pass it relative to
  // the shared/ dir.
  const abs = path.isAbsolute(jsonlPath)
    ? jsonlPath
    : path.resolve(__dirname, jsonlPath);
  if (!existsSync(abs)) {
    // The file may legitimately not exist yet — `gm-root.27.4`
    // populates these placeholders. Fail soft so the M1 wait path
    // surfaces the real failure (timeout) rather than a bogus
    // import error.
    throw new Error(`bead pack not found: ${abs} (gm-root.27.4 must land first)`);
  }
  return new Promise((resolve, reject) => {
    const proc = spawn('bd', ['import', abs], { cwd: projectDir, stdio: 'inherit' });
    proc.on('exit', (code) => {
      if (code === 0) resolve();
      else reject(new Error(`bd import exited ${code} (path=${abs})`));
    });
    proc.on('error', (err) => reject(err));
  });
}

// ─── Defaults ─────────────────────────────────────────────────────

const defaultBugFiler: BugFiler['fileBugBead'] = async (opts) => {
  // No bug filer wired (Wave 4 not landed). Log structured detail so
  // the operator + Playwright trace capture the failure.
  // eslint-disable-next-line no-console
  console.error(
    `[acceptance:bug-filer-stub] severity=${opts.severity} title=${JSON.stringify(opts.title)}\n${opts.body}`,
  );
  return null;
};

async function timed(
  fn: () => Promise<void>,
): Promise<{ ok: boolean; durationMs: number; error?: string }> {
  const start = Date.now();
  try {
    await fn();
    return { ok: true, durationMs: Date.now() - start };
  } catch (err) {
    return {
      ok: false,
      durationMs: Date.now() - start,
      error: (err as Error).message,
    };
  }
}
