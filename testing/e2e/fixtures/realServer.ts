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
import { mkdtempSync, rmSync, existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { createServer } from 'node:net';
import { fileURLToPath } from 'node:url';

/**
 * Workspace mode (gm-5v8v.11.1) — persisted in
 * `<beadsDir>/.gemba/workspace.toml` BEFORE `gemba serve` starts so the
 * future workspace-config reader (mode-gated nonce / audit-chain
 * behavior) sees a stable value at boot. Mirrors the fixture-side
 * WorkspaceMode in fixtures/modes.ts.
 */
export type WorkspaceMode = 'unsupervised' | 'supervised' | 'managed';

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
   * Plaintext auth token captured from stderr when this server was
   * spawned with `auth: 'token'` (gm-5v8v.12.1). Undefined under any
   * other auth mode. Specs use it as `Authorization: Bearer <token>`.
   */
  authToken?: string;
  /**
   * Exit code captured when the spawn was deliberately allowed to
   * fail (`expectBootFailure: true`). Undefined for normal long-
   * lived servers since they don't exit until dispose.
   */
  exitCode?: number | null;
  /** Exit signal under the same condition as `exitCode`. */
  exitSignal?: NodeJS.Signals | null;
  /**
   * Isolated env vars that confine bd to this tempdir (gm-h4n).
   * BdClient and any other bd-shelling code MUST inherit this env so
   * commands can't leak into the operator's real workspace.
   */
  env: NodeJS.ProcessEnv;
  /** Captured server stderr — handy when a spec fails. */
  stderr(): string;
  /**
   * Workspace mode the fixture wrote into `.gemba/workspace.toml`
   * before spawning gemba (gm-5v8v.11.1). Undefined when the spec did
   * not pass `mode` — the fixture does not write a default file in
   * that case so the caller's tempdir is byte-identical to the
   * pre-gm-5v8v.11.1 layout.
   */
  mode?: WorkspaceMode;
  /**
   * Absolute path of `.gemba/workspace.toml` the fixture wrote
   * (gm-5v8v.11.1). Specs assert on the persisted file via this path
   * — the contract the deep-mode tests exercise is "the file gemba
   * sees at boot lives here and contains the chosen mode". Undefined
   * when `mode` was not passed.
   */
  workspaceTomlPath?: string;
  /** Kill the server + remove the tempdir. Idempotent. */
  dispose(): Promise<void>;
};

export type AuthMode = 'open' | 'token';

