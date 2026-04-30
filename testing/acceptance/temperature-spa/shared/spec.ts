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
import { Narrator, noopNarrator } from './narrator';
import { runM1Step } from './steps/m1';
import { runM2Step } from './steps/m2';
import { runM3Step } from './steps/m3';
import { runTriageStep } from './steps/triage';
import {
  installDemoBanner,
  setDemoCaption,
  demoPause,
  DEMO_MODE,
} from './helpers/demo-mode';

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
  /**
   * Optional narrator (gm-root.27.37). When provided, runAcceptance
   * + each step emit phrase events at known boundaries. The narrator
   * builds a `narration.json` for downstream TTS / voice-over
   * pipelines aligned to the demo-mode video capture. Absent →
   * emits are no-ops.
   */
  narrator?: Narrator;
  /**
   * Optional path to write narration.json at run end. When the
   * narrator is set but this is absent, the JSON is included on the
   * AcceptanceReport but not persisted.
   */
  narrationOutputPath?: string;
  /**
   * Optional video path to embed in the narration.json envelope so
   * downstream renderers know which mp4 to align against. Demo mode
   * sets this; default runs leave it undefined.
   */
  narrationVideoPath?: string;
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
  /**
   * Narration timeline (gm-root.27.37). Present when opts.narrator
   * was provided. Aligned to startedAt; the renderer pairs it with
   * the Playwright video for TTS.
   */
  narration?: import('./narrator').NarrationFile;
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
  /**
   * Narrator (gm-root.27.37) — always defined on the context. Steps
   * call `narrator.emit(phrase, hint)` at semantically meaningful
   * boundaries (bead dispatch, build start, oracle pass, etc.).
   * Defaults to a no-op shim when the caller didn't supply one.
   */
  narrator: Pick<Narrator, 'emit'>;
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

  // gm-root.27.33: count bug-bead-stub invocations so we can fail
  // the Playwright test when steps internally bailed and filed
  // stubs. Without this, runAcceptance returns a clean report even
  // when M-steps timed out or assertions tripped — the report is
  // forensic only; the throw is what actually gates Playwright.
  const bugFilerInvocations: Array<{ severity: string; title: string }> = [];
  const wrappedBugFiler: SharedContext['fileBugBead'] = async (filerOpts) => {
    bugFilerInvocations.push({ severity: filerOpts.severity, title: filerOpts.title });
    const downstream = opts.bugFiler?.fileBugBead.bind(opts.bugFiler) ?? defaultBugFiler;
    return downstream(filerOpts);
  };

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
    fileBugBead: wrappedBugFiler,
    narrator: opts.narrator ?? noopNarrator,
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

  // Demo-mode (gm-root.27.36): install the caption banner + start
  // the acceptance with a brief title frame. No-op when
  // GEMBA_ACCEPTANCE_DEMO_MODE is unset.
  await installDemoBanner(ctx.page);
  ctx.narrator.emit('A fresh project boots', 'short');
  if (DEMO_MODE) {
    await setDemoCaption(ctx.page, 'Acceptance: building a SPA via beads');
    await demoPause(2_000);
  }

  // Step through the milestones, captioning each.
  ctx.narrator.emit(
    'The operator configures a pool with the acceptance-engineer persona',
    'long',
  );
  await setDemoCaption(ctx.page, 'M1 — scaffolding the project');
  milestones.M1 = await timed(() => runM1Step(ctx));
  await demoPause(1_500);

  await setDemoCaption(ctx.page, 'M2 — Hello world MVP');
  milestones.M2 = await timed(() => runM2Step(ctx));
  await demoPause(1_500);

  await setDemoCaption(ctx.page, 'Triage — operator approves a blocking escalation');
  milestones.triage = await timed(() => runTriageStep(ctx));
  await demoPause(1_500);

  await setDemoCaption(ctx.page, 'M3 — conversion table, oracle verifies 16 rows');
  milestones.M3 = await timed(() => runM3Step(ctx));
  await demoPause(2_000);

  ctx.narrator.emit('Done. Three milestones, two operator clicks', 'short');
  if (DEMO_MODE) {
    await setDemoCaption(ctx.page, 'Done — three milestones, two operator clicks');
    await demoPause(2_000);
  }

  // Narration timeline (gm-root.27.37). Build the JSON envelope; if
  // the caller asked for a file path, persist it. Otherwise it lives
  // on the report only.
  const narration = opts.narrator
    ? opts.narrator.build({ video_path: opts.narrationVideoPath })
    : undefined;
  if (opts.narrator && opts.narrationOutputPath) {
    opts.narrator.writeTo(opts.narrationOutputPath, {
      video_path: opts.narrationVideoPath,
    });
  }

  const report: AcceptanceReport = {
    variant: opts.variant,
    runID,
    startedAt,
    finishedAt: new Date().toISOString(),
    milestones,
    narration,
  };

  // gm-root.27.33: gate Playwright on real success. M-steps tolerate
  // internal failures (file a bug stub + return) so the run flushes
  // every gap in one pass. But the Playwright test itself MUST fail
  // when any step bailed or any bug stub was filed — otherwise
  // 'pnpm test:native green' is misleading.
  const failedMilestones = (Object.entries(milestones) as Array<
    [keyof AcceptanceReport['milestones'], AcceptanceReport['milestones']['M1']]
  >).filter(([, m]) => !m.ok);
  if (failedMilestones.length > 0 || bugFilerInvocations.length > 0) {
    const summary = [
      failedMilestones.length > 0
        ? `${failedMilestones.length} milestones failed: ${failedMilestones.map(([k, m]) => `${k}=${m.error ?? 'no-error'}`).join('; ')}`
        : null,
      bugFilerInvocations.length > 0
        ? `${bugFilerInvocations.length} bug stubs filed: ${bugFilerInvocations.map((b) => `${b.severity}=${b.title}`).join('; ')}`
        : null,
    ]
      .filter(Boolean)
      .join(' | ');
    throw new AcceptanceFailure(summary, report, bugFilerInvocations);
  }

  return report;
}

export class AcceptanceFailure extends Error {
  constructor(
    summary: string,
    public readonly report: AcceptanceReport,
    public readonly bugStubs: Array<{ severity: string; title: string }>,
  ) {
    super(`acceptance: ${summary}`);
    this.name = 'AcceptanceFailure';
  }
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
