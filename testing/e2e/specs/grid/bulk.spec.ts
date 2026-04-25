// specs/grid/bulk.spec.ts — gm-5v8v.6 (closed)
//
// Bulk actions (Cmd-E edit / Cmd-D defer / right-click on selection)
// are named on the bead but the WorkItemGrid currently has no
// selection model — rows render as a single click target that opens
// the drawer.
//
// Tracking: gm-5v8v.6.2 (feat(spa): WorkItemGrid row selection +
// bulk actions). Un-fixme each test below when that bead closes.

import { test } from '../../fixtures/server';

test.describe('WorkItemGrid bulk actions @route', () => {
  test.fixme('multi-select via Shift+click extends a selection range', async () => {
    // Needs the grid to grow row-level selection state + a visual
    // affordance (checkbox column or selected-row class).
  });

  test.fixme('Cmd-E opens the bulk-edit dialog over the selected rows', async () => {
    // Hotkey from gm-5v8v.4 (chrome / hotkeys); editor surface TBD.
  });

  test.fixme('Cmd-D defers every selected row in one nonce-batched call', async () => {
    // Same gating; the deep variant verifies bd state moved to
    // backlog for every id in the selection.
  });

  test.fixme('right-click on a selection opens a context menu', async () => {
    // Needs a context-menu component; not yet in the SPA.
  });
});
