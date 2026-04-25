// specs/grid/inline-edit.spec.ts — gm-5v8v.6 (closed)
//
// Inline-edit is named on the bead but does NOT exist in
// WorkItemGrid yet (gm-e12.3.1 ships row-click → drawer). The
// drawer carries field editors with their own
// work-item-{title,priority,status,labels,description,sprint,dod}-*
// testids — exercising those lives in
// specs/drawers/workitem-drawer.spec.ts under gm-5v8v.8.
//
// Tracking: gm-5v8v.6.1 (feat(spa): WorkItemGrid in-cell editing).
// Un-fixme each test below when that bead closes.

import { test } from '../../fixtures/server';

test.describe('WorkItemGrid inline edit @route', () => {
  test.fixme('click-to-edit activates a cell editor', async () => {
    // Lands when the grid grows in-cell editing (no SPA bead yet).
  });

  test.fixme('Escape cancels the in-flight edit without firing PATCH', async () => {
    // Same surface — needs the editor to exist first.
  });

  test.fixme('Enter commits and fires PATCH /api/work-items/:id with a fresh nonce', async () => {
    // Network assertion is in scope: the spec should observe one
    // PATCH per commit and confirm the X-GEMBA-Confirm header is
    // present + unique per click.
  });
});
