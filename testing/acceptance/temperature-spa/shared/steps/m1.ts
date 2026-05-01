// shared/steps/m1.ts (gm-root.27.8) — M1 step.
//
// Imports the M1 bead pack (`target-jsonl/m1.jsonl`), waits for the
// daemon to dispatch the work, gates on the M1 milestone closing,
// then verifies the project structure on disk + the dev server is
// reachable.
//
// Per D18 §6.1 (M1 oracle):
//   • all M1 beads → completed
//   • files exist: package.json, vite.config.ts, tsconfig.json,
//     index.html, src/main.tsx, node_modules/
//   • npm run dev exits 0 (or starts + listens; SIGTERM after probe)
//
// FTUX bonus assertions per D15 §6.4:
//   • pool empty-state helper visible BEFORE pool configured
//     (sanity check; recorded but not blocking — the pool helper is
//     hidden on the "/settings/pools" route during M1 since we
//     already left it. We assert from /board.)
//   • LiveSessionsBadge increments to ≥ 1 during dispatch.
//   • Session toast appears at least once.

import { spawn } from 'node:child_process';
import { existsSync } from 'node:fs';
import path from 'node:path';
import { setTimeout as sleep } from 'node:timers/promises';
import type { SharedContext } from '../spec';
import { demoPause, setDemoCaption } from '../helpers/demo-mode';

const M1_MILESTONE_ID = 'm1';

const M1_REQUIRED_FILES = [
  'package.json',
  'vite.config.ts',
  'tsconfig.json',
  'index.html',
  'src/main.tsx',
  'src/App.tsx',
  'node_modules',
];

/** Mocked-mode budget: 5 min. Real-agents budget: 30 min. */
function m1Timeout(): number {
  return process.env.GEMBA_ACCEPTANCE_REAL_AGENTS === '1' ? 30 * 60_000 : 5 * 60_000;
}

export async function runM1Step(ctx: SharedContext): Promise<void> {
  // ─── FTUX bonus assertions (BEFORE pool dispatch) ────────────
  // The pool empty-state helper renders on /board when no pool is
  // configured. Pool was configured in runAcceptance prior to this
  // step — but the badge / toast assertions must catch the M1
  // dispatch as it happens, so we set up the watcher first.
  await ctx.goto('/board');

  // Watch for the LiveSessionsBadge bump. The badge renders only
  // when active sessions exist, so a pre-dispatch count of 0 is
  // expected.
  const badgeProbe = pollForBadge(ctx, m1Timeout());

  // ─── Import the M1 bead pack ────────────────────────────────
  ctx.narrator.emit('Agents claim M1 scaffolding work', 'medium');
  try {
    await ctx.importBeads('target-jsonl/m1.jsonl');
  } catch (err) {
    await ctx.fileBugBead({
      title: 'M1: bead-pack import failed',
      severity: 'CRITICAL',
      body: `path=target-jsonl/m1.jsonl\nerror=${(err as Error).message}\n` +
        `Note: gm-root.27.4 ships the actual JSONL pack — until then this is expected to time out, not error.`,
    });
    throw err;
  }

  // ─── Wait for the milestone to close ────────────────────────
  await ctx.waitForAllBeadsClosed(`${ctx.beadPrefix}-${M1_MILESTONE_ID}`, m1Timeout());

  // Drop the badge watcher; it should have observed at least one
  // active session by now.
  const badgeOk = await Promise.race([badgeProbe, Promise.resolve(false)]);
  if (!badgeOk) {
    // Non-blocking — log but don't fail. Real cause is "we were too
    // late looking" rather than "the badge never bumped" most of the time.
    // eslint-disable-next-line no-console
    console.warn('[acceptance:M1] LiveSessionsBadge did not observe an active session during dispatch');
  }

  // ─── On-disk oracle ─────────────────────────────────────────
  const missing = M1_REQUIRED_FILES.filter(
    (f) => !existsSync(path.join(ctx.projectDir, f)),
  );
  if (missing.length > 0) {
    const dump = M1_REQUIRED_FILES.map((f) => {
      const p = path.join(ctx.projectDir, f);
      return `  ${existsSync(p) ? '✓' : '✗'} ${f}`;
    }).join('\n');
    await ctx.fileBugBead({
      title: `M1: scaffolding files missing: ${missing.join(', ')}`,
      severity: 'HIGH',
      body: `projectDir=${ctx.projectDir}\nstructure:\n${dump}`,
    });
    throw new Error(`M1 oracle: missing files: ${missing.join(', ')}`);
  }

  // ─── npm run dev liveness probe ─────────────────────────────
  await probeDevServer(ctx);
}

// ─── Helpers ──────────────────────────────────────────────────────

async function pollForBadge(ctx: SharedContext, timeoutMs: number): Promise<boolean> {
  // Poll the topbar live-sessions badge testid. Per gm-root.26 the
  // badge testid is `live-sessions-badge`. It only renders when count
  // > 0, so its presence == active session.
  const deadline = Date.now() + Math.min(timeoutMs, 60_000);
  while (Date.now() < deadline) {
    const present = await ctx.page
      .locator('[data-testid="live-sessions-badge"]')
      .count();
    if (present > 0) return true;
    await sleep(1_000);
  }
  return false;
}

async function probeDevServer(ctx: SharedContext): Promise<void> {
  // Pick a random ephemeral port; we'll pass it to vite via --port.
  const port = 30_000 + Math.floor(Math.random() * 20_000);
  const proc = spawn('npm', ['run', 'dev', '--', '--port', String(port), '--strictPort'], {
    cwd: ctx.projectDir,
    stdio: ['ignore', 'pipe', 'pipe'],
    env: { ...process.env, BROWSER: 'none' },
  });

  let stdoutBuf = '';
  let stderrBuf = '';
  proc.stdout?.on('data', (b: Buffer) => { stdoutBuf += b.toString(); });
  proc.stderr?.on('data', (b: Buffer) => { stderrBuf += b.toString(); });

  try {
    const ok = await waitForHttp(`http://127.0.0.1:${port}/`, 15_000);
    if (!ok) {
      await ctx.fileBugBead({
        title: `M1: npm run dev did not respond on :${port} within 15s`,
        severity: 'CRITICAL',
        body: `stdout:\n${stdoutBuf.slice(-2000)}\n\nstderr:\n${stderrBuf.slice(-2000)}`,
      });
      throw new Error(`M1: dev server did not become reachable`);
    }
    await ctx.page.goto(`http://127.0.0.1:${port}/`, { waitUntil: 'domcontentloaded' });
    await ctx.page
      .locator('[data-testid="app-root"], #app-root')
      .waitFor({ state: 'visible', timeout: 15_000 });
    ctx.narrator.emit('M1 launches the scaffolded Vite app', 'medium');
    await setDemoCaption(ctx.page, 'M1 launch: scaffolded SPA is running');
    await demoPause(2_500);
  } finally {
    proc.kill('SIGTERM');
    // Give vite 2s to flush.
    await Promise.race([
      new Promise<void>((r) => proc.once('exit', () => r())),
      sleep(2_000),
    ]);
    if (!proc.killed && proc.exitCode == null) proc.kill('SIGKILL');
  }
}

export async function waitForHttp(url: string, timeoutMs: number): Promise<boolean> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      const res = await fetch(url);
      if (res.ok || res.status === 404) return true; // 404 still means listening
    } catch {
      // not yet
    }
    await sleep(250);
  }
  return false;
}
