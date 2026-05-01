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
import { existsSync, readFileSync, writeFileSync } from 'node:fs';
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
  demoDragTo,
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
  /** Runtime bd issue prefix, e.g. e2e0. */
  beadPrefix: string;
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
  /** Poll `/api/work-items/{id}` until the bead is closed or timeout. */
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
  const importedPacks = new Set<string>();

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
    beadPrefix: opts.beadPrefix ?? 'tspa',
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
      importBeadsOnce(
        importedPacks,
        opts.projectDir,
        jsonlPath,
        opts.beadPrefix ?? 'tspa',
      ),
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

  if (DEMO_MODE) {
    await runDemoOpening(ctx);
  }

  // Step through the milestones, captioning each.
  ctx.narrator.emit(
    'The operator configures a pool with the acceptance-engineer persona',
    'long',
  );
  await setDemoCaption(ctx.page, 'M1 — scaffolding the project');
  milestones.M1 = await timed(() => runM1Step(ctx));
  await showAgentStatus(ctx, 'Agent status — M1 completed');
  await demoPause(1_500);

  await setDemoCaption(ctx.page, 'M2 — Hello world MVP');
  milestones.M2 = await timed(() => runM2Step(ctx));
  await showAgentStatus(ctx, 'Agent status — M2 completed');
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

async function runDemoOpening(ctx: SharedContext): Promise<void> {
  ctx.narrator.emit(
    'The full plan is visible before work starts: milestones, epics, and beads',
    'long',
  );
  await setDemoCaption(ctx.page, 'Loading the full plan: 3 milestones, 3 epics, 12 task beads');
  await ctx.importBeads('target-jsonl/decisions.jsonl');
  await ctx.importBeads('target-jsonl/m1.jsonl');
  await ctx.importBeads('target-jsonl/m2.jsonl');
  await ctx.importBeads('target-jsonl/m3.jsonl');

  await ctx.goto('/board?layout=list&power=1');
  await ctx.page.waitForSelector('[data-testid="board-list"]', { timeout: 15_000 });
  await ctx.page.waitForSelector('[data-testid="board-list-count"]', { timeout: 15_000 });
  await ctx.page.waitForFunction(
    () => document.querySelector('[data-testid="board-list-count"]')?.textContent?.includes('20 item'),
    null,
    { timeout: 15_000 },
  );
  await ctx.page.waitForSelector('[data-testid^="grid-row-"]', { timeout: 15_000 });
  await demoPause(1_500);

  ctx.narrator.emit('The operator drags the first milestone into progress', 'medium');
  await setDemoCaption(ctx.page, 'First drag: move M1 into In Progress to start development');
  await ctx.goto('/board?layout=workitem&show_backlog=1');
  await ctx.page.waitForSelector('[data-testid="board-workitem"]', { timeout: 15_000 });
  const m1 = beadID(ctx, 'm1');
  const source = ctx.page.locator(`[data-work-item-id$="${m1}"]`).first();
  const target = ctx.page.getByTestId('board-column-started');
  await source.waitFor({ state: 'visible', timeout: 15_000 });
  await target.waitFor({ state: 'visible', timeout: 15_000 });
  await demoDragTo(ctx.page, source, target, { steps: 18 });
  await ensureDemoCascadeStarted(ctx, m1);
  await demoPause(1_500);

  await showSessionCreatedBeadInRecent(ctx);

  ctx.narrator.emit('Board view shows milestone-coloured epics', 'medium');
  await setDemoCaption(ctx.page, 'Board view: epics grouped with milestone badges');
  await ctx.goto('/board?layout=epic&show_backlog=1');
  await ctx.page.waitForSelector('[data-testid="board-epic"]', { timeout: 15_000 });
  await demoPause(1_500);

  ctx.narrator.emit('Milestone detail shows the Definition of Done in the bead description', 'medium');
  await setDemoCaption(ctx.page, 'Milestone detail: Definition of Done in the bead');
  await ctx.goto(`/board?bead=${encodeURIComponent(beadID(ctx, 'm1'))}`);
  await ctx.page.waitForSelector('[data-testid="workitem-detail-id"]', { timeout: 15_000 });
  await ctx.page.waitForSelector('[data-testid="section-description"]', { timeout: 15_000 });
  await demoPause(1_800);

  ctx.narrator.emit('Epic detail shows child beads by state', 'medium');
  await setDemoCaption(ctx.page, 'Epic detail: child beads and state are visible');
  await ctx.goto(`/board/${encodeURIComponent(beadID(ctx, 'e1'))}`);
  await ctx.page.waitForSelector('[data-testid="epic-detail-id"]', { timeout: 15_000 });
  await ctx.page.waitForSelector('[data-testid="epic-section-children"]', { timeout: 15_000 });
  await demoPause(1_800);

  ctx.narrator.emit('Refinement shows backlog triage work', 'medium');
  await setDemoCaption(ctx.page, 'Refinement: backlog triage surface');
  await ctx.goto('/refine');
  await ctx.page.waitForSelector('[data-testid="refine-page"]', { timeout: 15_000 });
  await demoPause(1_500);
}

