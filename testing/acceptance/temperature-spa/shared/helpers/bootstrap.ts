// testing/acceptance/temperature-spa/shared/helpers/bootstrap.ts
//
// gm-root.27.3 — Ephemeral project bootstrap helper.
//
// Each acceptance test run gets:
//   1. A tempdir-isolated bd workspace (embedded Dolt; no shared :3307).
//   2. A free TCP port for `gemba serve`.
//   3. A real `gemba serve` process pointed at the tempdir.
//   4. A target project created via POST /api/v1/newproject/create.
//
// We DELIBERATELY use the embedded-Dolt-per-tempdir pattern from
// testing/e2e/fixtures/realServer.ts (gm-5v8v.2) instead of running
// our own ephemeral `dolt sql-server`. The shared :3307 server is
// fragile and the deep-mode E2E suite already paid for that lesson
// (see gm-h4n + gt CLAUDE.md). Embedded-per-tempdir keeps every
// acceptance run fully isolated on its own filesystem subtree.
//
// Per the acceptance design doc (D15, gm-1avi) §11.7:
//   "The deep-mode E2E gating issue (gm-h4n) — bd-init colliding on
//   the shared Dolt server — is solved by the ephemeral-Dolt helper.
//   Each acceptance run gets its own Dolt instance ..."
//
// We achieve that with embedded Dolt rather than a sql-server
// instance. The bead description called for "ephemeral Dolt server
// on a random port" — close-message will note the substitution.
//
// References:
//   - D15 docs/design/acceptance-temperature-spa.md §3, §11.7
//   - testing/e2e/fixtures/realServer.ts (existing pattern reused)
//   - internal/server/newproject.go (POST /api/v1/newproject/create)

