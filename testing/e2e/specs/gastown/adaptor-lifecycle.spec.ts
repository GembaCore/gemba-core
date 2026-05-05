// Optional Gas Town adaptor e2e.
//
// Run explicitly:
//   make build
//   pnpm --filter gemba-e2e test:gastown
//
// This is not part of commit hooks or CI. It starts a real gemba server
// with --orchestration=gastown, but puts a deterministic gt shim at the
// front of PATH so the adaptor lifecycle is exercised without spending
// tokens or launching a real agent runtime.

import { expect, test, type APIRequestContext } from '@playwright/test';
import { execFileSync, spawn, type ChildProcess } from 'node:child_process';
import { existsSync, mkdtempSync, mkdirSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { createServer } from 'node:net';
import { fileURLToPath } from 'node:url';

type StartedServer = {
  baseURL: string;
  beadsRoot: string;
  statePath: string;
  dispose(): Promise<void>;
};

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(here, '..', '..', '..', '..');
const gembaBin = process.env.GEMBA_E2E_GEMBA_BIN ?? join(repoRoot, 'bin', 'gemba');
const bdBin = process.env.GEMBA_E2E_BD_BIN ?? 'bd';

test.describe('@gastown optional Gas Town adaptor lifecycle', () => {
  test('dispatches a bead through gt sling, observes status, and completes it @gastown', async ({
    page,
    request,
  }) => {
    test.skip(!existsSync(gembaBin), `gemba binary missing at ${gembaBin}; run make build first`);
    test.skip(!binaryAvailable(bdBin), `bd binary ${bdBin} is not available on PATH`);

    const server = await startShimmedGastownServer();
    try {
      const beadID = createBead(server.beadsRoot, 'Gas Town adaptor e2e target');

      await page.goto(`${server.baseURL}/board`);
      await expect(page.getByText('Gas Town adaptor e2e target')).toBeVisible({
        timeout: 15_000,
      });

      const capabilities = await request
        .get(`${server.baseURL}/api/capabilities`)
        .then((r) => r.json());
      expect(JSON.stringify(capabilities)).toContain('gastown');
      const adaptors = await request.get(`${server.baseURL}/api/adaptors`).then((r) => r.json());
      expect(JSON.stringify(adaptors)).toContain('gastown');
      expect(JSON.stringify(adaptors)).toContain('healthy');
      const orchestrationState = await request
        .get(`${server.baseURL}/api/orchestration/state`)
        .then((r) => r.json());
      expect(JSON.stringify(orchestrationState)).toContain('e2erig');

      const start = await request.post(`${server.baseURL}/api/sessions`, {
        headers: { 'X-GEMBA-Confirm': `gastown-e2e-${Date.now()}` },
        data: {
          bead_id: beadID,
          agent_type: 'codex',
          workspace: 'e2erig',
        },
      });
      expect(start.status(), await start.text()).toBe(201);
      const started = await start.json();
      expect(started.provider_metadata?.adaptor ?? started.ProviderMetadata?.adaptor).toBe(
        'gastown'
      );
      const sessionID = started.id ?? started.ID;

      await expect
        .poll(() => sessionStatus(server.baseURL, request, beadID), {
          message: 'gt shim should report the slung bead as working',
          timeout: 15_000,
          intervals: [500],
        })
        .toBe('working');

      const peek = await request.get(
        `${server.baseURL}/api/sessions/${encodeURIComponent(sessionID)}/peek`
      );
      expect(peek.status(), await peek.text()).toBe(200);
      expect(await peek.text()).toContain('gt e2e shim transcript');

      await page.goto(`${server.baseURL}/sessions`);
      await expect(page.getByText(beadID)).toBeVisible({ timeout: 10_000 });
      await expect(page.getByText(/working/i)).toBeVisible({ timeout: 10_000 });

      await expect
        .poll(() => sessionStatus(server.baseURL, request, beadID), {
          message: 'gt shim should transition the polecat to completed',
          timeout: 20_000,
          intervals: [500],
        })
        .toBe('completed');

      await expect
        .poll(() => workItemState(server.baseURL, request, beadID), {
          message: 'fake gt completion should close the underlying bead',
          timeout: 15_000,
          intervals: [500],
        })
        .toMatch(/completed|closed|done/);

      const shimState = JSON.parse(execFileSync('cat', [server.statePath], { encoding: 'utf8' }));
      expect(shimState.sling?.bead).toBe(beadID);
      expect(shimState.sling?.target).toBe('e2erig');
      expect(shimState.sling?.agent).toBe('codex');
    } finally {
      await server.dispose();
    }
  });
});

async function startShimmedGastownServer(): Promise<StartedServer> {
  const root = mkdtempSync(join(tmpdir(), 'gemba-gt-e2e-'));
  const home = join(root, 'home');
  const binDir = join(root, 'bin');
  const city = join(root, 'city');
  const beadsRoot = join(root, 'rig');
  const worktrees = join(root, 'worktrees');
  const statePath = join(root, 'gt-state.json');
  const shimPath = join(binDir, 'gt');
  mkdirSync(home, { recursive: true });
  mkdirSync(binDir, { recursive: true });
  mkdirSync(city, { recursive: true });
  mkdirSync(beadsRoot, { recursive: true });
  mkdirSync(worktrees, { recursive: true });
  initGitRepo(beadsRoot);
  writeFileSync(statePath, JSON.stringify({ sling: null }, null, 2));
  writeFileSync(shimPath, gtShimSource(), { mode: 0o755 });

  const env = {
    ...process.env,
    HOME: home,
    PWD: beadsRoot,
    BEADS_DIR: join(beadsRoot, '.beads'),
    PATH: `${binDir}:${process.env.PATH ?? ''}`,
    GT_E2E_STATE: statePath,
    GT_E2E_RIG: 'e2erig',
    GT_E2E_RIG_WORKTREE: beadsRoot,
    GT_E2E_BEADS_CWD: beadsRoot,
    GT_E2E_COMPLETE_MS: process.env.GEMBA_E2E_GASTOWN_COMPLETE_MS ?? '5000',
  };

  execFileSync(
    bdBin,
    ['init', '--prefix', 'ge2e', '--non-interactive', '--quiet', '--skip-agents', '--skip-hooks'],
    { cwd: beadsRoot, env, stdio: ['ignore', 'ignore', 'pipe'] }
  );

  const port = await pickFreePort();
  const args = [
    'serve',
    '--listen',
    '127.0.0.1',
    '--port',
    String(port),
    '--beads-dir',
    beadsRoot,
    '--worktrees-dir',
    worktrees,
    '--orchestration',
    'gastown',
    '--city',
    city,
    '--quiet',
  ];
  const child = spawn(gembaBin, args, {
    cwd: beadsRoot,
    env,
    stdio: ['ignore', 'pipe', 'pipe'],
  });
  const stderr: string[] = [];
  child.stderr?.on('data', (chunk) => stderr.push(chunk.toString()));
  child.stdout?.on('data', () => {
    /* drained */
  });

  const baseURL = `http://127.0.0.1:${port}`;
  try {
    await waitForHealth(baseURL, child, () => stderr.join(''));
  } catch (err) {
    await killGracefully(child);
    rmSync(root, { recursive: true, force: true });
    throw err;
  }

  return {
    baseURL,
    beadsRoot,
    statePath,
    dispose: async () => {
      await killGracefully(child);
      rmSync(root, { recursive: true, force: true });
    },
  };
}

function createBead(beadsRoot: string, title: string): string {
  return execFileSync(bdBin, ['create', title, '--type', 'task', '--priority', '2', '--silent'], {
    cwd: beadsRoot,
    env: { ...process.env, BEADS_DIR: join(beadsRoot, '.beads') },
    encoding: 'utf8',
  }).trim();
}

function initGitRepo(repo: string): void {
  execFileSync('git', ['init', '-q'], { cwd: repo });
  execFileSync('git', ['config', 'user.email', 'gemba-e2e@example.com'], {
    cwd: repo,
  });
  execFileSync('git', ['config', 'user.name', 'Gemba E2E'], { cwd: repo });
  writeFileSync(join(repo, 'README.md'), '# Gas Town adaptor e2e\n');
  execFileSync('git', ['add', 'README.md'], { cwd: repo });
  execFileSync('git', ['commit', '-q', '-m', 'init'], { cwd: repo });
}

async function sessionStatus(
  baseURL: string,
  request: APIRequestContext,
  beadID: string
): Promise<string> {
  const res = await request.get(`${baseURL}/api/sessions?include_terminal=true`);
  const body = await res.json();
  const rows = Array.isArray(body.sessions) ? body.sessions : [];
  const row = rows.find((s: any) => s.assignment_id === beadID || s.AssignmentID === beadID);
  return String(row?.status ?? row?.Status ?? '');
}

async function workItemState(
  baseURL: string,
  request: APIRequestContext,
  beadID: string
): Promise<string> {
  const single = await request.get(`${baseURL}/api/work-items/${encodeURIComponent(beadID)}`);
  if (single.ok()) {
    const body = await single.json();
    const item = body.item ?? body;
    return String(item?.state_category ?? item?.status ?? '');
  }
  const res = await request.get(`${baseURL}/api/work-items?status=closed&limit=200`);
  const body = await res.json();
  const rows = Array.isArray(body.items) ? body.items : [];
  const row = rows.find((w: any) => w.id === beadID || String(w.id ?? '').endsWith(`/${beadID}`));
  return String(row?.state_category ?? row?.status ?? '');
}

function binaryAvailable(bin: string): boolean {
  if (bin.includes('/')) return existsSync(bin);
  try {
    execFileSync('which', [bin], { stdio: 'ignore' });
    return true;
  } catch {
    return false;
  }
}

function pickFreePort(): Promise<number> {
  return new Promise((resolveFn, rejectFn) => {
    const srv = createServer();
    srv.unref();
    srv.on('error', rejectFn);
    srv.listen(0, '127.0.0.1', () => {
      const addr = srv.address();
      if (typeof addr !== 'object' || addr === null) {
        srv.close();
        rejectFn(new Error('pickFreePort: address was not a TCP address'));
        return;
      }
      srv.close(() => resolveFn(addr.port));
    });
  });
}

async function waitForHealth(
  baseURL: string,
  child: ChildProcess,
  stderr: () => string
): Promise<void> {
  const deadline = Date.now() + 30_000;
  let lastErr: unknown;
  while (Date.now() < deadline) {
    if (child.exitCode !== null) {
      throw new Error(`gemba serve exited early (${child.exitCode})\n${stderr()}`);
    }
    try {
      const res = await fetch(`${baseURL}/api/health`);
      await res.body?.cancel().catch(() => {});
      return;
    } catch (err) {
      lastErr = err;
    }
    await new Promise((resolveFn) => setTimeout(resolveFn, 100));
  }
  throw new Error(`gemba serve did not become healthy: ${String(lastErr)}\n${stderr()}`);
}

async function killGracefully(child: ChildProcess): Promise<void> {
  if (child.exitCode !== null || child.signalCode !== null) return;
  child.kill('SIGTERM');
  await Promise.race([
    new Promise<void>((resolveFn) => child.once('exit', () => resolveFn())),
    new Promise<void>((resolveFn) => setTimeout(resolveFn, 2_000)),
  ]);
  if (child.exitCode === null && child.signalCode === null) {
    child.kill('SIGKILL');
  }
}

function gtShimSource(): string {
  return `#!/usr/bin/env node
const fs = require('node:fs');
const cp = require('node:child_process');
const path = require('node:path');

const statePath = process.env.GT_E2E_STATE;
const rig = process.env.GT_E2E_RIG || 'e2erig';
const worktree = process.env.GT_E2E_RIG_WORKTREE || process.cwd();
const beadsCwd = process.env.GT_E2E_BEADS_CWD || worktree;
const completeMs = Number(process.env.GT_E2E_COMPLETE_MS || '1500');
const args = process.argv.slice(2);

function load() {
  try { return JSON.parse(fs.readFileSync(statePath, 'utf8')); }
  catch { return { sling: null }; }
}
function save(s) { fs.writeFileSync(statePath, JSON.stringify(s, null, 2)); }
function maybeComplete(s) {
  if (!s.sling || s.sling.closed) return s;
  if (Date.now() < s.sling.completeAt) return s;
  s.sling.closed = true;
  s.sling.state = 'completed';
  save(s);
  try {
    cp.execFileSync('bd', ['close', s.sling.bead], {
      cwd: beadsCwd,
      env: { ...process.env, BEADS_DIR: path.join(beadsCwd, '.beads') },
      stdio: 'ignore',
      timeout: 3000,
    });
  } catch (err) {
    s.sling.closeError = String(err && err.message || err);
  }
  save(s);
  return s;
}
function printJSON(v) { process.stdout.write(JSON.stringify(v)); }
function rigRow() {
  return {
    name: rig,
    beads_prefix: 'ge2e',
    status: 'operational',
    witness: 'running',
    refinery: 'running',
    polecats: 1,
    crew: 1,
    repository: 'file://' + worktree,
    branch: 'main',
    worktree_path: worktree,
  };
}

if (args[0] === '--version' || args[0] === 'version') {
  console.log('gt version 1.0.0');
  process.exit(0);
}
if (args[0] === 'sling' && args[1] === '--help') {
  console.log('Usage: gt sling <bead> [target] --json --agent <agent> --create');
  process.exit(0);
}
if (args[1] === '--help') {
  console.log('ok');
  process.exit(0);
}
if (args.join(' ') === 'dolt status') {
  console.log('Dolt server OK');
  process.exit(0);
}
if (args.join(' ') === 'rig list --json') {
  printJSON([rigRow()]);
  process.exit(0);
}
if (args.join(' ') === 'polecat list --all --json') {
  const s = maybeComplete(load());
  if (!s.sling) {
    printJSON([{ rig, name: 'e2e', state: 'idle', session_running: true }]);
  } else if (s.sling.closed) {
    printJSON([{ rig, name: 'e2e', state: 'idle', issue: s.sling.bead, session_running: false }]);
  } else {
    printJSON([{ rig, name: 'e2e', state: 'working', issue: s.sling.bead, session_running: true }]);
  }
  process.exit(0);
}
if (args.join(' ') === 'convoy list --json') {
  const s = maybeComplete(load());
  if (!s.sling) {
    printJSON([]);
  } else {
    printJSON([{
      id: 'gt-e2e-convoy',
      title: 'Work: ' + s.sling.bead,
      status: s.sling.closed ? 'closed' : 'open',
      rig,
      issues: [s.sling.bead],
      polecats: ['e2e'],
      workers: [{
        rig,
        polecat: 'e2e',
        bead: s.sling.bead,
        status: s.sling.closed ? 'completed' : 'working',
      }],
      created_at: new Date(s.sling.startedAt).toISOString(),
      closed_at: s.sling.closed ? new Date().toISOString() : undefined,
    }]);
  }
  process.exit(0);
}
if (args[0] === 'sling') {
  const bead = args[1];
  const target = args[2] || rig;
  const agentIndex = args.indexOf('--agent');
  const agent = agentIndex >= 0 ? args[agentIndex + 1] : '';
  const now = Date.now();
  const s = load();
  s.sling = { bead, target, agent, startedAt: now, completeAt: now + completeMs, state: 'working', closed: false };
  save(s);
  console.log('ok');
  process.exit(0);
}
if (args[0] === 'peek') {
  console.log('gt e2e shim transcript: working on ' + (load().sling && load().sling.bead || 'nothing'));
  process.exit(0);
}
if (args[0] === 'mail' && args[1] === 'inbox') {
  printJSON([]);
  process.exit(0);
}
if (args[0] === 'escalate' && args[1] === 'list') {
  printJSON([]);
  process.exit(0);
}
if (args[0] === 'unsling' || args[0] === 'release') {
  const s = load();
  if (s.sling) {
    s.sling.closed = true;
    s.sling.state = args[0] === 'release' ? 'failed' : 'completed';
    save(s);
  }
  console.log('ok');
  process.exit(0);
}

console.error('gt e2e shim: unsupported command: ' + args.join(' '));
process.exit(1);
`;
}
