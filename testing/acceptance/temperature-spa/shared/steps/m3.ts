// shared/steps/m3.ts (gm-root.27.10) — M3 step.
//
// Imports the M3 bead pack, waits for the milestone to close, builds
// + serves the SPA, then loads it in Playwright and runs the
// strict-numeric oracle on the temperature conversion table.
//
// Per D18 §6.1 (M3 oracle):
//   • all M3 beads → completed
//   • build + preview gates as M2
//   • [data-testid="temperature-table"] exists
//   • exactly 16 [data-testid^="row-"] elements
//   • for each row, °F cell === (c * 9/5 + 32).toFixed(1) where c is
//     parsed from the testid suffix
//   • cherry-pick: row-0 = 32.0, row-100 = 212.0, row-300 = 572.0
//   • npm test (vitest) passes
//
// Numeric tolerance per D18 §6.2: STRICT-STRING — we compare the
// formatted string read from the cell against the expected
// `.toFixed(1)` string. No float compares.

import { spawn } from 'node:child_process';
import { existsSync } from 'node:fs';
import path from 'node:path';
import { setTimeout as sleep } from 'node:timers/promises';
import type { SharedContext } from '../spec';
import { waitForHttp } from './m1';

const M3_MILESTONE_ID = 'M3';

function m3Timeout(): number {
  return process.env.GEMBA_ACCEPTANCE_REAL_AGENTS === '1' ? 30 * 60_000 : 5 * 60_000;
}

