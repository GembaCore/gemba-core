// specs/drawers/epic-drawer.spec.ts — gm-5v8v.8
//
// Covers EpicDrawer header (id / copy / close), action buttons
// (Stage / Start / Dispatch / New child), state section, children
// section bucketed by state. Mutations + inline state-change on
// member rows are tagged @deep — they need real backend writes.

import { test, expect } from '../../fixtures/server';
import { EpicDrawerPO } from '../../pages/EpicDrawer';
import * as build from '../../builders/workitem';

test.describe('EpicDrawer @route', () => {
  test('opens via /board/:epicId and renders header + state', async ({ page, workPlane }) => {
    const epicId = 'gm-test-epic';
    workPlane.seed([
      build.epic({ id: epicId, title: 'Test epic', state_category: 'staged', status: 'staged' }),
    ]);

    const drawer = new EpicDrawerPO(page);
    await drawer.openByDeepLink(epicId);

    await drawer.expectOpenWith(epicId);
    // The state pill renders status + state_category.
    await expect(drawer.stateSection).toContainText('staged');
  });

  test('renders the four action buttons', async ({ page, workPlane }) => {
    const epicId = 'gm-test-epic-actions';
    workPlane.seed([
      build.epic({
        id: epicId,
        title: 'Actions epic',
        state_category: 'staged',
        derived: { agent_claimable: true, human_action_required: false, review_pending: false },
      }),
    ]);

    const drawer = new EpicDrawerPO(page);
    await drawer.openByDeepLink(epicId);

    // The four buttons described by ui-spec §5.6 — render and have
    // testids regardless of enabled-ness.
    await expect(drawer.stage).toBeVisible();
    await expect(drawer.start).toBeVisible();
    await expect(drawer.dispatch).toBeVisible();
    await expect(drawer.newChild).toBeVisible();
  });

  test('Stage is disabled when not agent_claimable', async ({ page, workPlane }) => {
    const epicId = 'gm-test-epic-blocked';
    workPlane.seed([
      build.epic({
        id: epicId,
        derived: { agent_claimable: false, human_action_required: true, review_pending: false },
      }),
    ]);

    const drawer = new EpicDrawerPO(page);
    await drawer.openByDeepLink(epicId);

    await expect(drawer.stage).toHaveAttribute('data-disabled', 'true');
    await expect(drawer.start).toHaveAttribute('data-disabled', 'true');
  });

  test('renders children bucketed by state when present', async ({ page, workPlane }) => {
    const epicId = 'gm-test-epic-with-children';
    // epicChildren() reads the parent_child edge from each child's
    // relationships array — the edge lives on the child, not the parent.
    const childA = build.workItem({
      id: 'gm-c-1',
      title: 'Done child',
      state_category: 'completed',
      status: 'closed',
      relationships: [build.parentChild(epicId, 'gm-c-1')],
    });
    const childB = build.workItem({
      id: 'gm-c-2',
      title: 'WIP child',
      state_category: 'started',
      status: 'in_progress',
      relationships: [build.parentChild(epicId, 'gm-c-2')],
    });
    workPlane.seed([build.epic({ id: epicId }), childA, childB]);

    const drawer = new EpicDrawerPO(page);
    await drawer.openByDeepLink(epicId);

    await expect(drawer.childrenSection).toContainText('Children (2)');
    await expect(page.getByTestId('epic-children-completed')).toBeVisible();
    await expect(page.getByTestId('epic-children-started')).toBeVisible();
  });

  test('closes via close button', async ({ page, workPlane }) => {
    const epicId = 'gm-test-epic-close-x';
    workPlane.seed([build.epic({ id: epicId })]);

    const drawer = new EpicDrawerPO(page);
    await drawer.openByDeepLink(epicId);

    await drawer.closeViaButton();
  });

  test('closes via Escape', async ({ page, workPlane }) => {
    const epicId = 'gm-test-epic-close-esc';
    workPlane.seed([build.epic({ id: epicId })]);

    const drawer = new EpicDrawerPO(page);
    await drawer.openByDeepLink(epicId);

    await drawer.closeViaEscape();
  });

  test.fixme('inline state-change on member rows uses popover with valid transitions @deep', async () => {
    // Owned by gm-5v8v.8 follow-up + gm-5v8v.2 (real-backend writes).
  });

  test.fixme('Open in graph navigates to /graph with epic context @deep', async () => {
    // Requires gm-5v8v.7 (graph specs) + the action button to exist.
  });
});
