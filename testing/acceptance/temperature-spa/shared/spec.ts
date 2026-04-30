// shared/spec.ts (gm-root.27.6) — Playwright spec body for the
// temperature-spa acceptance test, shared between native and gastown
// variants.
//
// Per D15 §10, this file owns the test orchestration:
//   • point the SPA at the bootstrapped project
//   • expose navigation + polling helpers as a shared context object
//   • call `runM1Step` / triage / `runM2Step` / `runM3Step` in order
//
// gm-root.27.24 contract reconciliation: bootstrapProject (.3)
// already spawns gemba serve via spinRealServer. Variants pass the
// baseURL + projectDir from that handle directly into runAcceptance;
// we don't spawn a second server. RunAcceptanceOpts therefore takes
// the simpler {variant, page, baseURL, projectDir, ...} shape rather
// than the older {bootstrap, agentFactory, ...} DI shape that
// spec.ts originally drafted.

import { spawn } from 'node:child_process';
import { existsSync } from 'node:fs';
import path from 'node:path';
import { setTimeout as sleep } from 'node:timers/promises';
import type { Page } from '@playwright/test';

import type {
  AgentRunnerFactory,
  BugFiler,
  EscalationInjector,
} from './contracts';
import { configurePool } from './pool-config';
import { runM1Step } from './steps/m1';
import { runM2Step } from './steps/m2';
import { runM3Step } from './steps/m3';
import { runTriageStep } from './steps/triage';

// ─── Types ────────────────────────────────────────────────────────

export interface RunAcceptanceOpts {
  variant: 'native' | 'gastown';
  /** Playwright page handle (drives the SPA). */
  page: Page;
  /** http://127.0.0.1:<port> — the gemba server bootstrap already started. */
  baseURL: string;
  /** Absolute path to the bootstrapped project directory. */
  projectDir: string;
  /** Optional run identifier; defaults to a fresh ulid-shape per call. */
  runID?: string;
  /** Wave 1 agent factory (`.1`). Constructed lazily by the spec. */
  agentFactory?: AgentRunnerFactory;
  /** Wave 1 escalation injector (`.5`). Used by the triage step. */
  escalationInjector?: EscalationInjector;
  /** Optional bug-filing helper (Wave 4, `.19`). Defaults to a stderr logger. */
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
  /** http://127.0.0.1:<port> — gemba server base URL. */
  baseURL: string;
  /**
   * gembaPort — derived from baseURL for steps that still build URLs
   * the legacy way (e.g., triage.ts). Prefer baseURL in new code.
   */
  gembaPort: number;
  /**
   * doltPort — exposed for completeness; bootstrap doesn't surface
   * it (embedded Dolt per tempdir per gm-root.27.3 + gm-h4n
   * discipline). Always 0 today; reserved for a future external
   * Dolt mode.
   */
  doltPort: number;
  /** Navigate to a SPA route by absolute path. */
  goto: (route: '/board' | '/settings/pools' | '/escalations' | string) => Promise<void>;
  /** Poll `/api/workitems/{id}` until the bead is closed or timeout. */
  waitForBeadClosed: (beadID: string, timeoutMs: number) => Promise<void>;
  /** Recurse children of a milestone; gate on all closed. */
  waitForAllBeadsClosed: (milestoneID: string, timeoutMs: number) => Promise<void>;
  /**
   * No-op today (gemba is owned by bootstrap; in-place restart not
   * supported). Documented gap for the pool-via-UI flow that needs
   * a restart after Save. Tracked as a v1 acceptance limitation —
   * variants currently pre-write pool.toml before bootstrap so no
   * restart is required.
   */
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
  const runID = opts.runID ?? randomRunID();
  const gembaPort = portFromBaseURL(opts.baseURL);

  const ctx: SharedContext = {
    variant: opts.variant,
    page: opts.page,
    projectDir: opts.projectDir,
    baseURL: opts.baseURL,
    gembaPort,
    doltPort: 0,
    goto: (route) => navigate(opts.page, opts.baseURL, route),
    waitForBeadClosed: (id, t) => waitForBeadClosed(opts.baseURL, id, t),
    waitForAllBeadsClosed: (mid, t) => waitForAllBeadsClosed(opts.baseURL, mid, t),
    restartServer: async () => {
      // Server lifecycle is owned by bootstrap (gm-root.27.3). In-place
      // restart isn't wired in v1 — variants pre-stage pool.toml before
      // bootstrap so the daemon picks it up on first launch. If a future
      // bead needs hot-reload, expose a restart hook on ProjectHandle.
    },
    importBeads: (jsonlPath) => importBeadsCLI(opts.projectDir, jsonlPath),
    escalationInjector: opts.escalationInjector ?? defaultEscalationInjector,
    fileBugBead: opts.bugFiler?.fileBugBead.bind(opts.bugFiler) ?? defaultBugFiler,
  };

