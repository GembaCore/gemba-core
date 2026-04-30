// testing/acceptance/temperature-spa/shared/runner/mock.ts
//
// gm-root.27.2 — MockAgentRunner.
//
// Implements AgentRunner (per shared/contracts.ts). For each bead it
// claims, the mock:
//   1. Fetches the bead from the gemba server.
//   2. Parses the leading frontmatter block (template:, testid:, files:).
//   3. Dispatches to the matching template handler (templates.ts).
//   4. Closes the bead via `bd close <id>`.
//   5. Emits `gemba-state bead-done --bead <id>` so the autodispatch
//      daemon transitions the runner's session to SessionReady (per
//      gm-s47n.11 + gm-s47n.14 native idle lifecycle).
//
// Determinism: runner sleeps are bounded (max 200ms per template),
// no random timing, no LLM calls. Suitable for CI default per D16.
//
// References:
//   - D16 docs/design/acceptance-temperature-spa.md §4 (architecture)
//   - shared/contracts.ts (AgentRunner interface)
//   - templates.ts (handler implementations)
//   - frontmatter.ts (description parser)

import { spawnSync } from 'node:child_process';
import type { AgentRunner } from '../contracts';
import { parseFrontmatter } from './frontmatter';
import { getTemplate, type TemplateContext } from './templates';

export type MockRunnerEnv = {
  /** http://127.0.0.1:<port> — the bootstrapped gemba server. */
  baseURL: string;
  /** Absolute path to the target project directory. */
  projectDir: string;
  /** Override the bd binary. Defaults to 'bd' on PATH. */
  bdBin?: string;
  /** Override the gemba-state binary. Defaults to 'gemba-state' on PATH. */
  gembaStateBin?: string;
  /** Optional progress logger; threaded into TemplateContext.log. */
  log?: (msg: string) => void;
};

export class MockAgentRunner implements AgentRunner {
  private closed = false;
  constructor(private readonly env: MockRunnerEnv) {}

  async run(beadID: string): Promise<void> {
    if (this.closed) {
      throw new Error('MockAgentRunner.run: runner is closed');
    }
    this.env.log?.(`mock: claim ${beadID}`);

    const bead = await this.fetchBead(beadID);
    const fm = parseFrontmatter(bead.description);
    const handler = getTemplate(fm.template);
    if (!handler) {
      // Fallback: keyword match on title (per D16 §4.2 fallback).
      const fromTitle = matchTemplateFromTitle(bead.title);
      if (!fromTitle) {
        // No template found — escalate per the bead's DoD. We file
        // a synthetic escalation (or for now, throw — the harness's
        // error-then-recover logic surfaces this).
        throw new Error(
          `MockAgentRunner: no template matches bead ${beadID} ` +
            `(frontmatter template='${fm.template ?? '(none)'}', ` +
            `title='${bead.title}')`
        );
      }
      this.env.log?.(`mock: title-match template=${fromTitle} for ${beadID}`);
      const tplFromTitle = getTemplate(fromTitle);
      if (!tplFromTitle) {
        throw new Error(
          `MockAgentRunner: title-match returned non-template '${fromTitle}'`
        );
      }
      await this.runHandler(tplFromTitle, beadID, bead.labels ?? [], fm);
    } else {
      await this.runHandler(handler, beadID, bead.labels ?? [], fm);
    }

    // Close the bead.
    await this.closeBead(beadID);
    // Emit bead-done so the pool-side lifecycle transitions to
    // SessionReady (gm-s47n.11). Best-effort: a missing
    // gemba-state binary doesn't fail the run — the warning tells
    // operators to install it.
    this.emitBeadDone(beadID);
    this.env.log?.(`mock: closed ${beadID}`);
  }

  async recycle(): Promise<void> {
    // Nothing to recycle — the mock has no warm session state.
    this.env.log?.('mock: recycle (no-op)');
  }

  async close(): Promise<void> {
    this.closed = true;
    this.env.log?.('mock: close');
  }

  // ── Internals ─────────────────────────────────────────────────────

  private async runHandler(
    handler: ReturnType<typeof getTemplate> & object,
    beadID: string,
    labels: string[],
    fm: ReturnType<typeof parseFrontmatter>
  ): Promise<void> {
    const ctx: TemplateContext = {
      projectDir: this.env.projectDir,
      beadID,
      labels,
      log: this.env.log,
    };
    await (handler as NonNullable<ReturnType<typeof getTemplate>>)(ctx, fm);
  }

  private async fetchBead(beadID: string): Promise<{
    id: string;
    title: string;
    description?: string;
    labels?: string[];
  }> {
    const url = `${this.env.baseURL}/api/work-items/${encodeURIComponent(beadID)}`;
    const res = await fetch(url);
    if (!res.ok) {
      throw new Error(
        `MockAgentRunner.fetchBead ${beadID}: ${res.status} ${await res
          .text()
          .catch(() => '')}`
      );
    }
    return (await res.json()) as {
      id: string;
      title: string;
      description?: string;
      labels?: string[];
    };
  }

  private async closeBead(beadID: string): Promise<void> {
    const bd = this.env.bdBin ?? 'bd';
    const res = spawnSync(
      bd,
      ['close', beadID, '-m', 'Closed by MockAgentRunner (gm-root.27.2)'],
      { cwd: this.env.projectDir, stdio: 'pipe' }
    );
    if (res.status !== 0) {
      throw new Error(
        `MockAgentRunner.closeBead ${beadID}: bd exited ${res.status}\n` +
          (res.stderr?.toString() ?? '')
      );
    }
  }

  private emitBeadDone(beadID: string): void {
    const bin = this.env.gembaStateBin ?? 'gemba-state';
    const res = spawnSync(bin, ['bead-done', '--bead', beadID], {
      cwd: this.env.projectDir,
      stdio: 'pipe',
    });
    if (res.status !== 0) {
      // Non-fatal: the test still functions if gemba-state is
      // missing (the daemon just doesn't observe SessionReady from
      // this runner). We surface it via the log.
      this.env.log?.(
        `mock: gemba-state bead-done failed (status=${res.status}); ` +
          'pool reuse may not engage. Ensure gemba-state is on PATH.'
      );
    }
  }
}

/**
 * Best-effort title → template fallback. Used when the bead
 * description doesn't carry a `template:` frontmatter entry. Order
 * matters: more-specific keywords win.
 */
function matchTemplateFromTitle(title: string): string | undefined {
  const t = title.toLowerCase();
  if (/init repo|git init|package\.json|vite\.config/.test(t)) return 'init-repo';
  if (/npm install|node_modules/.test(t)) return 'npm-install';
  if (/test\.tsx|vitest|spec/.test(t)) return 'write-test';
  if (/\.tsx|\.ts|component|temperaturerows|temperature.*table/.test(t))
    return 'write-component';
  if (/\bbuild\b|vite build|rebuild/.test(t)) return 'build';
  if (/\bserve\b|preview/.test(t)) return 'serve';
  if (/tsconfig|index\.html|main\.tsx/.test(t)) return 'noop';
  return undefined;
}
