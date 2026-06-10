// specs/drawers/new-session-dialog.spec.ts — gm-5v8v.8 (in progress)
//
// NewSessionDialog. Opens via the Dispatch button on EpicDetail or
// WorkItemDrawer (gm-native.21) or the standalone /sessions header.
// The /sessions route currently surfaces an unrelated render-loop
// (gm-dt9y) — until that's fixed, drive the dialog through the
// EpicDetail's Dispatch button instead.
//
// Submission verification (POST /api/sessions + tmux pane + worktree
// materialised) is tagged @deep — gm-5v8v.2.

import { test, expect } from '../../fixtures/server';
import { EpicDetailPO } from '../../pages/EpicDetail';
import * as build from '../../builders/workitem';
import { agent, resetAgentIds } from '../../builders/agent';
import { orchestrationManifest } from '../../fixtures/capabilitiesPlane';

test.describe('NewSessionDialog @route', () => {
  test.beforeEach(() => resetAgentIds());

  test('Dispatch button is disabled until an OrchestrationPlane is bound', async ({
    page,
    workPlane,
  }) => {
    // No capabilitiesPlane.set(...) — the envelope stays
    // {orchestration_plane: null}. EpicDetail reads
    // useCapabilities().orchestrationPlane and disables Dispatch
    // when it's missing.
    const epicId = 'gm-disp-no-plane';
    workPlane.seed([
      build.epic({
        id: epicId,
        state_category: 'staged',
        derived: { agent_claimable: true, human_action_required: false, review_pending: false },
      }),
    ]);

    const detail = new EpicDetailPO(page);
    await detail.openByDeepLink(epicId);

    await expect(detail.dispatch).toHaveAttribute('data-disabled', 'true');
  });

  test('opens from EpicDetail Dispatch button when an OrchestrationPlane is bound', async ({
    page,
    workPlane,
    capabilitiesPlane,
  }) => {
    capabilitiesPlane.setOrchestrationPlane(orchestrationManifest());

    const epicId = 'gm-disp-with-plane';
    workPlane.seed([
      build.epic({
        id: epicId,
        state_category: 'staged',
        derived: { agent_claimable: true, human_action_required: false, review_pending: false },
      }),
    ]);

    const detail = new EpicDetailPO(page);
    await detail.openByDeepLink(epicId);
    await detail.dispatch.click();

    const dialog = page.getByRole('dialog', { name: 'New session' });
    await expect(dialog).toBeVisible();
    // The bead id is prefilled and locked into the dialog.
    await expect(dialog).toContainText(epicId);
  });

  test('agent picker derives tokens from /api/agents dialects', async ({
    page,
    workPlane,
    agentPlane,
    capabilitiesPlane,
  }) => {
    capabilitiesPlane.setOrchestrationPlane(orchestrationManifest());
    // Seed two agents with distinct dialects; the dialog's
    // agentTypeOptions de-dupes and sorts them.
    agentPlane.seed([
      agent({ id: 'a-1', dialect: 'claude' }),
      agent({ id: 'a-2', dialect: 'codex' }),
    ]);

    const epicId = 'gm-disp-pick';
    workPlane.seed([
      build.epic({
        id: epicId,
        state_category: 'staged',
        derived: { agent_claimable: true, human_action_required: false, review_pending: false },
      }),
    ]);

    const detail = new EpicDetailPO(page);
    await detail.openByDeepLink(epicId);
    await detail.dispatch.click();

    const dialog = page.getByRole('dialog', { name: 'New session' });
    await expect(dialog).toBeVisible();
    // Agent-type select: NewSessionDialog renders a <select> with one
    // <option> per derived dialect. Locate by role/name to stay
    // resilient to the absence of a per-control testid.
    const select = dialog.getByRole('combobox');
    await expect(select).toBeVisible();
    // Both dialects show up as options.
    await expect(dialog.locator('option[value="claude"]')).toHaveCount(1);
    await expect(dialog.locator('option[value="codex"]')).toHaveCount(1);
  });

  test.fixme('POST /api/sessions starts a real tmux pane + worktree @deep', async () => {
    // gm-5v8v.2.
  });
});
