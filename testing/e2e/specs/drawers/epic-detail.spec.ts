// specs/drawers/epic-detail.spec.ts — gm-root.22.6
//
// Covers EpicDetail header (id / copy), action buttons
// (Stage / Start / Dispatch / New child), state section, children
// section bucketed by state, RHP URL codec, and per-route scoping.
// Replaces epic-drawer.spec.ts (gm-5v8v.8).

import { test, expect } from '../../fixtures/server';
import { EpicDetailPO } from '../../pages/EpicDetail';
import * as build from '../../builders/workitem';

test.describe('EpicDetail @route', () => {
  test('opens via /board/:epicId and renders header + state', async ({ page, workPlane }) => {
    const epicId = 'gm-test-epic';
    workPlane.seed([
      build.epic({ id: epicId, title: 'Test epic', state_category: 'staged', status: 'staged' }),
    ]);

    const detail = new EpicDetailPO(page);
    await detail.openByDeepLink(epicId);

    await detail.expectOpenWith(epicId);
    // The state pill renders status + state_category.
    await expect(detail.stateSection).toContainText('staged');
  });

  test('URL grows ?rhp=epic:<id> when /board/:epicId is visited', async ({ page, workPlane }) => {
    const epicId = 'gm-test-epic-url';
    workPlane.seed([build.epic({ id: epicId })]);

    const detail = new EpicDetailPO(page);
    await detail.openByDeepLink(epicId);

    // The RHP codec adds ?rhp=epic:<epicId> to the URL.
    await expect(page).toHaveURL(new RegExp(`rhp=epic(?::|%3A)${encodeURIComponent(epicId)}`));
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

    const detail = new EpicDetailPO(page);
    await detail.openByDeepLink(epicId);

    await expect(detail.stage).toBeVisible();
    await expect(detail.start).toBeVisible();
    await expect(detail.dispatch).toBeVisible();
    await expect(detail.newChild).toBeVisible();
  });

  test('Stage is disabled when not agent_claimable', async ({ page, workPlane }) => {
    const epicId = 'gm-test-epic-blocked';
    workPlane.seed([
      build.epic({
        id: epicId,
        derived: { agent_claimable: false, human_action_required: true, review_pending: false },
      }),
    ]);

    const detail = new EpicDetailPO(page);
    await detail.openByDeepLink(epicId);

    await expect(detail.stage).toHaveAttribute('data-disabled', 'true');
    await expect(detail.start).toHaveAttribute('data-disabled', 'true');
  });

  test('renders children bucketed by state when present', async ({ page, workPlane }) => {
    const epicId = 'gm-test-epic-with-children';
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

    const detail = new EpicDetailPO(page);
    await detail.openByDeepLink(epicId);

    await expect(detail.childrenSection).toContainText('Children (2)');
    await expect(page.getByTestId('epic-children-completed')).toBeVisible();
    await expect(page.getByTestId('epic-children-started')).toBeVisible();
  });

  test('navigating away clears the epic detail tab (per-route scoping)', async ({
    page,
    workPlane,
  }) => {
    const epicId = 'gm-test-epic-scope';
    workPlane.seed([build.epic({ id: epicId })]);

    const detail = new EpicDetailPO(page);
    await detail.openByDeepLink(epicId);

    // Confirm the tab opened.
    await expect(detail.idLabel).toBeVisible();
    await expect(page).toHaveURL(/rhp=epic/);

    // Navigate away — per-current-route scoping should clear the tab.
    await page.goto('/escalations');
    await expect(page).not.toHaveURL(/[?&]rhp=/);

    // Navigate back — the detail tab is NOT reinstated (state is route-scoped,
    // not per-history-entry).
    await page.goto('/board');
    await expect(detail.idLabel).toHaveCount(0);
  });

  test.fixme('inline state-change on member rows uses popover with valid transitions @deep', async () => {
    // Tracked under gm-ocx3 (SPA) — member rows are read-only today.
    // Lifts when that bead closes.
  });

  test.fixme('Open in graph navigates to /graph with epic context @deep', async () => {
    // Tracked under gm-qoio (SPA) — EpicDetail's actions toolbar
    // doesn't expose an Open-in-graph button yet. Lifts when that
    // bead closes.
  });
});
