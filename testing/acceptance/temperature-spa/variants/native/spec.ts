// gm-root.27.12 — native variant entry point.
//
// Configures `gemba serve --orchestration=native --pool-config <fixture>`
// against an ephemeral project, points it at the local pool fixture, and
// hands off to the shared acceptance body. Runs by default in the
// acceptance suite — the gastown variant (../gastown/spec.ts) is opt-in.
//
// Wave 3a, depends on:
//   - bootstrapProject (gm-root.27.3,  shared/helpers/bootstrap.ts)
//   - runAcceptance     (gm-root.27.6,  shared/spec.ts) — pending
//   - native pool fixture (gm-root.27.13, ./fixtures/pool.toml)
//   - cleanupNative     (gm-root.27.14, ./cleanup.ts)
//
// NOTE on pool-config delivery: bootstrapProject (gm-root.27.3) currently
// does not forward `--pool-config` to its gemba spawn. Until that
// extension lands (tracked as a follow-on), this spec writes the fixture
// into `<projectDir>/pool.toml` so any pool-config reader that defaults to
// the project root picks it up. The fixture path is also attached to the
// test report so a future flag-forwarding bootstrap can pass it directly.

import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';
import { test } from '@playwright/test';
import { bootstrapProject } from '../../shared/helpers/bootstrap';
import { runAcceptance } from '../../shared/spec';
import { injectEscalation } from '../../shared/helpers/escalation';
import type { EscalationInjector } from '../../shared/contracts';
import { DEMO_MODE } from '../../shared/helpers/demo-mode';
import { Narrator } from '../../shared/narrator';

const here = dirname(fileURLToPath(import.meta.url));
const POOL_CONFIG = resolve(here, 'fixtures/pool.toml');
const POOL_CONFIG_CODEX = resolve(here, 'fixtures/pool.codex.toml');
const POOL_CONFIG_CLAUDE = resolve(here, 'fixtures/pool.claude.toml');

test.describe('temperature-spa @native', () => {
  test('builds the SPA end-to-end via beads (native orchestration)', async ({ page }, testInfo) => {
    const realAgents = process.env.GEMBA_ACCEPTANCE_REAL_AGENTS === '1';
    const realAgentType = (process.env.GEMBA_ACCEPTANCE_AGENT ?? 'codex').toLowerCase();
    // gm-root.28.8: CI default uses --orchestration=mock so the
    // dispatch daemon routes work to the in-process mock plane. When
    // GEMBA_ACCEPTANCE_REAL_AGENTS=1, use native orchestration plus a
    // selected real-agent pool fixture for a persuasive end-to-end run.
    const poolConfig = realAgents
      ? realAgentType === 'claude'
        ? POOL_CONFIG_CLAUDE
        : POOL_CONFIG_CODEX
      : POOL_CONFIG;
    const project = await bootstrapProject({
      workerIndex: testInfo.workerIndex,
      serveArgs: [
        `--orchestration=${realAgents ? 'native' : 'mock'}`,
        '--pool-config', poolConfig,
      ],
    });

    testInfo.attach('native-pool-config', { path: poolConfig, contentType: 'text/plain' });
    testInfo.annotations.push({
      type: 'pool-config',
      description: `--pool-config ${poolConfig} forwarded to gemba serve via bootstrap`,
    });
    testInfo.annotations.push({
      type: 'agent-mode',
      description: realAgents ? `native ${realAgentType} agent` : 'mock orchestration',
    });

    const escalationInjector: EscalationInjector = {
      async inject(spec) {
        const res = await injectEscalation(project.baseURL, {
          target: spec.beadID,
          kind: spec.kind,
          urgency: spec.urgency,
          summary: `Synthetic escalation for ${spec.beadID} (acceptance test)`,
        });
        if (!res.ok) {
          const detail = 'message' in res.err ? `: ${res.err.message}` : '';
          throw new Error(
            `injectEscalation failed (${res.err.kind}${detail}): see gm-root.27.22 backend follow-up`,
          );
        }
        return { escalationID: res.value.id };
      },
    };

    try {
      await runAcceptance({
        variant: 'native',
        page,
        baseURL: project.baseURL,
        projectDir: project.projectDir,
        beadPrefix: project.beadPrefix,
        escalationInjector,
        narrator: DEMO_MODE ? new Narrator() : undefined,
        narrationOutputPath: DEMO_MODE ? testInfo.outputPath('narration.json') : undefined,
        narrationVideoPath: DEMO_MODE ? testInfo.outputPath('temperature-spa-demo.mp4') : undefined,
      });
    } finally {
      await project.cleanup();
    }
  });
});
