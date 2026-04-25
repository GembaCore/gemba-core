// specs/drawers/workitem-evidence.spec.ts — gm-5v8v.8 (in progress)
//
// Evidence is rendered inside the Summary tab, not as a separate
// tab — confirm the layout, citations link inline.

import { test } from '../../fixtures/server';

test.describe('WorkItemDrawer Evidence @route', () => {
  test.fixme('evidence table renders inside Summary (not separate tab)', async () => {
    // Seed an evidence array on the work item, open the drawer,
    // assert the table appears under the description tab pane.
  });

  test.fixme('citation links open the source ref', async () => {
    // Each Evidence has source + ref; the rendered link should
    // carry the ref as href. Click target depends on Evidence.kind.
  });
});