async function showSessionCreatedBeadInRecent(ctx: SharedContext): Promise<void> {
  ctx.narrator.emit(
    'A running session files a follow-up bead, and propulsion pulls it into scope',
    'medium',
  );
  await setDemoCaption(ctx.page, 'Session-created bead: auto-staged by the active M1 cascade');
  const created = await createSessionFollowUpBead(ctx);
  await waitForWorkItemCategory(ctx, created.id, ['staged', 'started', 'completed'], 10_000);
  await demoPause(1_000);

  ctx.narrator.emit('Recent shows the new bead for inspection', 'medium');
  await setDemoCaption(ctx.page, 'Recent: inspect the bead the session just created');
  await ctx.goto('/recent');
  await ctx.page.getByTestId('recent-preset-1h').click();
  const row = ctx.page.getByTestId(`recent-row-${created.id}`);
  await row.waitFor({ state: 'visible', timeout: 15_000 });
  await demoPause(1_200);
  await row.click();
  await ctx.page.waitForSelector('[data-testid="workitem-detail-id"]', { timeout: 15_000 });
  await demoPause(1_800);
}

async function createSessionFollowUpBead(ctx: SharedContext): Promise<RawIssue> {
  const nonceBase = process.env.GEMBA_E2E_CONFIRM_NONCE ?? 'acceptance-test';
  const res = await fetch(`${ctx.baseURL}/api/work-items`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'X-GEMBA-Confirm': `${nonceBase}-session-followup-${Date.now()}`,
    },
    body: JSON.stringify({
      item: {
        title: 'Session-created follow-up: record scaffold evidence',
        kind: 'task',
        status: 'open',
        state_category: 'backlog',
        description:
          '---\n' +
          'template: noop\n' +
          '---\n\n' +
          '# Goal\n' +
          'A running agent noticed a small follow-up while M1 was active and filed it under the current epic.\n\n' +
          '# Definition of Done\n' +
          '- The bead appears in Recent.\n' +
          '- The active M1 cascade moves it from backlog into staged or in progress automatically.\n',
        labels: ['acceptance', 'milestone:m1', 'created-by:session', 'template:noop'],
        relationships: [
          { kind: 'parent_child', from: beadID(ctx, 'e1'), to: '' },
        ],
      },
    }),
  });
  if (!res.ok) {
    const txt = await res.text().catch(() => '');
    throw new Error(`demo session follow-up create failed ${res.status}: ${txt}`);
  }
  return (await res.json()) as RawIssue;
}

async function showAgentStatus(ctx: SharedContext, caption: string): Promise<void> {
  if (!DEMO_MODE) return;
  ctx.narrator.emit(caption, 'short');
  await setDemoCaption(ctx.page, caption);
  await ctx.goto('/sessions');
  await ctx.page.waitForSelector('[data-testid="sessions-page"]', { timeout: 15_000 });
  await demoPause(2_000);
}

