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

import { copyFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, resolve, join } from 'node:path';
import { test } from '@playwright/test';
import { bootstrapProject } from '../../shared/helpers/bootstrap';
import { runAcceptance } from '../../shared/spec';
import { makeAgentRunnerFactory } from '../../shared/runner/factory';
import { injectEscalation } from '../../shared/helpers/escalation';
import type { EscalationInjector } from '../../shared/contracts';

const here = dirname(fileURLToPath(import.meta.url));
const POOL_CONFIG = resolve(here, 'fixtures/pool.toml');

test.describe('temperature-spa @native', () => {
  test('builds the SPA end-to-end via beads (native orchestration)', async ({ page }, testInfo) => {
    const project = await bootstrapProject({ workerIndex: testInfo.workerIndex });

    testInfo.attach('native-pool-config', { path: POOL_CONFIG, contentType: 'text/plain' });

    const projectPoolPath = join(project.projectDir, 'pool.toml');
    copyFileSync(POOL_CONFIG, projectPoolPath);
    testInfo.annotations.push({
      type: 'pool-config',
      description: `wrote ${POOL_CONFIG} to ${projectPoolPath}`,
    });

    const agentFactory = makeAgentRunnerFactory({
      baseURL: project.baseURL,
      projectDir: project.projectDir,
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
          throw new Error(
            `injectEscalation failed (${res.err.kind}): see gm-root.27.22 backend follow-up`,
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
        agentFactory,
        escalationInjector,
      });
    } finally {
      await project.cleanup();
    }
  });
});
