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
import { mkdtempSync, rmSync, existsSync, mkdirSync, readFileSync } from 'node:fs';
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
  /**
   * Isolated env vars that confine bd to this tempdir (gm-h4n).
   * BdClient and any other bd-shelling code MUST inherit this env so
   * commands can't leak into the operator's real workspace.
   */
  env: NodeJS.ProcessEnv;
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

  // gm-h4n: bd init can fall back to the shared :3307 Dolt server
  // (~/.beads/shared-server/) when one exists, silently leaking the
  // tempdir's --prefix into the operator's real workspace. Force pure
  // local-embedded mode by hiding the shared-server discovery dir:
  // override HOME to the tempdir so ~/.beads/ resolves to a fresh,
  // empty path bd can't see anything in. Apply the same env to gemba
  // serve below — its bd subprocesses inherit it.
  const isolatedEnv = {
    ...process.env,
    HOME: baseDir,
    PWD: baseDir,
    // Belt-and-suspenders: explicitly point bd at the tempdir's
    // workspace so it can't auto-discover anything else.
    BEADS_DIR: join(baseDir, '.beads'),
  };

  // bd init creates .beads/ in cwd. Use a unique short prefix so any
  // ids it stamps don't collide with the developer's local rigs if
  // a stray export ever leaks into a real workspace.
  const prefix = `e2e${opts.workerIndex}`;
  try {
    execFileSync(
      bdBin,
      [
        'init',
        '--prefix', prefix,
        '--non-interactive',
        '--quiet',
        '--skip-agents',
        '--skip-hooks',
      ],
      {
        cwd: baseDir,
        stdio: ['ignore', 'ignore', 'pipe'],
        env: isolatedEnv,
      }
    );
  } catch (err) {
    rmSync(baseDir, { recursive: true, force: true });
    throw new Error(`bd init failed in ${baseDir}: ${(err as Error).message}`);
  }

  // gm-h4n post-condition: confirm bd init produced a genuinely
  // isolated workspace. If bd somehow fell through to the shared
  // :3307 server (the bug this guard defends against), metadata.json
  // would show dolt_mode='server' OR a dolt_database that doesn't
  // match our --prefix. Either signal means HOME-isolation didn't
  // hold and the fixture is risking shared-state pollution; bail
  // out before the test runs and writes more state.
  try {
    assertEmbeddedIsolation(join(baseDir, '.beads'), prefix);
  } catch (err) {
    rmSync(baseDir, { recursive: true, force: true });
    throw err;
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
      // gm-h4n: gemba serve shells to bd internally; that bd inherits
      // this env, so the same HOME-isolation that protects bd init
      // protects every WorkPlane subprocess from leaking into the
      // shared :3307 server.
      env: isolatedEnv,
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
    env: isolatedEnv,
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

interface BeadsMetadata {
  dolt_mode?: string;
  dolt_database?: string;
  dolt_server_host?: string;
  dolt_server_port?: number;
}

// assertEmbeddedIsolation reads the .beads/metadata.json bd just
// wrote and confirms it reflects a genuinely isolated workspace
// (gm-h4n). Two failure modes the fixture absolutely must not let
// through:
//
//   1. dolt_mode !== 'embedded' — bd registered a server connection
//      somewhere, which means it found one. With HOME pointed at a
//      tempdir there is no server to find, so seeing this means the
//      isolation contract broke.
//   2. dolt_database does not match the prefix we passed — bd's
//      database name is derived from --prefix (with - → _). A
//      mismatch suggests bd loaded a workspace from elsewhere
//      (synced remote, fall-through to operator's real .beads).
//
// Either signal aborts the fixture before the test runs, converting
// silent shared-state corruption into a loud, actionable failure.
function assertEmbeddedIsolation(beadsDir: string, prefix: string): void {
  const metaPath = join(beadsDir, 'metadata.json');
  if (!existsSync(metaPath)) {
    throw new Error(
      `gm-h4n: bd init did not produce ${metaPath}. Isolation may have ` +
        `redirected to a non-embedded backend; check HOME / BEADS_DIR env.`
    );
  }
  let parsed: BeadsMetadata;
  try {
    parsed = JSON.parse(readFileSync(metaPath, 'utf8')) as BeadsMetadata;
  } catch (err) {
    throw new Error(
      `gm-h4n: ${metaPath} is not parseable JSON: ${(err as Error).message}`
    );
  }
  if (parsed.dolt_mode !== 'embedded') {
    throw new Error(
      `gm-h4n: bd init produced dolt_mode=${JSON.stringify(parsed.dolt_mode)} ` +
        `(expected 'embedded'). HOME / BEADS_DIR isolation failed; the fixture ` +
        `is at risk of writing into the shared :3307 Dolt server. ` +
        `metadata.json: ${JSON.stringify(parsed)}`
    );
  }
  // bd normalises - to _ in database names. Our prefix is 'e2e<N>'
  // which has no hyphens, but be defensive in case the convention
  // grows them.
  const expectedDb = prefix.replace(/-/g, '_');
  if (parsed.dolt_database !== expectedDb) {
    throw new Error(
      `gm-h4n: bd init produced dolt_database=${JSON.stringify(parsed.dolt_database)} ` +
        `(expected ${JSON.stringify(expectedDb)}). bd may have synced from a ` +
        `remote or fallen through to a different workspace.`
    );
  }
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
