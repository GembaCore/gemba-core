// shared/steps/m2.ts (gm-root.27.9) — M2 step.
//
// Imports the M2 bead pack, waits for the milestone to close, then
// builds + serves the SPA and verifies "Hello world" rendered with
// the expected testid.
//
// Per D18 §6.1 (M2 oracle):
//   • all M2 beads → completed
//   • npm run build exits 0; dist/index.html exists
//   • vite preview responds 200
//   • [data-testid="app-root"] textContent === "Hello world"
//   • npm test (vitest) passes

import { spawn } from 'node:child_process';
import { existsSync } from 'node:fs';
import path from 'node:path';
import { setTimeout as sleep } from 'node:timers/promises';
import type { SharedContext } from '../spec';
import { waitForHttp } from './m1';

const M2_MILESTONE_ID = 'M2';

function m2Timeout(): number {
  return process.env.GEMBA_ACCEPTANCE_REAL_AGENTS === '1' ? 30 * 60_000 : 5 * 60_000;
}

export async function runM2Step(ctx: SharedContext): Promise<void> {
  // ─── Import + wait for daemon ───────────────────────────────
  await ctx.importBeads('target-jsonl/m2.jsonl');
  await ctx.waitForAllBeadsClosed(M2_MILESTONE_ID, m2Timeout());

  // ─── npm run build ──────────────────────────────────────────
  await runNpm(ctx, ['run', 'build'], {
    bugTitle: 'target build failed',
    severity: 'CRITICAL',
    failMsg: 'M2: npm run build exited non-zero',
  });

  const distIndex = path.join(ctx.projectDir, 'dist', 'index.html');
  if (!existsSync(distIndex)) {
    await ctx.fileBugBead({
      title: 'M2: dist/index.html missing after build',
      severity: 'CRITICAL',
      body: `expected: ${distIndex}`,
    });
    throw new Error(`M2 oracle: dist/index.html does not exist`);
  }

  // ─── vite preview ───────────────────────────────────────────
  const port = 30_000 + Math.floor(Math.random() * 20_000);
  const previewProc = spawn(
    'npx',
    ['vite', 'preview', '--port', String(port), '--strictPort'],
    {
      cwd: ctx.projectDir,
      stdio: ['ignore', 'pipe', 'pipe'],
      env: { ...process.env, BROWSER: 'none' },
    },
  );
  let stderr = '';
  previewProc.stderr?.on('data', (b: Buffer) => { stderr += b.toString(); });

  try {
    const ok = await waitForHttp(`http://127.0.0.1:${port}/`, 15_000);
    if (!ok) {
      await ctx.fileBugBead({
        title: `preview server unreachable on port ${port}`,
        severity: 'CRITICAL',
        body: `stderr:\n${stderr.slice(-2000)}`,
      });
      throw new Error(`M2: preview server unreachable on :${port}`);
    }

    // ─── Playwright load + testid assertion ───────────────────
    await ctx.page.goto(`http://127.0.0.1:${port}/`, { waitUntil: 'domcontentloaded' });
    const root = ctx.page.locator('[data-testid="app-root"]');
    await root.waitFor({ state: 'visible', timeout: 15_000 }).catch(async () => {
      await ctx.fileBugBead({
        title: 'missing testid: app-root',
        severity: 'HIGH',
        body: `M2 expected [data-testid="app-root"] on the served page; not found.`,
      });
      throw new Error(`M2 oracle: [data-testid="app-root"] not found`);
    });
    const text = (await root.textContent())?.trim();
    if (text !== 'Hello world') {
      await ctx.fileBugBead({
        title: `M2 oracle mismatch: app-root text`,
        severity: 'HIGH',
        body: `expected: "Hello world"\ngot: ${JSON.stringify(text)}`,
      });
      throw new Error(`M2 oracle: app-root text "${text}" !== "Hello world"`);
    }

    // ─── npm test (vitest) ────────────────────────────────────
    await runNpm(ctx, ['test', '--', '--run'], {
      bugTitle: 'target tests failed: M2',
      severity: 'HIGH',
      failMsg: 'M2: npm test failed',
    });
  } finally {
    previewProc.kill('SIGTERM');
    await Promise.race([
      new Promise<void>((r) => previewProc.once('exit', () => r())),
      sleep(2_000),
    ]);
    if (!previewProc.killed && previewProc.exitCode == null) previewProc.kill('SIGKILL');
  }
}

interface NpmOpts {
  bugTitle: string;
  severity: 'CRITICAL' | 'HIGH';
  failMsg: string;
}

async function runNpm(ctx: SharedContext, args: string[], opts: NpmOpts): Promise<void> {
  const proc = spawn('npm', args, {
    cwd: ctx.projectDir,
    stdio: ['ignore', 'pipe', 'pipe'],
  });
  let stdout = '';
  let stderr = '';
  proc.stdout?.on('data', (b: Buffer) => { stdout += b.toString(); });
  proc.stderr?.on('data', (b: Buffer) => { stderr += b.toString(); });
  const code: number | null = await new Promise((resolve) => {
    proc.on('exit', (c) => resolve(c));
  });
  if (code !== 0) {
    await ctx.fileBugBead({
      title: opts.bugTitle,
      severity: opts.severity,
      body: `npm ${args.join(' ')} exited ${code}\n\nstdout:\n${stdout.slice(-2000)}\n\nstderr:\n${stderr.slice(-2000)}`,
    });
    throw new Error(`${opts.failMsg} (exit ${code})`);
  }
}