function beadID(ctx: SharedContext, suffix: string): string {
  return `${ctx.beadPrefix}-${suffix}`;
}

async function ensureDemoCascadeStarted(ctx: SharedContext, wrapperID: string): Promise<void> {
  const observed = await waitForWorkItemCategory(ctx, wrapperID, ['started', 'completed'], 3_000);
  const nonceBase = process.env.GEMBA_E2E_CONFIRM_NONCE ?? 'acceptance-test';
  if (!observed) {
    const patch = await fetch(`${ctx.baseURL}/api/work-items/${encodeURIComponent(wrapperID)}`, {
      method: 'PATCH',
      headers: {
        'Content-Type': 'application/json',
        'X-GEMBA-Confirm': `${nonceBase}-wrapper-patch-${Date.now()}`,
      },
      body: JSON.stringify({ state_category: 'started' }),
    });
    if (!patch.ok) {
      const txt = await patch.text().catch(() => '');
      throw new Error(`demo fallback PATCH ${wrapperID} -> started failed ${patch.status}: ${txt}`);
    }
  }
  const cascade = await fetch(`${ctx.baseURL}/api/work-items/${encodeURIComponent(wrapperID)}/cascade-dispatch`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'X-GEMBA-Confirm': `${nonceBase}-cascade-${Date.now()}`,
    },
    body: JSON.stringify({ agent_type: process.env.GEMBA_ACCEPTANCE_AGENT ?? 'claude' }),
  });
  if (!cascade.ok && cascade.status !== 409) {
    const txt = await cascade.text().catch(() => '');
    throw new Error(`demo fallback cascade-dispatch ${wrapperID} failed ${cascade.status}: ${txt}`);
  }
}

async function importBeadsOnce(
  importedPacks: Set<string>,
  projectDir: string,
  jsonlPath: string,
  beadPrefix: string,
): Promise<void> {
  const key = `${beadPrefix}:${jsonlPath}`;
  if (importedPacks.has(key)) return;
  await importBeadsCLI(projectDir, jsonlPath, beadPrefix);
  importedPacks.add(key);
}

async function ensureDemoDispatchStarted(ctx: SharedContext, beadID: string): Promise<void> {
  const observed = await waitForWorkItemCategory(ctx, beadID, ['started', 'completed'], 3_000);
  if (observed) return;
  const nonceBase = process.env.GEMBA_E2E_CONFIRM_NONCE ?? 'acceptance-test';
  const patchNonce = `${nonceBase}-patch-${Date.now()}`;
  const startNonce = `${nonceBase}-start-${Date.now()}`;
  const patch = await fetch(`${ctx.baseURL}/api/work-items/${encodeURIComponent(beadID)}`, {
    method: 'PATCH',
    headers: {
      'Content-Type': 'application/json',
      'X-GEMBA-Confirm': patchNonce,
    },
    body: JSON.stringify({ state_category: 'started' }),
  });
  if (!patch.ok) {
    const txt = await patch.text().catch(() => '');
    throw new Error(`demo fallback PATCH ${beadID} -> started failed ${patch.status}: ${txt}`);
  }
  const start = await fetch(`${ctx.baseURL}/api/sessions`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'X-GEMBA-Confirm': startNonce,
    },
    body: JSON.stringify({ bead_id: beadID, agent_type: 'claude' }),
  });
  if (!start.ok && start.status !== 409) {
    const txt = await start.text().catch(() => '');
    throw new Error(`demo fallback POST /sessions for ${beadID} failed ${start.status}: ${txt}`);
  }
}

