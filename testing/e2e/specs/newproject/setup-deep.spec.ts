// specs/newproject/setup-deep.spec.ts
//
// Deep coverage for the deterministic setup gate that runs before the
// Onboarder LLM starts. This deliberately exercises
// /api/v1/onboarding/setup directly instead of driving the whole
// conversational route: real /newproject/start may require a configured
// LLM, while setup must remain deterministic and testable without one.

import { execFileSync } from 'node:child_process';
import { existsSync, mkdtempSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { test, expect } from '../../fixtures/server';

test.describe('@deep /onboard deterministic setup', () => {
  test.skip(({ backend }) => backend !== 'real', 'deep-only — needs a real gemba serve');

  test('adopts an existing native worktree and seeds runtime guidance before LLM launch @deep', async ({
    serverInfo,
  }) => {
    const worktree = mkdtempSync(join(tmpdir(), 'gemba-onboard-e2e-'));
    try {
      try {
        execFileSync('git', ['init', '--initial-branch=main'], {
          cwd: worktree,
          stdio: 'ignore',
        });
      } catch {
        execFileSync('git', ['init'], { cwd: worktree, stdio: 'ignore' });
        execFileSync('git', ['symbolic-ref', 'HEAD', 'refs/heads/main'], {
          cwd: worktree,
          stdio: 'ignore',
        });
      }

      const res = await fetch(`${serverInfo.baseURL}/api/v1/onboarding/setup`, {
        method: 'POST',
        headers: {
          'content-type': 'application/json',
          'x-gemba-confirm': `setup-${Date.now()}`,
        },
        body: JSON.stringify({
          origin: 'existing',
          project_name: 'onboard-e2e',
          github_project: 'GembaCore/onboard-e2e',
          orchestration: 'native',
          worktree_path: worktree,
          source_analysis_tool: 'none',
        }),
      });

      expect(res.status).toBe(200);
      const body = (await res.json()) as {
        project_path?: string;
        frames?: Array<{ line?: string; done?: boolean }>;
        checks?: Record<string, string>;
      };
      expect(body.project_path).toBe(worktree);
      expect(body.frames?.some((frame) => frame.line?.includes('Setup complete'))).toBe(true);
      expect(body.checks?.source_analysis).toBe('skipped');
      expect(existsSync(join(worktree, '.gemba', 'workspace.toml'))).toBe(true);
      expect(existsSync(join(worktree, 'AGENTS.md'))).toBe(true);
      expect(readFileSync(join(worktree, 'AGENTS.md'), 'utf8')).toContain('Beads');
      expect(existsSync(join(worktree, 'CLAUDE.md'))).toBe(true);
      expect(existsSync(join(worktree, '.Codex', 'settings.local.json'))).toBe(true);
    } finally {
      rmSync(worktree, { recursive: true, force: true });
    }
  });
});
