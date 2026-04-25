// specs/drawers/new-session-dialog.spec.ts — gm-5v8v.8 (in progress)
//
// NewSessionDialog. Opens via the Dispatch button on EpicDrawer or
// WorkItemDrawer (gm-native.21) or the standalone /sessions header.
// The /sessions route currently surfaces an unrelated render-loop
// (gm-dt9y) — until that's fixed, drive the dialog through the
// EpicDrawer's Dispatch button instead.
//
// Submission verification (POST /api/sessions + tmux pane + worktree
// materialised) is tagged @deep — gm-5v8v.2.

import { test, expect } from '../../fixtures/server';
import { EpicDrawerPO } from '../../pages/EpicDrawer';
import * as build from '../../builders/workitem';

test.describe('NewSessionDialog @route', () => {
  test.fixme(
    'opens from EpicDrawer Dispatch button when an OrchestrationPlane is bound',
    async ({ page, workPlane }) => {
      // Requires the fake backend to surface an OrchestrationPlane
      // capability so the Dispatch button isn't disabled. The
      // /api/capabilities handler currently returns {} — extending
      // it with a fake orchestration-plane shape lands as a follow-up.
      const epicId = 'gm-test-epic-dispatch';
      workPlane.seed([
        build.epic({
          id: epicId,
          state_category: 'staged',
          derived: { agent_claimable: true, human_action_required: false, review_pending: false },
        }),
      ]);

      const drawer = new EpicDrawerPO(page);
      await drawer.openByDeepLink(epicId);
      await drawer.dispatch.click();

      const dialog = page.getByRole('dialog', { name: 'New session' });
      await expect(dialog).toBeVisible();
    }
  );

  test.fixme('agent picker derives tokens from /api/agents', async () => {
    // Seed a couple of agents with dialect strings via the fake
    // /api/agents handler; assert the select has those options.
  });

  test.fixme('POST /api/sessions starts a real tmux pane + worktree @deep', async () => {
    // gm-5v8v.2.
  });
});
