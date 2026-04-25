// specs/drawers/workitem-evidence.spec.ts — gm-5v8v.8
//
// Per ui-spec §5.7, Evidence is a table on the Summary tab, not its
// own tab. The Summary tab is the SPA's 'description' tab. Section
// renders inline at the bottom of the pane (gm-g9t1).

import { test, expect } from '../../fixtures/server';
import { WorkItemDrawerPO } from '../../pages/WorkItemDrawer';
import * as build from '../../builders/workitem';

test.describe('WorkItemDrawer Evidence @route', () => {
  test('Evidence renders inline on the Summary (description) tab', async ({
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
    // Description tab is active by default; Evidence is in the same pane.
    const section = page.getByTestId('section-evidence');
    await expect(section).toBeVisible();
    await expect(section).toContainText('commit');
    await expect(section).toContainText('git');
    await expect(section).toContainText('abc123');
    await expect(section).toContainText('Refactored gem');
    await expect(section).toContainText('log');
    await expect(section).toContainText('ci');
    await expect(section).toContainText('Tests passed');
  });

  test('Evidence section shows empty-state copy when no Evidence is attached', async ({
    page,
    workPlane,
  }) => {
    const id = 'gm-ev-empty';
    workPlane.seed([build.workItem({ id })]);

    const drawer = new WorkItemDrawerPO(page);
    await drawer.openByDeepLink(id);

    const section = page.getByTestId('section-evidence');
    await expect(section).toContainText('No evidence attached');
  });

  test('no standalone "Evidence" tab appears in the tablist', async ({
    page,
    workPlane,
  }) => {
    workPlane.seed([build.workItem({ id: 'gm-ev-no-tab' })]);
    const drawer = new WorkItemDrawerPO(page);
    await drawer.openByDeepLink('gm-ev-no-tab');
    await expect(drawer.tabs).toBeVisible();
    await expect(page.getByTestId('drawer-tab-evidence')).toHaveCount(0);
  });

  test('citation refs render as <a> when the ref is a URL (gm-4z9n)', async ({
    page,
    workPlane,
  }) => {
    // EvidenceRef promotes the ref to a link when it resolves to a
    // URL — anything starting with https?:// or kind=url falls through
    // resolveEvidenceHref. Plain refs stay as text.
    const id = 'gm-ev-cite';
    workPlane.seed([
      build.workItem({
        id,
        evidence: [
          {
            id: 'ev-link',
            kind: 'commit',
            source: 'git',
            ref: 'https://github.com/example/repo/commit/abc',
            summary: 'PR landed',
            captured_at: '2026-04-25T03:00:00Z',
          },
          {
            id: 'ev-plain',
            kind: 'custom',
            source: 'manual',
            ref: 'see Slack #incident',
            summary: 'Notes',
            captured_at: '2026-04-25T04:00:00Z',
          },
        ],
      }),
    ]);

    const drawer = new WorkItemDrawerPO(page);
    await drawer.openByDeepLink(id);

    const link = page.getByTestId('evidence-ref-ev-link');
    await expect(link).toBeVisible();
    await expect(link).toHaveAttribute(
      'href',
      'https://github.com/example/repo/commit/abc'
    );
    await expect(link).toHaveAttribute('rel', 'noopener noreferrer');
    // The plain ref does NOT render as a link — no testid emitted.
    await expect(page.getByTestId('evidence-ref-ev-plain')).toHaveCount(0);
  });
});
