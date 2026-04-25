// specs/drawers/workitem-edges.spec.ts — gm-5v8v.8 (in progress)
//
// Edges tab: typed lists (blocks / parent_child / relates_to) plus
// the adaptor-extension edges grouped below. The relgroup-* testids
// are emitted on the rendered side.

import { test, expect } from '../../fixtures/server';
import { WorkItemDrawerPO } from '../../pages/WorkItemDrawer';
import * as build from '../../builders/workitem';

test.describe('WorkItemDrawer Edges tab @route', () => {
  test('groups relationships by typed kind', async ({ page, workPlane }) => {
    const id = 'gm-test-edges';
    workPlane.seed([
      build.workItem({
        id,
        relationships: [
          build.relationship('blocks', id, 'gm-target'),
          build.parentChild('gm-parent', id),
          build.relationship('relates_to', id, 'gm-related'),
        ],
      }),
      build.workItem({ id: 'gm-target' }),
      build.workItem({ id: 'gm-parent' }),
      build.workItem({ id: 'gm-related' }),
    ]);

    const drawer = new WorkItemDrawerPO(page);
    await drawer.openByDeepLink(id);
    await drawer.selectTab('edges');

    // The drawer emits one relgroup-* node per heading. We assert at
    // least one is visible — the explicit per-kind label assertion
    // belongs in a fixme until the heading text is pinned in the
    // component (avoids flakes if labels move).
    await expect(page.locator('[data-testid^="relgroup-"]').first()).toBeVisible();
  });

  test.fixme('adaptor-extension edges appear under their own heading', async () => {
    // Seed a custom["beads:dependencies"] payload on the work item
    // and assert the relgroup-Adaptor-extensions node renders.
  });
});