import { execFileSync } from 'node:child_process';
import { existsSync, mkdirSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { spinRealServer, type RealServer } from '../../../../e2e/fixtures/realServer';

export type ProjectHandle = {
  /** http://127.0.0.1:<port> — pass to Playwright as baseURL. */
  baseURL: string;
  /** Tempdir gemba is pointed at — also the bd workspace root. */
  projectDir: string;
  /** Project name from /api/v1/newproject/create. */
  projectName: string;
  /**
   * The bd-issue prefix initialized in this workspace (e.g. 'e2e0').
   * Used by the JSONL pack loader to substitute {{PREFIX}} placeholders
   * before `bd import` (gm-root.27.4 / .29).
   */
  beadPrefix: string;
  /**
   * Tear everything down: kill gemba, rm tempdir, free ports.
   * Idempotent; safe to call from t.Cleanup. Survives partial-failure
   * states (e.g., gemba died mid-run).
   */
  cleanup(): Promise<void>;
  /**
   * Underlying real-server handle for callers that need to inspect
   * server-level state (auth token, beadsDir, etc.). Most acceptance
   * tests use {baseURL} and {projectDir} directly.
   */
  server: RealServer;
};

export type BootstrapOptions = {
  /**
   * Project name passed to /api/v1/newproject/create. Defaults to a
   * randomized identifier so concurrent runs don't collide on
   * names visible to the operator.
   */
  projectName?: string;
  /**
   * Project description. Defaults to a fixed string identifying the
   * acceptance harness.
   */
  description?: string;
  /**
   * Workspace mode for the bootstrapped project. Defaults to
   * 'unsupervised' (no nonce / no audit chain), matching the
   * acceptance test's hermetic runtime.
   */
  workspaceMode?: 'unsupervised' | 'supervised' | 'managed';
  /**
   * Playwright worker index for tempdir labeling. When called from a
   * Playwright fixture, pass `workerInfo.workerIndex`. Outside
   * Playwright, pass any stable integer; concurrent test runs should
   * use distinct values to keep tempdir paths disambiguated.
   */
  workerIndex?: number;
  /**
   * gm-root.27.21 — extra argv forwarded to `gemba serve`. Used by
   * the variant wrappers to pass `--orchestration=...` and
   * `--pool-config <path>` so the autodispatch daemon picks up the
   * test's pool configuration on first launch (no in-place restart
   * needed). Appended verbatim after the fixture's own flags.
   */
  serveArgs?: string[];
};

/**
 * bootstrapProject spins a fresh, isolated gemba server and creates a
 * single project inside it. The returned handle exposes the baseURL
 * Playwright should drive against and the projectDir on disk that
 * MockAgentRunner / target-build steps will operate against.
 *
 * Hermetic across concurrent invocations: each call gets its own
 * tempdir + free port + bd workspace + project. Tear down via
 * handle.cleanup() (or rely on the realServer fixture's dispose
 * if you embed bootstrapProject inside a Playwright fixture).
 */
export async function bootstrapProject(
  opts: BootstrapOptions = {}
): Promise<ProjectHandle> {
  const runId = randomRunId();
  const projectName = opts.projectName ?? `tspa-${runId}`;
  const description =
    opts.description ?? 'Acceptance test target — Celsius/Fahrenheit table SPA';
  const workspaceMode = opts.workspaceMode ?? 'unsupervised';
  // workerIndex is required by spinRealServer for tempdir labeling.
  // Outside Playwright, default to a process-pid-based fallback so
  // concurrent acceptance runs in CI don't collide on the same path.
  const workerIndex = opts.workerIndex ?? process.pid;

  // Spin gemba serve against an isolated bd workspace (embedded Dolt).
  // spinRealServer runs `bd init` in the tempdir before launching
  // gemba, so the resulting workspace is already a valid project — no
  // separate POST /api/v1/newproject/create call is needed (and would
  // collide with the prior bd init).
  //
  // Caveat: spinRealServer's bd init does NOT auto-seed personas /
  // agents.toml / CLAUDE.md the way `gemba newproject` does
  // (gm-root.24). Pool config + persona-routed dispatch may need to
  // seed those files separately. Tracked under the acceptance-suite
  // first-run cleanup arc.
  const previousDisableWatcher = process.env.GEMBA_DISABLE_BD_WATCHER;
  const previousEnableTestEscalations = process.env.GEMBA_ENABLE_TEST_ESCALATIONS;
  process.env.GEMBA_DISABLE_BD_WATCHER = '1';
  process.env.GEMBA_ENABLE_TEST_ESCALATIONS = '1';
  let server: RealServer;
  try {
    server = await spinRealServer({
      workerIndex,
      mode: workspaceMode,
      auth: 'open',
      serveArgs: opts.serveArgs,
      serveEnv: process.env.GEMBA_ACCEPTANCE_REAL_AGENTS === '1'
        ? {
          HOME: process.env.HOME,
          GEMBA_ACCEPTANCE_MERGE_BEAD_WORKTREES: '1',
        }
        : undefined,
      beforeServe: (workspaceDir) => {
        seedAcceptanceEnginerPersona(workspaceDir);
        if (process.env.GEMBA_ACCEPTANCE_REAL_AGENTS === '1') {
          seedCodexAgentRegistry(workspaceDir);
          seedAcceptanceGitRepo(workspaceDir);
        }
      },
    });
  } finally {
    if (previousDisableWatcher == null) {
      delete process.env.GEMBA_DISABLE_BD_WATCHER;
    } else {
      process.env.GEMBA_DISABLE_BD_WATCHER = previousDisableWatcher;
    }
    if (previousEnableTestEscalations == null) {
      delete process.env.GEMBA_ENABLE_TEST_ESCALATIONS;
    } else {
      process.env.GEMBA_ENABLE_TEST_ESCALATIONS = previousEnableTestEscalations;
    }
  }
  // projectName is preserved on the handle for reporting even though
  // we didn't formally create a "project" via the API — the bd
  // workspace IS the project from the acceptance test's perspective.
  void runId;

  return {
    baseURL: server.baseURL,
    projectDir: server.beadsDir,
    projectName,
    // spinRealServer initializes bd with prefix `e2e${workerIndex}`.
    beadPrefix: `e2e${workerIndex}`,
    cleanup: async () => {
      // dispose() in realServer kills the gemba child, removes the
      // worktrees parent, and rms the tempdir. Idempotent.
      await server.dispose();
    },
    server,
  };
}

/**
 * Drop a minimal acceptance-engineer.toml under <projectDir>/.gemba/
 * personas/. Matches the persona-shape contract (gm-57b) tightly
 * enough for pool config + dispatch to route work; the actual agent
 * runtime is MockAgentRunner so heavy persona fields (volunteer_mode,
 * skills, etc.) are kept minimal.
 *
 * Spec mirrors target-jsonl/decisions.jsonl.tmpl tspa-d2.
 */
function seedAcceptanceEnginerPersona(projectDir: string): void {
  const dir = join(projectDir, '.gemba', 'personas');
  mkdirSync(dir, { recursive: true });
  const toml = `# Acceptance test persona — written by gm-root.27.34 bootstrap helper.
# Minimum schema for pool config + dispatch routing; actual agent
# runtime is either mock orchestration or the native Codex driver.

id = "acceptance-engineer"
name = "Acceptance Engineer"
role = "Engineer"
variety = "coach"
description = "Mocked engineer persona used by the gm-root.27 acceptance harness. Builds the temperature-spa target through the M1/M2/M3 milestones."
icon = "🧪"
`;
  writeFileSync(join(dir, 'acceptance-engineer.toml'), toml, 'utf8');
}

function seedCodexAgentRegistry(projectDir: string): void {
  const dir = join(projectDir, '.gemba');
  mkdirSync(dir, { recursive: true });
  const toml = `# Acceptance test agent registry — written before gemba serve boots.

[[agent]]
name             = "codex"
binary           = "gemba-codex-driver"
args             = ["--sandbox", "workspace-write", "--ask-for-approval", "never"]
model            = "gpt-5.4-mini"
preamble         = "codex_exec"
hooks            = "none"
interaction_mode = "balanced"
intra_parallel   = true
max_parallel     = 2

[[agent]]
name             = "claude"
binary           = "claude"
args             = ["--permission-mode", "bypassPermissions"]
model            = "claude-opus-4-7"
preamble         = "claude_md"
hooks            = "claude_code"
intra_parallel   = true
max_parallel     = 2
`;
  writeFileSync(join(dir, 'agents.toml'), toml, 'utf8');
}

function seedAcceptanceGitRepo(projectDir: string): void {
  if (existsSync(join(projectDir, '.git'))) {
    return;
  }
  const gitignore = [
    '.beads/',
    '.dolt/',
    '.claude/',
    'CLAUDE.md',
    'worktrees/',
    'node_modules/',
    'dist/',
    'test-results/',
    'm*.jsonl',
    '.gemba/session-prompts/',
    '',
  ].join('\n');
  writeFileSync(join(projectDir, '.gitignore'), gitignore, 'utf8');

  runGit(projectDir, 'init', '-b', 'main');
  runGit(projectDir, 'config', 'user.name', 'Gemba Acceptance');
  runGit(projectDir, 'config', 'user.email', 'acceptance@gemba.local');
  runGit(projectDir, 'add', '.gitignore', '.gemba');
  runGit(projectDir, 'commit', '-m', 'seed acceptance workspace');
}

function runGit(projectDir: string, ...args: string[]): void {
  execFileSync('git', args, {
    cwd: projectDir,
    stdio: ['ignore', 'ignore', 'pipe'],
  });
}

/**
 * Eight-character random alphanumeric run id. Visible in project
 * names + tempdir paths so concurrent runs are easy to disambiguate
 * in logs / orphan cleanup.
 */
function randomRunId(): string {
  const alphabet = 'abcdefghijklmnopqrstuvwxyz0123456789';
  let id = '';
  for (let i = 0; i < 8; i += 1) {
    id += alphabet[Math.floor(Math.random() * alphabet.length)];
  }
  return id;
}
