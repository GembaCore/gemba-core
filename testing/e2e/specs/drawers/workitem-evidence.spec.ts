// specs/drawers/workitem-evidence.spec.ts — gm-5v8v.8
//
// The bead originally said "evidence table renders inside Summary
// (not separate tab)" — but the SPA actually renders Evidence under
// its own `evidence` tab (WorkItemDrawer.tsx L434). The spec mirrors
// the implementation; the tab-vs-Summary disagreement is captured
// as a fixme so a future ui-spec or SPA change can decide which
// surface wins.

import { test, expect } from '../../fixtures/server';
import { WorkItemDrawerPO } from '../../pages/WorkItemDrawer';
import * as build from '../../builders/workitem';

test.describe('WorkItemDrawer Evidence @route', () => {
  test('Evidence tab renders one row per Evidence entry', async ({
    page,
    workPlane,
  }) => {
    const id = 'gm-ev-1';
    workPlane.seed([
      build.workItem({
        id,
        evidence: [
          {
            id: 'ev-a',
            kind: 'commit',
            source: 'git',
            ref: 'abc123',
            summary: 'Refactored gem',
            captured_at: '2026-04-25T01:00:00Z',
          },
          {
            id: 'ev-b',
            kind: 'log',
            source: 'ci',
            ref: 'job/42',
            summary: 'Tests passed',
            captured_at: '2026-04-25T02:00:00Z',
          },
        ],
      }),
    ]);

    const drawer = new WorkItemDrawerPO(page);
    await drawer.openByDeepLink(id);
    await drawer.selectTab('evidence');

    const section = page.getByTestId('section-evidence');
    await expect(section).toBeVisible();
    // EvidenceRow surfaces kind / source / ref / summary inline.
    await expect(section).toContainText('commit');
    await expect(section).toContainText('git');
    await expect(section).toContainText('abc123');
    await expect(section).toContainText('Refactored gem');
    await expect(section).toContainText('log');
    await expect(section).toContainText('ci');
    await expect(section).toContainText('Tests passed');
  });

  test('Evidence tab shows empty-state copy when no Evidence is attached', async ({
    page,
    workPlane,
  }) => {
    const id = 'gm-ev-empty';
    workPlane.seed([build.workItem({ id })]);

    const drawer = new WorkItemDrawerPO(page);
    await drawer.openByDeepLink(id);
    await drawer.selectTab('evidence');

    const section = page.getByTestId('section-evidence');
    await expect(section).toContainText('No evidence attached');
  });

  test.fixme(
    'evidence renders inside Summary instead of its own tab',
    async () => {
      // The bead expects the table inside Summary; the SPA renders
      // it under the `evidence` tab. Spec captures the disagreement
      // until ui-spec / SPA reconciles which surface is canonical.
    }
  );

  test.fixme('citation links open the source ref', async () => {
    // EvidenceRow currently renders ref as plain text, not an <a>.
    // When the SPA promotes the ref to a link this fixme lifts.
  });
});
