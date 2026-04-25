// fixtures/realServer.ts
//
// gm-5v8v.2 — real-backend launcher for the deep matrix.
//
// Per Playwright worker:
//
//   1. Make a tempdir under $TMPDIR.
//   2. `bd init` inside it (creates `.beads/` with an embedded Dolt
//      engine local to that directory — sidesteps the shared
//      :3307 server entirely; no orphan-DB risk).
//   3. Pick a free TCP port via net.createServer().listen(0).
//   4. Spawn `bin/gemba serve --beads-dir <td> --port <p> --quiet`.
//   5. Poll GET /api/health until 200 or 30s.
//   6. Tear down on dispose: kill the gemba child + rm -rf the tempdir.
//
// Embedded-Dolt-per-tempdir is deliberate: the shared :3307 server is
// fragile and operators have already paid for orphan-DB pollution
// (see gt CLAUDE.md). This fixture stays out of that whole problem
// space — every worker is fully isolated on its own filesystem subtree.
//
// One server per worker (Playwright `scope: 'worker'`). The server
// outlives every test the worker runs, which keeps spinup amortized
// and tests fast. Tests reset bead state between runs via BdClient.

import { spawn, execFileSync, type ChildProcess } from 'node:child_process';
import { mkdtempSync, rmSync, existsSync, mkdirSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { createServer } from 'node:net';
import { fileURLToPath } from 'node:url';

export type RealServer = {
  /** http://127.0.0.1:<port> — pass to Playwright as baseURL. */
  baseURL: string;
  /** Workspace dir gemba is pointed at. Equals the bd cwd. */
  beadsDir: string;
  /** Per-worker worktrees parent. */
  worktreesDir: string;
  /** Server PID (mostly for diagnostics). */
  pid: number;
  /** Captured server stderr — handy when a spec fails. */
  stderr(): string;
  /** Kill the server + remove the tempdir. Idempotent. */
  dispose(): Promise<void>;
};

export type SpinOptions = {
  /** Playwright worker index — used to label the tempdir. */
  workerIndex: number;
  /** Override the gemba binary path. Defaults to repo bin/gemba. */
  gembaBin?: string;
  /** Override the bd binary. Defaults to whatever's on PATH. */
  bdBin?: string;
  /** Boot timeout in ms. Defaults to 30s. */
  bootTimeoutMs?: number;
};

/** Spin a real gemba serve instance against a per-worker beads workspace. */
export async function spinRealServer(opts: SpinOptions): Promise<RealServer> {
  const gembaBin = opts.gembaBin ?? defaultGembaBin();
  const bdBin = opts.bdBin ?? 'bd';
  const bootTimeoutMs = opts.bootTimeoutMs ?? 30_000;

  if (!existsSync(gembaBin)) {
    throw new Error(
      `gemba binary not found at ${gembaBin}. ` +
        `Run \`make build\` first, or pass GEMBA_E2E_GEMBA_BIN=/path/to/gemba.`
    );
  }

  const baseDir = mkdtempSync(join(tmpdir(), `gemba-e2e-w${opts.workerIndex}-`));
  const worktreesDir = join(baseDir, 'worktrees');
  mkdirSync(worktreesDir, { recursive: true });

  // bd init creates .beads/ in cwd. Use a unique short prefix so any
  // ids it stamps don't collide with the developer's local rigs if
  // a stray export ever leaks into a real workspace.
  try {
    execFileSync(
      bdBin,
      [
        'init',
        '--prefix', `e2e${opts.workerIndex}`,
        '--non-interactive',
        '--quiet',
        '--skip-agents',
        '--skip-hooks',
      ],
      {
        cwd: baseDir,
        stdio: ['ignore', 'ignore', 'pipe'],
      }
    );
  } catch (err) {
    rmSync(baseDir, { recursive: true, force: true });
    throw new Error(`bd init failed in ${baseDir}: ${(err as Error).message}`);
  }

  const port = await pickFreePort();
  const child = spawn(
    gembaBin,
    [
      'serve',
      '--listen', '127.0.0.1',
      '--port', String(port),
      '--beads-dir', baseDir,
      '--worktrees-dir', worktreesDir,
      '--quiet',
    ],
    {
      cwd: baseDir,
      stdio: ['ignore', 'pipe', 'pipe'],
      // Detach so we can group-kill on dispose. Treat the child as a
      // disposable resource; never inherit the parent's signals.
      env: { ...process.env, PWD: baseDir },
    }
  );

  const stderrChunks: string[] = [];
  child.stderr?.on('data', (c) => stderrChunks.push(c.toString()));
  child.stdout?.on('data', () => {/* drained but ignored */});

  const baseURL = `http://127.0.0.1:${port}`;
  let exited: { code: number | null; signal: NodeJS.Signals | null } | undefined;
  child.once('exit', (code, signal) => {
    exited = { code, signal };
  });

  try {
    await waitForHealth(baseURL, bootTimeoutMs, () => exited);
  } catch (err) {
    child.kill('SIGKILL');
    rmSync(baseDir, { recursive: true, force: true });
    throw new Error(
      `${(err as Error).message}\n\nServer stderr:\n${stderrChunks.join('') || '(empty)'}`
    );
  }

  let disposed = false;
  return {
    baseURL,
    beadsDir: baseDir,
    worktreesDir,
    pid: child.pid ?? -1,
    stderr: () => stderrChunks.join(''),
    dispose: async () => {
      if (disposed) return;
      disposed = true;
      await killGracefully(child);
      rmSync(baseDir, { recursive: true, force: true });
    },
  };
}

function defaultGembaBin(): string {
  if (process.env.GEMBA_E2E_GEMBA_BIN) return process.env.GEMBA_E2E_GEMBA_BIN;
  // testing/e2e/fixtures/realServer.ts → repo root is two parents up.
  const here = dirname(fileURLToPath(import.meta.url));
  return resolve(here, '..', '..', '..', 'bin', 'gemba');
}

async function pickFreePort(): Promise<number> {
  return new Promise((resolveFn, rejectFn) => {
    const srv = createServer();
    srv.unref();
    srv.on('error', rejectFn);
    srv.listen(0, '127.0.0.1', () => {
      const addr = srv.address();
      if (typeof addr !== 'object' || addr === null) {
        srv.close();
        rejectFn(new Error('pickFreePort: net.Server.address() returned non-object'));
        return;
      }
      const { port } = addr;
      srv.close(() => resolveFn(port));
    });
  });
}

async function waitForHealth(
  baseURL: string,
  timeoutMs: number,
  exitedRef: () => { code: number | null; signal: NodeJS.Signals | null } | undefined
): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  let lastErr: unknown;
  while (Date.now() < deadline) {
    const exited = exitedRef();
    if (exited) {
      throw new Error(
        `gemba serve exited before becoming healthy (code=${exited.code} signal=${exited.signal})`
      );
    }
    try {
      const res = await fetch(`${baseURL}/api/health`);
      if (res.ok) return;
      lastErr = new Error(`/api/health returned ${res.status}`);
    } catch (err) {
      lastErr = err;
    }
    await sleep(100);
  }
  throw new Error(
    `gemba serve did not become healthy at ${baseURL} within ${timeoutMs}ms; ` +
      `last error: ${lastErr instanceof Error ? lastErr.message : String(lastErr)}`
  );
}

async function killGracefully(child: ChildProcess): Promise<void> {
  if (child.exitCode !== null || child.signalCode !== null) return;
  return new Promise((resolveFn) => {
    const timer = setTimeout(() => {
      try { child.kill('SIGKILL'); } catch { /* already gone */ }
      resolveFn();
    }, 5_000);
    child.once('exit', () => {
      clearTimeout(timer);
      resolveFn();
    });
    try { child.kill('SIGTERM'); } catch { /* already gone */ }
  });
}

function sleep(ms: number): Promise<void> {
  return new Promise((r) => setTimeout(r, ms));
}