export type SpinOptions = {
  /** Playwright worker index — used to label the tempdir. */
  workerIndex: number;
  /** Override the gemba binary path. Defaults to repo bin/gemba. */
  gembaBin?: string;
  /** Override the bd binary. Defaults to whatever's on PATH. */
  bdBin?: string;
  /** Boot timeout in ms. Defaults to 30s. */
  bootTimeoutMs?: number;

  // gm-5v8v.12.1 — auth fixture options
  /**
   * Authentication mode. Default behavior (omit) maps to gemba's
   * "auth open" default. Pass 'token' to spawn with --auth=token; the
   * fixture parses the bootstrapped token from stderr and stamps it
   * onto RealServer.authToken.
   */
  auth?: AuthMode;
  /**
   * Bind interface override. Default 127.0.0.1. Pass 0.0.0.0 (or
   * similar) to test the gm-99g sanity gate that refuses to start an
   * open-auth server on a non-loopback interface.
   */
  listen?: string;
  /**
   * Forward `--dangerously-skip-permissions` so /api/config returns
   * yolo_available=true. Default false.
   */
  dangerouslySkipPermissions?: boolean;
  /**
   * If true, the spawn is expected to exit non-zero before /api/health
   * goes green. The function returns a RealServer with authToken+
   * baseURL undefined and exitCode set. Specs use this for the
   * loopback / bind-policy negative tests (gm-99g).
   */
  expectBootFailure?: boolean;

  // gm-5v8v.11.1 — workspace-mode fixture
  /**
   * Workspace mode to persist into `<beadsDir>/.gemba/workspace.toml`
   * BEFORE spawning `gemba serve`. The file is written with a single
   * `mode = "<value>"` line (plus a fixture-attribution comment) so a
   * future workspace-config reader sees a stable mode at boot.
   *
   * Omit to keep the pre-gm-5v8v.11.1 behavior (no .gemba/ created by
   * the fixture).
   */
  mode?: WorkspaceMode;
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

  // gm-5v8v.11.1: persist the workspace mode BEFORE the server boots.
  // Future readers (mode-gated nonce + audit-chain) parse this file
  // during gemba's startup; writing AFTER spawn would race the boot.
  // The .gemba directory lives alongside .beads in the workspace root,
  // matching the convention used by .gemba/personas, .gemba/agents.toml,
  // and .gemba/repositories elsewhere in the codebase.
  let workspaceTomlPath: string | undefined;
  if (opts.mode) {
    const gembaDir = join(baseDir, '.gemba');
    mkdirSync(gembaDir, { recursive: true });
    workspaceTomlPath = join(gembaDir, 'workspace.toml');
    const body =
      `# gm-5v8v.11.1 — written by testing/e2e/fixtures/realServer.ts\n` +
      `# Workspace mode for the deep-modes spec matrix. Do not edit by hand.\n` +
      `mode = "${opts.mode}"\n`;
    writeFileSync(workspaceTomlPath, body, { encoding: 'utf8' });
  }

  const listen = opts.listen ?? '127.0.0.1';
  const port = await pickFreePort();

  const args = [
    'serve',
    '--listen', listen,
    '--port', String(port),
    '--beads-dir', baseDir,
    '--worktrees-dir', worktreesDir,
    '--quiet',
  ];
  if (opts.auth) {
    args.push('--auth', opts.auth === 'open' ? 'none' : opts.auth);
  }
  if (opts.dangerouslySkipPermissions) {
    args.push('--dangerously-skip-permissions');
  }
  // gemba serve defaults to --orchestration=native, which auto-detects
  // its terminal backend from TMUX or TERM_PROGRAM. Linux CI runners
  // set neither, so the server crashes before /api/health responds —
  // that's what was tanking every [smoke-deep] spec. Force a tmux
  // backend (preinstalled on the ubuntu runners gemba targets) when
  // env-detect would fail; macOS dev keeps its iTerm/Terminal.app
  // auto-detect because TERM_PROGRAM is set there.
  //
  // The chosen backend is just enough to satisfy the startup probe;
  // the smoke specs that fail today don't actually exercise pane
  // creation. Tests that DO drive native sessions can override by
  // adding `--orchestration` / `--terminal` ahead of this point in a
  // future opts field.
  const hasTerminalEnv =
    !!process.env.TMUX ||
    (typeof process.env.TERM_PROGRAM === 'string' &&
      process.env.TERM_PROGRAM.length > 0);
  if (!hasTerminalEnv && !args.includes('--orchestration') && !args.includes('--terminal')) {
    args.push('--terminal', 'tmux');
  }

  const child = spawn(gembaBin, args, {
    cwd: baseDir,
    stdio: ['ignore', 'pipe', 'pipe'],
    // gm-h4n: gemba serve shells to bd internally; that bd inherits
    // this env, so the same HOME-isolation that protects bd init
    // protects every WorkPlane subprocess from leaking into the
    // shared :3307 server.
    env: isolatedEnv,
  });

  const stderrChunks: string[] = [];
  child.stderr?.on('data', (c) => stderrChunks.push(c.toString()));
  child.stdout?.on('data', () => {/* drained but ignored */});

  const baseURL = `http://${listen}:${port}`;
  let exited: { code: number | null; signal: NodeJS.Signals | null } | undefined;
  child.once('exit', (code, signal) => {
    exited = { code, signal };
  });

  // gm-5v8v.12.1: expected-boot-failure path. Used for negative tests
  // like the gm-99g sanity gate (--listen=0.0.0.0 + --auth=open must
  // refuse to start). Wait for exit, do NOT poll /api/health, return
  // with exitCode populated and baseURL undefined.
  if (opts.expectBootFailure) {
    const exitInfo = await waitForExit(child, bootTimeoutMs);
    rmSync(baseDir, { recursive: true, force: true });
    return {
      baseURL: '',
      beadsDir: baseDir,
      worktreesDir,
      pid: child.pid ?? -1,
      env: isolatedEnv,
      exitCode: exitInfo.code,
      exitSignal: exitInfo.signal,
      mode: opts.mode,
      workspaceTomlPath,
      stderr: () => stderrChunks.join(''),
      dispose: async () => { /* no-op — already cleaned */ },
    };
  }

  try {
    await waitForHealth(baseURL, bootTimeoutMs, () => exited);
  } catch (err) {
    child.kill('SIGKILL');
    rmSync(baseDir, { recursive: true, force: true });
    throw new Error(
      `${(err as Error).message}\n\nServer stderr:\n${stderrChunks.join('') || '(empty)'}`
    );
  }

  // Parse the bootstrapped auth token from stderr when --auth=token.
  // ensurePrimaryToken in cli/serve.go writes a recognizable banner
  // with the literal "Token:" line; we grep that.
  let authToken: string | undefined;
  if (opts.auth === 'token') {
    authToken = parseAuthTokenFromStderr(stderrChunks.join(''));
    if (!authToken) {
      child.kill('SIGKILL');
      rmSync(baseDir, { recursive: true, force: true });
      throw new Error(
        `gemba serve --auth=token did not print a bootstrap token to stderr.\n` +
          `stderr:\n${stderrChunks.join('') || '(empty)'}`
      );
    }
  }

  let disposed = false;
  return {
    baseURL,
    beadsDir: baseDir,
    worktreesDir,
    pid: child.pid ?? -1,
    authToken,
    env: isolatedEnv,
    mode: opts.mode,
    workspaceTomlPath,
    stderr: () => stderrChunks.join(''),
    dispose: async () => {
      if (disposed) return;
      disposed = true;
      await killGracefully(child);
      rmSync(baseDir, { recursive: true, force: true });
    },
  };
}