  const milestones: AcceptanceReport['milestones'] = {
    M1: { ok: false, durationMs: 0 },
    M2: { ok: false, durationMs: 0 },
    M3: { ok: false, durationMs: 0 },
    triage: { ok: false, durationMs: 0 },
  };

  // Configure the pool via the SPA. Note: configurePool requires the
  // editor's selectors per gm-s47n.18 to be present and a restart to
  // pick up the saved pool.toml. The variant pre-stages pool.toml in
  // projectDir today (cleanup tracked under gm-root.27.27); the UI
  // call here is best-effort and serves to exercise the editor.
  try {
    await ctx.goto('/settings/pools');
    const scope = opts.variant === 'native' ? 'local' : (opts.rigName ?? `acceptance-${runID}`);
    await configurePool(opts.page, {
      variant: opts.variant,
      scope,
      persona: 'acceptance-engineer',
      size: 1,
      floor: 0.5,
    });
  } catch (err) {
    // Don't abort — the pre-staged pool.toml keeps the daemon
    // dispatching even if the UI step fails (e.g., editor not yet
    // mounted). The first end-to-end run (gm-root.27.29) will
    // verify both paths.
    // eslint-disable-next-line no-console
    console.warn(`[acceptance] configurePool failed (continuing): ${(err as Error).message}`);
  }

  // Step through the milestones.
  milestones.M1 = await timed(() => runM1Step(ctx));
  milestones.M2 = await timed(() => runM2Step(ctx));
  milestones.triage = await timed(() => runTriageStep(ctx));
  milestones.M3 = await timed(() => runM3Step(ctx));

  return {
    variant: opts.variant,
    runID,
    startedAt,
    finishedAt: new Date().toISOString(),
    milestones,
  };
}

function portFromBaseURL(baseURL: string): number {
  const m = baseURL.match(/:([0-9]+)(?:\/|$)/);
  return m && m[1] ? parseInt(m[1], 10) : 0;
}

function randomRunID(): string {
  const alphabet = 'abcdefghijklmnopqrstuvwxyz0123456789';
  let id = '';
  for (let i = 0; i < 8; i += 1) {
    id += alphabet[Math.floor(Math.random() * alphabet.length)];
  }
  return id;
}

const defaultEscalationInjector: EscalationInjector = {
  async inject() {
    throw new Error(
      'EscalationInjector not provided to runAcceptance — pass one or expect the triage step to fall back.',
    );
  },
};

// ─── Navigation helpers ───────────────────────────────────────────

async function navigate(page: Page, baseURL: string, route: string): Promise<void> {
  const url = `${baseURL}${route.startsWith('/') ? route : `/${route}`}`;
  await page.goto(url, { waitUntil: 'domcontentloaded' });
}

// ─── Bead-state polling ───────────────────────────────────────────

async function waitForBeadClosed(
  baseURL: string,
  beadID: string,
  timeoutMs: number,
): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  let last: string | undefined;
  while (Date.now() < deadline) {
    try {
      const res = await fetch(`${baseURL}/api/workitems/${encodeURIComponent(beadID)}`);
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
  baseURL: string,
  milestoneID: string,
  timeoutMs: number,
): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      const open = await collectOpenDescendants(baseURL, milestoneID);
      if (open.length === 0) return;
    } catch (err) {
      void err;
    }
    await sleep(2_000);
  }
  throw new Error(`milestone ${milestoneID} did not fully close within ${timeoutMs}ms`);
}

async function collectOpenDescendants(baseURL: string, parentID: string): Promise<string[]> {
  const open: string[] = [];
  const queue: string[] = [parentID];
  const seen = new Set<string>();
  while (queue.length > 0) {
    const id = queue.shift()!;
    if (seen.has(id)) continue;
    seen.add(id);
    const res = await fetch(`${baseURL}/api/workitems/${encodeURIComponent(id)}`);
    if (!res.ok) {
      open.push(id);
      continue;
    }
    const node = (await res.json()) as { state_category?: string };
    if (node.state_category !== 'completed') open.push(id);

    const childRes = await fetch(
      `${baseURL}/api/workitems?parent_id=${encodeURIComponent(id)}`,
    );
    if (childRes.ok) {
      const json = (await childRes.json()) as {
        items?: Array<{ id: string }>;
        workitems?: Array<{ id: string }>;
      };
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