async function waitForWorkItemCategory(
  ctx: SharedContext,
  beadID: string,
  categories: string[],
  timeoutMs: number,
): Promise<boolean> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    const item = await fetchWorkItem(ctx.baseURL, beadID);
    if (item && categories.includes(item.state_category ?? '')) return true;
    await sleep(250);
  }
  return false;
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
      const item = await fetchWorkItem(baseURL, beadID);
      last = item ? `${item.state_category}/${item.status}` : 'not found';
      if (item && isClosedWorkItem(item)) return;
    } catch (err) {
      last = `fetch error: ${String((err as Error).message)}`;
    }
    await sleep(2_000);
  }
  throw new Error(
    `bead ${beadID} did not close within ${timeoutMs}ms (last: ${last ?? 'never observed'})`,
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

type RawIssue = {
  id: string;
  status?: string;
  state_category?: string;
  issue_type?: string;
  kind?: string;
  dependencies?: Array<{ issue_id?: string; depends_on_id?: string; type?: string }>;
  relationships?: Array<{ kind?: string; from?: string; to?: string }>;
};

type IssueSnapshot = {
  byID: Map<string, RawIssue>;
  childrenByParent: Map<string, string[]>;
};

async function collectOpenDescendants(baseURL: string, parentID: string): Promise<string[]> {
  const root = await fetchWorkItem(baseURL, parentID);
  if (root && isClosedWorkItem(root)) return [];
  const snap = await fetchWorkItemsSnapshot(baseURL);
  const open: string[] = [];
  const queue: string[] = [nativeBeadID(parentID)];
  const seen = new Set<string>();
  while (queue.length > 0) {
    const id = queue.shift()!;
    if (seen.has(id)) continue;
    seen.add(id);
    const node = snap.byID.get(id);
    if (!node) {
      open.push(id);
      continue;
    }
    if (id !== nativeBeadID(parentID) && !isWrapperIssue(node) && !isClosedWorkItem(node)) open.push(id);
    for (const child of snap.childrenByParent.get(id) ?? []) {
      queue.push(child);
    }
  }
  return open;
}

function loadIssueSnapshot(projectDir: string): IssueSnapshot {
  const file = path.join(projectDir, '.beads', 'issues.jsonl');
  const body = readFileSync(file, 'utf8');
  const byID = new Map<string, RawIssue>();
  const childrenByParent = new Map<string, string[]>();
  for (const line of body.split(/\r?\n/)) {
    if (line.trim() === '') continue;
    const issue = JSON.parse(line) as RawIssue;
    byID.set(issue.id, issue);
    for (const dep of issue.dependencies ?? []) {
      if (dep.type !== 'parent-child') continue;
      const child = dep.issue_id ?? issue.id;
      const parent = dep.depends_on_id;
      if (!child || !parent) continue;
      const children = childrenByParent.get(parent) ?? [];
      children.push(child);
      childrenByParent.set(parent, children);
    }
  }
  return { byID, childrenByParent };
}

async function fetchWorkItemsSnapshot(baseURL: string): Promise<IssueSnapshot> {
  const res = await fetch(`${baseURL}/api/work-items?limit=500`);
  if (!res.ok) throw new Error(`GET /api/work-items failed ${res.status}`);
  const json = (await res.json()) as { items?: RawIssue[]; workitems?: RawIssue[] };
  const items = json.items ?? json.workitems ?? [];
  const byID = new Map<string, RawIssue>();
  const childrenByParent = new Map<string, string[]>();
  for (const item of items) {
    byID.set(nativeBeadID(item.id), item);
  }
  for (const item of items) {
    const childID = nativeBeadID(item.id);
    for (const rel of item.relationships ?? []) {
      if (rel.kind !== 'parent_child') continue;
      const parent = nativeBeadID(rel.from ?? '');
      const child = nativeBeadID(rel.to ?? childID);
      if (!parent || !child) continue;
      const children = childrenByParent.get(parent) ?? [];
      if (!children.includes(child)) children.push(child);
      childrenByParent.set(parent, children);
    }
  }
  return { byID, childrenByParent };
}

async function fetchWorkItem(baseURL: string, beadID: string): Promise<RawIssue | null> {
  const res = await fetch(`${baseURL}/api/work-items/${encodeURIComponent(beadID)}`);
  if (res.ok) return (await res.json()) as RawIssue;
  if (res.status !== 404) throw new Error(`GET /api/work-items/${beadID} failed ${res.status}`);
  const snap = await fetchWorkItemsSnapshot(baseURL);
  return snap.byID.get(nativeBeadID(beadID)) ?? null;
}

function nativeBeadID(id: string): string {
  const slash = id.lastIndexOf('/');
  return slash >= 0 ? id.slice(slash + 1) : id;
}

function isClosedWorkItem(issue: RawIssue): boolean {
  const state = (issue.state_category ?? '').toLowerCase();
  if (state === 'completed' || state === 'canceled') return true;
  return ['closed', 'completed', 'done', 'canceled', 'cancelled'].includes(
    (issue.status ?? '').toLowerCase(),
  );
}

function isWrapperIssue(issue: RawIssue): boolean {
  return ['epic', 'milestone'].includes((issue.issue_type ?? issue.kind ?? '').toLowerCase());
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
      if (DEMO_MODE && kind === 'm1') {
        abs = writeDemoManualGateM1(abs, beadPrefix, projectDir);
      }
    } else {
      throw new Error(
        `bead pack not found: ${abs} (no .jsonl or .jsonl.tmpl)`,
      );
    }
  }
  if (packAlreadyImported(projectDir, abs)) return Promise.resolve();
  return new Promise((resolve, reject) => {
    const proc = spawn('bd', ['import', abs], { cwd: projectDir, stdio: ['ignore', 'pipe', 'pipe'] });
    let output = '';
    proc.stdout.on('data', (chunk: Buffer) => {
      const text = chunk.toString();
      output += text;
      process.stdout.write(text);
    });
    proc.stderr.on('data', (chunk: Buffer) => {
      const text = chunk.toString();
      output += text;
      process.stderr.write(text);
    });
    proc.on('exit', (code) => {
      if (code === 0) resolve();
      else if (/nothing to commit/i.test(output)) resolve();
      else reject(new Error(`bd import exited ${code} (path=${abs})`));
    });
    proc.on('error', (err) => reject(err));
  });
}