// parseAuthTokenFromStderr greps the stderr buffer for the
// "Token:  <plaintext>" line that ensurePrimaryToken writes (see
// internal/cli/serve.go). Returns the token, or undefined if the
// banner wasn't printed (rare — only happens when a hash file
// already exists at startup, which doesn't apply in our tempdir).
function parseAuthTokenFromStderr(buf: string): string | undefined {
  const m = buf.match(/^\s*Token:\s+(\S+)\s*$/m);
  return m ? m[1] : undefined;
}

// waitForExit resolves when the child exits, or rejects if the
// timeout fires first. Used for expected-boot-failure spawns where
// we WANT the exit and would rather see a clean error than hang.
function waitForExit(
  child: ChildProcess,
  timeoutMs: number
): Promise<{ code: number | null; signal: NodeJS.Signals | null }> {
  if (child.exitCode !== null) {
    return Promise.resolve({ code: child.exitCode, signal: child.signalCode });
  }
  return new Promise((resolveFn, rejectFn) => {
    const timer = setTimeout(() => {
      try { child.kill('SIGKILL'); } catch { /* gone */ }
      rejectFn(new Error(`process did not exit within ${timeoutMs}ms`));
    }, timeoutMs);
    child.once('exit', (code, signal) => {
      clearTimeout(timer);
      resolveFn({ code, signal });
    });
  });
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
      // ANY HTTP response — including 401 in --auth=token mode — proves
      // the listener is bound and the auth middleware is engaged. Only
      // network-level errors (ECONNREFUSED) mean we're still booting.
      // Drain the body so the connection is released before the next poll.
      await res.body?.cancel().catch(() => {});
      return;
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