export async function runM3Step(ctx: SharedContext): Promise<void> {
  await ctx.importBeads('target-jsonl/m3.jsonl');
  await ctx.waitForAllBeadsClosed(M3_MILESTONE_ID, m3Timeout());

  // Build.
  await runNpm(ctx, ['run', 'build'], {
    bugTitle: 'target build failed (M3)',
    severity: 'CRITICAL',
    failMsg: 'M3: npm run build exited non-zero',
  });
  if (!existsSync(path.join(ctx.projectDir, 'dist', 'index.html'))) {
    await ctx.fileBugBead({
      title: 'M3: dist/index.html missing after build',
      severity: 'CRITICAL',
      body: `projectDir=${ctx.projectDir}`,
    });
    throw new Error('M3 oracle: dist/index.html does not exist');
  }

  // Serve.
  const port = 30_000 + Math.floor(Math.random() * 20_000);
  const previewProc = spawn(
    'npx',
    ['vite', 'preview', '--port', String(port), '--strictPort'],
    { cwd: ctx.projectDir, stdio: ['ignore', 'pipe', 'pipe'], env: { ...process.env, BROWSER: 'none' } },
  );
  let stderr = '';
  previewProc.stderr?.on('data', (b: Buffer) => { stderr += b.toString(); });

  try {
    const ok = await waitForHttp(`http://127.0.0.1:${port}/`, 15_000);
    if (!ok) {
      await ctx.fileBugBead({
        title: `preview server unreachable on port ${port} (M3)`,
        severity: 'CRITICAL',
        body: `stderr:\n${stderr.slice(-2000)}`,
      });
      throw new Error(`M3: preview server unreachable on :${port}`);
    }

    await ctx.page.goto(`http://127.0.0.1:${port}/`, { waitUntil: 'domcontentloaded' });

    // ─── Table existence ──────────────────────────────────────
    const table = ctx.page.locator('[data-testid="temperature-table"]');
    await table.waitFor({ state: 'visible', timeout: 15_000 }).catch(async () => {
      await ctx.fileBugBead({
        title: 'missing testid: temperature-table',
        severity: 'HIGH',
        body: 'M3 expected [data-testid="temperature-table"] on the served page; not found.',
      });
      throw new Error('M3 oracle: temperature-table not found');
    });

    // ─── Row count ────────────────────────────────────────────
    const rows = ctx.page.locator('[data-testid^="row-"]');
    const rowCount = await rows.count();
    if (rowCount !== 16) {
      await ctx.fileBugBead({
        title: `row-count: expected 16, got ${rowCount}`,
        severity: 'HIGH',
        body: `M3 oracle row count mismatch.`,
      });
      throw new Error(`M3 oracle: expected 16 rows, got ${rowCount}`);
    }

    // ─── Per-row strict-numeric oracle ────────────────────────
    // Parse celsius from the testid; assume the °F cell is the
    // second cell (last <td>) of the row. We walk by index instead
    // of iterating the locator handle list to keep the assertion
    // ordering deterministic.
    for (let i = 0; i < rowCount; i++) {
      const row = rows.nth(i);
      const tid = await row.getAttribute('data-testid');
      if (tid == null) {
        throw new Error(`M3 oracle: row ${i} has no data-testid attribute`);
      }
      const m = /^row-(-?\d+)$/.exec(tid);
      if (!m) {
        await ctx.fileBugBead({
          title: `M3 oracle: malformed row testid: ${tid}`,
          severity: 'HIGH',
          body: `expected pattern row-{integer}, got ${tid}`,
        });
        throw new Error(`M3 oracle: malformed row testid ${tid}`);
      }
      const c = Number(m[1]);
      const expectedF = (c * 9 / 5 + 32).toFixed(1);
      const got = await readFahrenheitCell(row);
      if (got !== expectedF) {
        await ctx.fileBugBead({
          title: `oracle mismatch row-${c}: expected ${expectedF}, got ${got}`,
          severity: 'HIGH',
          body: `row index=${i}, testid=${tid}, expected=${expectedF}, got=${JSON.stringify(got)}`,
        });
        throw new Error(`M3 oracle: row-${c} expected °F=${expectedF}, got=${got}`);
      }
    }

    // ─── Cherry-pick assertions ───────────────────────────────
    for (const [c, expected] of [
      [0, '32.0'],
      [100, '212.0'],
      [300, '572.0'],
    ] as const) {
      const cell = ctx.page.locator(`[data-testid="row-${c}"]`);
      const count = await cell.count();
      if (count === 0) {
        await ctx.fileBugBead({
          title: `M3 oracle: cherry-pick row-${c} not present`,
          severity: 'HIGH',
          body: `Expected exactly one row with testid row-${c}; got ${count}.`,
        });
        throw new Error(`M3 oracle: cherry-pick row-${c} missing`);
      }
      const got = await readFahrenheitCell(cell.first());
      if (got !== expected) {
        await ctx.fileBugBead({
          title: `oracle mismatch row-${c}: expected ${expected}, got ${got}`,
          severity: 'HIGH',
          body: `Cherry-pick assertion failed.`,
        });
        throw new Error(`M3 cherry-pick row-${c}: expected ${expected}, got ${got}`);
      }
    }

    // ─── npm test ─────────────────────────────────────────────
    await runNpm(ctx, ['test', '--', '--run'], {
      bugTitle: 'target tests failed: M3',
      severity: 'HIGH',
      failMsg: 'M3: npm test failed',
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

/**
 * Reads the °F text out of a row. Convention: the row is a <tr> with
 * two <td>s (celsius, fahrenheit) OR has a child element with
 * data-testid="cell-f"/"f-cell". We try the testid path first, fall
 * back to "last cell text".
 */
async function readFahrenheitCell(row: import('@playwright/test').Locator): Promise<string> {
  const named = row.locator('[data-testid="cell-f"], [data-testid="f-cell"], [data-testid="fahrenheit-cell"]');
  if ((await named.count()) > 0) {
    return ((await named.first().textContent()) ?? '').trim();
  }
  const cells = row.locator('td');
  const n = await cells.count();
  if (n >= 2) {
    return ((await cells.nth(n - 1).textContent()) ?? '').trim();
  }
  // Fallback: full row text.
  return ((await row.textContent()) ?? '').trim();
}

interface NpmOpts {
  bugTitle: string;
  severity: 'CRITICAL' | 'HIGH';
  failMsg: string;
}

async function runNpm(ctx: SharedContext, args: string[], opts: NpmOpts): Promise<void> {
  const proc = spawn('npm', args, { cwd: ctx.projectDir, stdio: ['ignore', 'pipe', 'pipe'] });
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
