// fixtures/bdClient.ts
//
// gm-5v8v.2 — minimal bd CLI wrapper for deep-mode specs.
//
// The bd binary is the canonical write surface for the beads workspace.
// gemba serve reads/writes through bd as well, so seeding via bd is
// equivalent to a real operator's bd command — no test-only API path.
//
// Methods stay thin: one per CLI verb the smoke tier needs. Builders
// (gm-5v8v.5/.6/.8) layer richer factory shapes on top of this.

import { execFile } from 'node:child_process';
import { promisify } from 'node:util';

const execFileP = promisify(execFile);
const LOCK_RETRY_DELAYS_MS = [250, 500, 1_000, 2_000, 4_000, 8_000];

export type BdCreateOptions = {
  title: string;
  type?: 'task' | 'epic' | 'bug' | 'feature' | 'chore' | 'decision';
  priority?: 0 | 1 | 2 | 3 | 4;
  description?: string;
  labels?: string[];
};

export type BdWorkItem = {
  id: string;
  title: string;
  status: string;
  state_category?: string;
  priority?: number;
  type?: string;
};

export class BdClient {
  private readonly cwd: string;
  private readonly bin: string;
  private readonly env: NodeJS.ProcessEnv;

  constructor(opts: {
    beadsDir: string;
    bdBin?: string;
    /**
     * Env to pass to every bd subprocess. MUST be the env returned by
     * spinRealServer (carries HOME=<tempdir> isolation per gm-h4n).
     * Defaults to process.env for tests that don't have a real server
     * yet — those tests are responsible for their own isolation.
     */
    env?: NodeJS.ProcessEnv;
  }) {
    this.cwd = opts.beadsDir;
    this.bin = opts.bdBin ?? 'bd';
    this.env = opts.env ?? process.env;
  }

  /** Create a bead. Returns the new id. */
  async create(opts: BdCreateOptions): Promise<{ id: string }> {
    const args = ['create', opts.title, '--silent'];
    if (opts.type) args.push('--type', opts.type);
    if (opts.priority !== undefined) args.push('--priority', String(opts.priority));
    if (opts.description) args.push('--description', opts.description);
    if (opts.labels && opts.labels.length) args.push('--labels', opts.labels.join(','));
    const { stdout } = await this.run(args);
    const id = stdout.trim();
    if (!id) throw new Error(`bd create did not return an id (stdout=${stdout})`);
    return { id };
  }

  /** List beads as parsed JSON. Empty list when none exist. */
  async list(extra: string[] = []): Promise<BdWorkItem[]> {
    const { stdout } = await this.run(['list', '--json', ...extra]);
    const parsed = stdout.trim() ? JSON.parse(stdout) : [];
    return Array.isArray(parsed) ? parsed : (parsed.items ?? []);
  }

  /** Close a bead (state → completed). */
  async close(id: string): Promise<void> {
    await this.run(['close', id]);
  }

  /** Delete every open + closed bead. Used between specs to reset state. */
  async resetAll(): Promise<void> {
    const items = await this.list(['--all']);
    for (const item of items) {
      // bd has no `delete`; the cheapest reset is closing every bead.
      // For a hard wipe between tests, future work can drop the
      // workspace and re-init. Smoke tier doesn't need that yet.
      try { await this.close(item.id); } catch { /* idempotent */ }
    }
  }

  private async run(args: string[]) {
    let lastErr: unknown;
    for (let attempt = 0; attempt <= LOCK_RETRY_DELAYS_MS.length; attempt += 1) {
      try {
        return await execFileP(this.bin, args, {
          cwd: this.cwd,
          env: this.env,
          maxBuffer: 16 * 1024 * 1024,
        });
      } catch (err) {
        lastErr = err;
        if (!isEmbeddedDoltLockError(err) || attempt === LOCK_RETRY_DELAYS_MS.length) {
          throw err;
        }
        const retryDelayMs = LOCK_RETRY_DELAYS_MS[attempt];
        if (retryDelayMs === undefined) throw err;
        await delay(retryDelayMs);
      }
    }
    throw lastErr;
  }
}

function isEmbeddedDoltLockError(err: unknown): boolean {
  const combined = `${errorField(err, 'message')}\n${errorField(err, 'stderr')}`;
  return (
    combined.includes('another process holds the exclusive lock') ||
    combined.includes('embedded backend supports only one writer at a time')
  );
}

function errorField(err: unknown, field: 'message' | 'stderr'): string {
  if (!err || typeof err !== 'object' || !(field in err)) return '';
  const value = (err as Record<string, unknown>)[field];
  return typeof value === 'string' ? value : '';
}

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