function writeDemoManualGateM1(jsonlPath: string, beadPrefix: string, projectDir: string): string {
  const firstTaskID = `${beadPrefix}-1`;
  const gateID = `${beadPrefix}-d2`;
  const body = readFileSync(jsonlPath, 'utf8');
  const rendered = body
    .split(/\r?\n/)
    .map((line) => {
      if (line.trim() === '') return line;
      const row = JSON.parse(line) as {
        id?: string;
        dependencies?: Array<{ issue_id?: string; depends_on_id?: string; type?: string }>;
      };
      if (row.id !== firstTaskID) return line;
      const dependencies = row.dependencies ?? [];
      const hasGate = dependencies.some(
        (dep) =>
          dep.issue_id === firstTaskID &&
          dep.depends_on_id === gateID &&
          dep.type === 'blocks',
      );
      if (!hasGate) {
        row.dependencies = [
          ...dependencies,
          { issue_id: firstTaskID, depends_on_id: gateID, type: 'blocks' },
        ];
      }
      return JSON.stringify(row);
    })
    .join('\n');
  const out = path.join(projectDir, 'm1.demo-manual-gate.jsonl');
  writeFileSync(out, rendered, 'utf8');
  return out;
}

function packAlreadyImported(projectDir: string, jsonlPath: string): boolean {
  const issuePath = path.join(projectDir, '.beads', 'issues.jsonl');
  if (!existsSync(issuePath)) return false;
  const want = issueIDsFromJSONL(jsonlPath);
  if (want.length === 0) return false;
  const snap = loadIssueSnapshot(projectDir);
  return want.every((id) => snap.byID.has(id));
}

function issueIDsFromJSONL(jsonlPath: string): string[] {
  const body = readFileSync(jsonlPath, 'utf8');
  const ids: string[] = [];
  for (const line of body.split(/\r?\n/)) {
    if (line.trim() === '') continue;
    const row = JSON.parse(line) as { id?: string };
    if (row.id) ids.push(row.id);
  }
  return ids;
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
