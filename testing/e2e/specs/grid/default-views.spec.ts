// specs/grid/default-views.spec.ts — gm-5v8v.6.3
//
// The visible Grid route was retired; the underlying WorkItemGrid still
// has a legacy power-list host at /board?layout=list&power=1 so old
// deep links and named-view URLs keep working. Refine owns the visible
// table affordance, while this spec pins URL-level compatibility for
// the named view registry.

import { test, expect } from '../../fixtures/server';

const NOW = '2026-04-25T12:00:00Z';
const SEVEN_DAYS_AGO = '2026-04-18T12:00:01Z';
const EIGHT_DAYS_AGO = '2026-04-17T12:00:00Z';

function legacyList(view?: string): string {
  const params = new URLSearchParams({ layout: 'list', power: '1' });
  if (view) params.set('view', view);
  return `/board?${params.toString()}`;
}

test.describe('Legacy list named views @route', () => {
  test('no named view shows the seeded list', async ({ page, workPlane }) => {
    workPlane.seed([
      { id: 'gm-1', kind: 'task', title: 'staged', status: 'open', state_category: 'staged', created_at: NOW, updated_at: NOW },
      { id: 'gm-2', kind: 'task', title: 'in-progress', status: 'in_progress', state_category: 'started', created_at: NOW, updated_at: NOW },
    ]);
    await page.goto(legacyList());
    await expect(page.locator('[data-testid^="grid-row-gm-"]')).toHaveCount(2);
  });

  test('Staged view filters to state_category=staged', async ({ page, workPlane }) => {
    workPlane.seed([
      { id: 'gm-1', kind: 'task', title: 'a staged item', status: 'open', state_category: 'staged', created_at: NOW, updated_at: NOW },
      { id: 'gm-2', kind: 'task', title: 'an unstarted', status: 'open', state_category: 'unstarted', created_at: NOW, updated_at: NOW },
      { id: 'gm-3', kind: 'task', title: 'in progress', status: 'in_progress', state_category: 'started', created_at: NOW, updated_at: NOW },
    ]);
    await page.goto(legacyList('staged'));
    await expect(page.locator('[data-testid^="grid-row-gm-"]')).toHaveCount(1);
    await expect(page.getByText('a staged item', { exact: false })).toBeVisible();
  });

  test('In Progress view filters to state_category=started', async ({ page, workPlane }) => {
    workPlane.seed([
      { id: 'gm-1', kind: 'task', title: 'progress one', status: 'in_progress', state_category: 'started', created_at: NOW, updated_at: NOW },
      { id: 'gm-2', kind: 'task', title: 'progress two', status: 'in_progress', state_category: 'started', created_at: NOW, updated_at: NOW },
      { id: 'gm-3', kind: 'task', title: 'a staged item', status: 'open', state_category: 'staged', created_at: NOW, updated_at: NOW },
    ]);
    await page.goto(legacyList('in-progress'));
    await expect(page.locator('[data-testid^="grid-row-gm-"]')).toHaveCount(2);
    await expect(page.getByText('progress one', { exact: false })).toBeVisible();
    await expect(page.getByText('progress two', { exact: false })).toBeVisible();
  });

  test('Blocked view derives blocked rows from human_action_required', async ({ page, workPlane }) => {
    workPlane.seed([
      { id: 'gm-1', kind: 'task', title: 'blocked item', status: 'open', state_category: 'started', created_at: NOW, updated_at: NOW, derived: { agent_claimable: false, human_action_required: true, review_pending: false } },
      { id: 'gm-2', kind: 'task', title: 'unblocked item', status: 'open', state_category: 'started', created_at: NOW, updated_at: NOW, derived: { agent_claimable: true, human_action_required: false, review_pending: false } },
      { id: 'gm-3', kind: 'task', title: 'no derived', status: 'open', state_category: 'unstarted', created_at: NOW, updated_at: NOW },
    ]);
    await page.goto(legacyList('blocked'));
    await expect(page.locator('[data-testid^="grid-row-gm-"]')).toHaveCount(1);
    await expect(page.getByText('blocked item', { exact: false })).toBeVisible();
  });

  test('Ready to stage view filters by derived.agent_claimable', async ({ page, workPlane }) => {
    workPlane.seed([
      { id: 'gm-1', kind: 'task', title: 'ready bead', status: 'open', state_category: 'unstarted', created_at: NOW, updated_at: NOW, derived: { agent_claimable: true, human_action_required: false, review_pending: false } },
      { id: 'gm-2', kind: 'task', title: 'not ready', status: 'open', state_category: 'unstarted', created_at: NOW, updated_at: NOW, derived: { agent_claimable: false, human_action_required: false, review_pending: false } },
      { id: 'gm-3', kind: 'task', title: 'wrong state', status: 'open', state_category: 'staged', created_at: NOW, updated_at: NOW, derived: { agent_claimable: true, human_action_required: false, review_pending: false } },
    ]);
    await page.goto(legacyList('ready-to-stage'));
    await expect(page.locator('[data-testid^="grid-row-gm-"]')).toHaveCount(1);
    await expect(page.getByText('ready bead', { exact: false })).toBeVisible();
  });

  test('Recently Done view filters to state_category=completed within last 7d, sorted desc', async ({ page, workPlane }) => {
    workPlane.seed([
      { id: 'gm-old', kind: 'task', title: 'closed-stale', status: 'closed', state_category: 'completed', created_at: EIGHT_DAYS_AGO, updated_at: EIGHT_DAYS_AGO },
      { id: 'gm-recent', kind: 'task', title: 'closed-fresh', status: 'closed', state_category: 'completed', created_at: SEVEN_DAYS_AGO, updated_at: SEVEN_DAYS_AGO },
      { id: 'gm-now', kind: 'task', title: 'closed-now', status: 'closed', state_category: 'completed', created_at: NOW, updated_at: NOW },
      { id: 'gm-open', kind: 'task', title: 'open-recent', status: 'in_progress', state_category: 'started', created_at: NOW, updated_at: NOW },
    ]);
    await page.clock.setFixedTime(new Date(NOW));
    await page.goto(legacyList('done-recent'));
    await expect(page.locator('[data-testid^="grid-row-gm-"]')).toHaveCount(2);
    const rows = page.locator('[data-testid^="grid-row-gm-"]');
    const titles = await rows.allInnerTexts();
    expect(titles[0]).toContain('closed-now');
    expect(titles[1]).toContain('closed-fresh');
  });
});
