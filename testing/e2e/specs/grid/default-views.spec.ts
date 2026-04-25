// specs/grid/default-views.spec.ts — gm-5v8v.6.3
//
// ui-spec §6.8 calls for canonical default views in the grid:
// "Staged", "In Progress", "Blocked", "Ready to stage", "Recently
// Done". gm-5v8v.6.3 ships the picker UI in GridPage; this spec
// asserts each view applies the right filter and post-filter.

import { test, expect } from '../../fixtures/server';

const NOW = '2026-04-25T12:00:00Z';
const SEVEN_DAYS_AGO = '2026-04-18T12:00:01Z';
const EIGHT_DAYS_AGO = '2026-04-17T12:00:00Z';

test.describe('Grid default views (ui-spec §6.8) @route', () => {
  test('picker exposes all five named views', async ({ page }) => {
    await page.goto('/grid');
    for (const id of [
      'grid-view-staged',
      'grid-view-in-progress',
      'grid-view-blocked',
      'grid-view-ready-to-stage',
      'grid-view-recently-done',
    ]) {
      await expect(page.getByTestId(id)).toBeVisible();
    }
  });

  test('Staged view filters to state_category=staged', async ({ page, workPlane }) => {
    workPlane.seed([
      { id: 'gm-1', kind: 'task', title: 'a staged item',  status: 'open',        state_category: 'staged',    created_at: NOW, updated_at: NOW },
      { id: 'gm-2', kind: 'task', title: 'an unstarted',   status: 'open',        state_category: 'unstarted', created_at: NOW, updated_at: NOW },
      { id: 'gm-3', kind: 'task', title: 'in progress',    status: 'in_progress', state_category: 'started',   created_at: NOW, updated_at: NOW },
    ]);
    await page.goto('/grid');
    await page.getByTestId('grid-view-staged').click();
    await expect(page.locator('[data-testid^="grid-row-"]')).toHaveCount(1);
    await expect(page.getByText('a staged item', { exact: false })).toBeVisible();
  });

  test('In Progress view filters to state_category=started', async ({ page, workPlane }) => {
    workPlane.seed([
      { id: 'gm-1', kind: 'task', title: 'progress one',   status: 'in_progress', state_category: 'started',   created_at: NOW, updated_at: NOW },
      { id: 'gm-2', kind: 'task', title: 'progress two',   status: 'in_progress', state_category: 'started',   created_at: NOW, updated_at: NOW },
      { id: 'gm-3', kind: 'task', title: 'a staged item',  status: 'open',        state_category: 'staged',    created_at: NOW, updated_at: NOW },
    ]);
    await page.goto('/grid');
    await page.getByTestId('grid-view-in-progress').click();
    await expect(page.locator('[data-testid^="grid-row-"]')).toHaveCount(2);
    await expect(page.getByText('progress one', { exact: false })).toBeVisible();
    await expect(page.getByText('progress two', { exact: false })).toBeVisible();
  });

  test('Blocked view derives blocked rows from human_action_required', async ({ page, workPlane }) => {
    // Server-side derivation of blocked-ness lives in DerivedSignals
    // (gm-gsh) — the SPA reads `derived.human_action_required` rather
    // than re-deriving from open escalations + edges. The cross-source
    // derivation lives server-side; we only assert the post-filter.
    workPlane.seed([
      { id: 'gm-1', kind: 'task', title: 'blocked item',   status: 'open', state_category: 'started',   created_at: NOW, updated_at: NOW, derived: { agent_claimable: false, human_action_required: true,  review_pending: false } },
      { id: 'gm-2', kind: 'task', title: 'unblocked item', status: 'open', state_category: 'started',   created_at: NOW, updated_at: NOW, derived: { agent_claimable: true,  human_action_required: false, review_pending: false } },
      { id: 'gm-3', kind: 'task', title: 'no derived',     status: 'open', state_category: 'unstarted', created_at: NOW, updated_at: NOW },
    ]);
    await page.goto('/grid');
    await page.getByTestId('grid-view-blocked').click();
    await expect(page.locator('[data-testid^="grid-row-"]')).toHaveCount(1);
    await expect(page.getByText('blocked item', { exact: false })).toBeVisible();
  });

  test('Ready to stage view filters by derived.agent_claimable', async ({ page, workPlane }) => {
    workPlane.seed([
      { id: 'gm-1', kind: 'task', title: 'ready bead',     status: 'open', state_category: 'unstarted', created_at: NOW, updated_at: NOW, derived: { agent_claimable: true,  human_action_required: false, review_pending: false } },
      { id: 'gm-2', kind: 'task', title: 'not ready',      status: 'open', state_category: 'unstarted', created_at: NOW, updated_at: NOW, derived: { agent_claimable: false, human_action_required: false, review_pending: false } },
      { id: 'gm-3', kind: 'task', title: 'wrong state',    status: 'open', state_category: 'staged',    created_at: NOW, updated_at: NOW, derived: { agent_claimable: true,  human_action_required: false, review_pending: false } },
    ]);
    await page.goto('/grid');
    await page.getByTestId('grid-view-ready-to-stage').click();
    await expect(page.locator('[data-testid^="grid-row-"]')).toHaveCount(1);
    await expect(page.getByText('ready bead', { exact: false })).toBeVisible();
  });

  test('Recently Done view filters to state_category=completed within last 7d, sorted desc', async ({ page, workPlane }) => {
    workPlane.seed([
      { id: 'gm-old',    kind: 'task', title: 'closed-stale', status: 'closed',      state_category: 'completed', created_at: EIGHT_DAYS_AGO, updated_at: EIGHT_DAYS_AGO },
      { id: 'gm-recent', kind: 'task', title: 'closed-fresh', status: 'closed',      state_category: 'completed', created_at: SEVEN_DAYS_AGO, updated_at: SEVEN_DAYS_AGO },
      { id: 'gm-now',    kind: 'task', title: 'closed-now',   status: 'closed',      state_category: 'completed', created_at: NOW,            updated_at: NOW },
      { id: 'gm-open',   kind: 'task', title: 'open-recent',  status: 'in_progress', state_category: 'started',   created_at: NOW,            updated_at: NOW },
    ]);
    // The post-filter compares updated_at against Date.now(); fix
    // browser time to the assertion's NOW so the 7-day window is
    // deterministic. The fake-mode dispatcher returns updated_at
    // unchanged from the seed.
    await page.clock.install({ time: new Date(NOW) });
    await page.goto('/grid');
    await page.getByTestId('grid-view-recently-done').click();
    await expect(page.locator('[data-testid^="grid-row-"]')).toHaveCount(2);
    // Sort: newest updated_at first (gm-now before gm-recent).
    const rows = page.locator('[data-testid^="grid-row-"]');
    const titles = await rows.allInnerTexts();
    expect(titles[0]).toContain('closed-now');
    expect(titles[1]).toContain('closed-fresh');
  });

  test('clicking the active view a second time clears it', async ({ page, workPlane }) => {
    workPlane.seed([
      { id: 'gm-1', kind: 'task', title: 'staged',     status: 'open',        state_category: 'staged',    created_at: NOW, updated_at: NOW },
      { id: 'gm-2', kind: 'task', title: 'in-progress', status: 'in_progress', state_category: 'started',   created_at: NOW, updated_at: NOW },
    ]);
    await page.goto('/grid');
    await page.getByTestId('grid-view-staged').click();
    await expect(page.locator('[data-testid^="grid-row-"]')).toHaveCount(1);
    await page.getByTestId('grid-view-staged').click();
    // After clearing, no view filter is active — the chip filter
    // also reset, so all seeded rows show.
    await expect(page.locator('[data-testid^="grid-row-"]')).toHaveCount(2);
  });

  test('?view= URL param activates the view on first paint', async ({ page, workPlane }) => {
    workPlane.seed([
      { id: 'gm-1', kind: 'task', title: 'a staged item',  status: 'open',        state_category: 'staged',  created_at: NOW, updated_at: NOW },
      { id: 'gm-2', kind: 'task', title: 'in progress',    status: 'in_progress', state_category: 'started', created_at: NOW, updated_at: NOW },
    ]);
    await page.goto('/grid?view=staged');
    await expect(page.getByTestId('grid-view-staged')).toHaveAttribute('data-active', 'true');
    await expect(page.locator('[data-testid^="grid-row-"]')).toHaveCount(1);
  });
});
