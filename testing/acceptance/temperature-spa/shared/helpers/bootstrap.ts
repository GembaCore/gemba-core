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

import { spinRealServer, type RealServer } from '../../../../e2e/fixtures/realServer';

export type ProjectHandle = {
  /** http://127.0.0.1:<port> — pass to Playwright as baseURL. */
  baseURL: string;
  /** Tempdir gemba is pointed at — also the bd workspace root. */
  projectDir: string;
  /** Project name from /api/v1/newproject/create. */
  projectName: string;
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
  const server = await spinRealServer({
    workerIndex,
    mode: workspaceMode,
    auth: 'open',
    serveArgs: opts.serveArgs,
  });

  // Create the project. POST /api/v1/newproject/create runs the same
  // atomic ratify the conversational flow uses, just with an empty
  // milestone tree (we'll populate via JSONL import from the M1/M2/M3
  // pack — see gm-root.27.4).
  const createUrl = `${server.baseURL}/api/v1/newproject/create`;
  const res = await fetch(createUrl, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      project_name: projectName,
      description,
    }),
  });
  if (!res.ok) {
    // Clean up before surfacing the failure so we don't leak resources
    // when the API rejects the create.
    await server.dispose();
    const text = await res.text().catch(() => '<unreadable>');
    throw new Error(
      `bootstrapProject: POST /api/v1/newproject/create failed (${res.status}): ${text}`
    );
  }
  // Body shape matches newProjectCreate's response — at minimum the
  // project path is returned. We don't depend on the exact shape; we
  // rely on server.beadsDir which is the workspace root.
  await res.json().catch(() => undefined);

  return {
    baseURL: server.baseURL,
    projectDir: server.beadsDir,
    projectName,
    cleanup: async () => {
      // dispose() in realServer kills the gemba child, removes the
      // worktrees parent, and rms the tempdir. Idempotent.
      await server.dispose();
    },
    server,
  };
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
