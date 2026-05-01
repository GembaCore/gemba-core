// gm-root.27.15 — gastown variant entry point.
//
// Drives the SPA's /settings/pools "+ New rig" / "+ New polecat" buttons
// (RunCommandModal, gm-s47n.17) to create a per-run rig + polecat through
// the UI, then drives the gemba server with that pool. Opt-in: only runs
// when GEMBA_ACCEPTANCE_RUN_GASTOWN=1 — gastown orchestration shells to
// `gt`, which is a developer-machine-only assumption.
//
// Wave 3b, depends on:
//   - bootstrapProject  (gm-root.27.3,  shared/helpers/bootstrap.ts)
//   - runAcceptance     (gm-root.27.6,  shared/spec.ts) — pending
//   - configurePool     (gm-root.27.7,  shared/configurePool.ts) — pending
//   - capability probe  (gm-e7.12)
//   - RunCommandModal   (gm-s47n.17)
//   - pool template     (gm-root.27.16, ./pool-template.ts)
//   - cleanupGastown    (gm-root.27.17, ./cleanup.ts)
//
// NOTE on orchestration delivery: bootstrapProject (gm-root.27.3) currently
// hard-codes --orchestration=native via spinRealServer. Until that
// extension lands (tracked as a follow-on), the gastown variant cannot
// actually swap orchestration mode at runtime. The flow is structurally
// in place: rig+polecat creation, pool config save through the SPA, the
// round-trip render comparison from gm-root.27.16, and the cleanup chain.

import { writeFileSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { test, expect, type Page } from '@playwright/test';
import { bootstrapProject } from '../../shared/helpers/bootstrap';
import { runAcceptance } from '../../shared/spec';
import { configurePool } from '../../shared/pool-config';
import { renderGastownPoolToml } from './pool-template';
import { cleanupGastown, type GastownHandle } from './cleanup';
import { injectEscalation } from '../../shared/helpers/escalation';
import type { EscalationInjector } from '../../shared/contracts';

const RUN_GASTOWN = process.env.GEMBA_ACCEPTANCE_RUN_GASTOWN === '1';

test.describe('temperature-spa @gastown', () => {
  test.skip(!RUN_GASTOWN, 'set GEMBA_ACCEPTANCE_RUN_GASTOWN=1 to enable the gastown variant');

  test('builds the SPA end-to-end via beads (gastown orchestration)', async ({ page }, testInfo) => {
    const runId = (testInfo.testId || `r${Date.now()}`).replace(/[^a-z0-9]/gi, '').slice(0, 8);
    const rigName = `acceptance-${runId}`;
    const polecatName = `acceptance-pc-${runId}`;

    // gm-root.27.21 — orchestration mode passed at boot. Pool config
    // for gastown is still saved through the UI (rig name is dynamic
    // per-run; not knowable until after the gt rig create UI step).
    const project = await bootstrapProject({
      workerIndex: testInfo.workerIndex,
      serveArgs: ['--orchestration=gastown'],
    });

    const handle: GastownHandle = {
      project,
      rigName,
      polecatName,
      page,
      log: (m: string) => testInfo.annotations.push({ type: 'cleanup', description: m }),
    };

    try {
      await page.goto(`${project.baseURL}/settings/pools`);

      // gm-e7.12 — fail fast if the gt binary the operator has installed
      // is older than the capability the UI flow requires.
      await expect(
        page.getByTestId('gt-capability-probe-ok'),
        'gt capability probe must pass — install the matching gt build',
      ).toBeVisible({ timeout: 10_000 });

      await createRigViaModal(page, rigName);
      await page.reload();
      await createPolecatViaModal(page, polecatName, rigName);

      await configurePool(page, {
        variant: 'gastown',
        scope: rigName,
        persona: 'acceptance-engineer',
        size: 1,
        floor: 0.5,
      });

      // Round-trip check (gm-root.27.16 DoD): render the template for the
      // same rig and assert the SPA-saved file is byte-identical.
      const expected = renderGastownPoolToml(rigName);
      const writtenPath = join(project.projectDir, 'pool.toml');
      const written = readFileSync(writtenPath, 'utf8');
      expect(written, 'configurePool output must match the gastown template').toBe(expected);
      writeFileSync(`${writtenPath}.expected`, expected);
      testInfo.attach('gastown-pool-expected', {
        path: `${writtenPath}.expected`,
        contentType: 'text/plain',
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

      await runAcceptance({
        variant: 'gastown',
        page,
        baseURL: project.baseURL,
        projectDir: project.projectDir,
        beadPrefix: project.beadPrefix,
        rigName,
        escalationInjector,
      });
    } finally {
      await cleanupGastown(handle);
    }
  });
});

async function createRigViaModal(page: Page, rigName: string): Promise<void> {
  await page.getByRole('button', { name: /\+ New rig/i }).click();
  await page.getByLabel(/rig name/i).fill(rigName);
  await page.getByRole('button', { name: /^run$/i }).click();
  await expect(page.getByText(new RegExp(`rig ${rigName} created`, 'i'))).toBeVisible({
    timeout: 30_000,
  });
}

async function createPolecatViaModal(
  page: Page,
  polecatName: string,
  rigName: string,
): Promise<void> {
  await page.getByRole('button', { name: /\+ New polecat/i }).click();
  await page.getByLabel(/polecat name/i).fill(polecatName);
  const rigSelect = page.getByLabel(/rig/i);
  if ((await rigSelect.count()) > 0) await rigSelect.first().fill(rigName);
  await page.getByRole('button', { name: /^run$/i }).click();
  await expect(page.getByText(new RegExp(`polecat ${polecatName} created`, 'i'))).toBeVisible({
    timeout: 30_000,
  });
}
