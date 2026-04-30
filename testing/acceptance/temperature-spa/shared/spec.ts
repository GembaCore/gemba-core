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
import { fileURLToPath } from 'node:url';
import type { Page } from '@playwright/test';

// gm-root.27.29 found: __dirname is not defined under "type": "module".
// Compute it from import.meta.url so importBeadsCLI can resolve relative
// jsonl paths from the shared/ directory.
const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

import type {
  AgentRunnerFactory,
  BugFiler,
  EscalationInjector,
} from './contracts';
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
  /**
   * bd issue prefix for the project (e.g. 'e2e0'). Used to render the
   * target JSONL pack's {{PREFIX}} placeholders. Defaults to 'tspa'.
   */
  beadPrefix?: string;
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
    importBeads: (jsonlPath) =>
      importBeadsCLI(opts.projectDir, jsonlPath, opts.beadPrefix ?? 'tspa'),
    escalationInjector: opts.escalationInjector ?? defaultEscalationInjector,
    fileBugBead: opts.bugFiler?.fileBugBead.bind(opts.bugFiler) ?? defaultBugFiler,
  };

  const milestones: AcceptanceReport['milestones'] = {
    M1: { ok: false, durationMs: 0 },
    M2: { ok: false, durationMs: 0 },
    M3: { ok: false, durationMs: 0 },
    triage: { ok: false, durationMs: 0 },
  };

  // Pool config is variant-owned (gm-root.27.27 reconciliation):
  //
  //  - Native: pool.toml is delivered to gemba serve as --pool-config
  //    via serveArgs at boot (gm-root.27.21). The variant wrapper
  //    needs no UI step.
  //
  //  - Gastown: rig name is dynamic per-run, so pool.toml CAN'T be
  //    pre-staged. The variant wrapper drives the UI to create the
  //    rig + polecat + save pool.toml BEFORE calling runAcceptance.
  //
  // Either way, by the time runAcceptance is called, the daemon is
  // configured. spec.ts no longer drives configurePool itself — the
  // variants own it.

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

async function importBeadsCLI(
  projectDir: string,
  jsonlPath: string,
  beadPrefix: string,
): Promise<void> {
  // Resolve the jsonl path absolutely — callers pass it relative to
  // the shared/ dir. If the rendered .jsonl doesn't exist but a
  // .jsonl.tmpl is adjacent (gm-root.27.4 ships templates with
  // {{PREFIX}} placeholders), render to a tempdir and import that.
  const requested = path.isAbsolute(jsonlPath)
    ? jsonlPath
    : path.resolve(__dirname, jsonlPath);
  let abs = requested;
  if (!existsSync(abs)) {
    const tmpl = `${requested}.tmpl`;
    if (existsSync(tmpl)) {
      const { renderPack } = await import('./target-jsonl/loader');
      const kind = path.basename(requested, '.jsonl') as
        | 'm1'
        | 'm2'
        | 'm3'
        | 'decisions';
      const rendered = renderPack(kind, beadPrefix, projectDir);
      abs = rendered.path;
    } else {
      throw new Error(
        `bead pack not found: ${abs} (no .jsonl or .jsonl.tmpl)`,
      );
    }
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
