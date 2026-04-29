// specs/chrome/sidebar.spec.ts — six-item left rail (gm-e12.19, second
// amendment 2026-04-28).
//
// Sidebar nav consolidated to six first-order panes plus a bottom-
// anchored Settings entry. Secondary surfaces (Backlog, Sprints, Coach,
// Agent groups, Graph, Drift, Capability Browser, Mail) survive as
// deep-link routes today and roll up under their pane's in-screen
// tabs as gm-e12.19.4-7 land.
//
// gm-i65 / gm-uipx.13: /walk implicitly starts a walk on mount via
// POST /v1/walks:start. The fake backend returns {} for that
// endpoint so the WalkContext can't initialise — clicking the
// 'Review' link mid-loop leaves the page in an unfetched state
// and the next click can race the spinner. The link is asserted
// for visibility but excluded from the click-nav loop; the walk
// suite (walk.spec.ts) installs its own page.route stubs and
// covers the navigation contract there.
//
// gm-e11.8.2: escalation badge — numeric pill on the Escalations nav
// item, live-count from useEscalations (SSE-invalidated).

import { test, expect } from '../../fixtures/server';
import type { EscalationRequest } from '../../fixtures/escalationStore';

const NAV_LINKS = [
  { label: 'Plan', path: '/board' },
  // Review is rendered but click-nav-excluded — see comment above.
  { label: 'Review', path: '/walk', clickNav: false },
  { label: 'Escalations', path: '/escalations' },
  { label: 'Sessions', path: '/sessions' },
  { label: 'Settings', path: '/settings' },
] as const;

test.describe('Sidebar @chrome', () => {
  // gm-root.17.12: workspace-scoped sidebar items mute on cold-start
  // (no active project). Seed an active project so existing nav-loop
  // tests assert the operational path. Cold-start muting is asserted
  // in its own describe block below.
  test.beforeEach(({ projectsStore }) => {
    projectsStore.seed([
      { name: 'demo', path: '/tmp/projects/demo', active: true },
    ]);
  });

  test('renders the Gemba brand and every nav link', async ({ page }) => {
    await page.goto('/board');

    const sidebar = page.locator('aside').first();
    await expect(sidebar).toBeVisible();
    await expect(sidebar.getByText('Gemba', { exact: true })).toBeVisible();

    for (const { label } of NAV_LINKS) {
      await expect(sidebar.getByRole('link', { name: label })).toBeVisible();
    }
  });

  test('clicking a nav link navigates to its route', async ({ page }) => {
    // Re-anchor at /board for each iteration so a long sidebar loop
    // doesn't fight the AdaptorBanner / SSE re-renders that detach
    // the previous-iteration's link target between clicks. Each click
    // is now isolated: navigate, click, assert URL.
    for (const link of NAV_LINKS) {
      if ('clickNav' in link && link.clickNav === false) continue;
      await page.goto('/board');
      const sidebar = page.locator('aside').first();
      await sidebar.getByRole('link', { name: link.label }).click();
      await expect(page).toHaveURL(new RegExp(`${link.path}(?:[?/].*)?$`));
    }
  });

  test('the active link reflects the current route', async ({ page }) => {
    await page.goto('/sessions');
    const sessions = page.locator('aside').first().getByRole('link', { name: 'Sessions' });
    // NavLink stamps an `active` className when the route matches; we
    // assert the visual cue (bg-neutral-200 dark variant) only via
    // attribute, not the class string itself which churns with Tailwind.
    await expect(sessions).toHaveAttribute('href', '/sessions');
    // Active-link is the only link inside the aside whose computed
    // background isn't the hover transparent. Use aria-current as the
    // semantic check — react-router sets it on the active NavLink.
    await expect(sessions).toHaveAttribute('aria-current', 'page');
  });
});

// gm-root.17.12: cold-start happy path — workspace-scoped panes mute
// when no project is active. Settings is the only item in the
// six-pane rail that stays interactive without a workspace (Capability
// Browser folded into Settings as a tab under gm-e12.19.2).
test.describe('Sidebar cold-start @chrome', () => {
  test.beforeEach(({ projectsStore }) => {
    projectsStore.seed([]); // no projects → no active workspace
  });

  // Use /settings as the staging route — it stays interactive on
  // cold-start (gm-root.17.12) and does NOT trigger the /new →
  // /board+modal redirect (gm-e12.21.3) that races against
  // toHaveURL assertions in this suite.

  test('workspace-scoped panes render disabled', async ({ page }) => {
    await page.goto('/settings');
    const sidebar = page.locator('aside').first();
    await expect(sidebar).toBeVisible();

    const workspaceScoped = [
      'Plan',
      'Review',
      'Escalations',
      'Sessions',
    ];
    for (const label of workspaceScoped) {
      const item = sidebar.getByText(label, { exact: true });
      await expect(item).toBeVisible();
      // role="link" with aria-disabled="true" is rendered instead of a
      // real <a>: clicking does not navigate.
      const wrap = sidebar.locator('[aria-disabled="true"]', { hasText: label });
      await expect(wrap).toBeVisible();
    }
  });

  test('Settings stays interactive on cold-start', async ({ page }) => {
    await page.goto('/settings');
    const sidebar = page.locator('aside').first();
    await expect(sidebar.getByRole('link', { name: 'Settings' })).toBeVisible();
  });

  test('clicking a muted item does not navigate', async ({ page }) => {
    await page.goto('/settings');
    const sidebar = page.locator('aside').first();
    const muted = sidebar.locator('[aria-disabled="true"]', { hasText: 'Plan' });
    await muted.click({ force: true }); // bypass cursor: not-allowed for the test
    await expect(page).toHaveURL(/\/settings$/);
  });
});

// gm-e11.8.2: escalation badge — numeric pill on the Escalations nav item.
test.describe('Sidebar escalation badge @chrome', () => {
  test.beforeEach(({ projectsStore }) => {
    projectsStore.seed([
      { name: 'demo', path: '/tmp/projects/demo', active: true },
    ]);
  });

  test('shows open escalation count when escalations are seeded', async ({ page, escalationPlane }) => {
    const now = new Date().toISOString();
    const escalations: EscalationRequest[] = [
      { id: 'esc-1', source: 'question', urgency: 'advisory', title: 'Q1', prompt: 'p1', state: 'open', created_at: now },
      { id: 'esc-2', source: 'question', urgency: 'advisory', title: 'Q2', prompt: 'p2', state: 'open', created_at: now },
    ];
    escalationPlane.seed(escalations);

    await page.goto('/board');
    const sidebar = page.locator('aside').first();
    const badge = sidebar.locator('[data-testid="sidebar-escalations-badge"]');
    await expect(badge).toBeVisible();
    await expect(badge).toHaveText('2');
  });

  test('badge is absent when there are no escalations', async ({ page, escalationPlane }) => {
    escalationPlane.seed([]);
    await page.goto('/board');
    const sidebar = page.locator('aside').first();
    await expect(sidebar.getByRole('link', { name: 'Escalations' })).toBeVisible();
    await expect(sidebar.locator('[data-testid="sidebar-escalations-badge"]')).toHaveCount(0);
  });

  test('badge is absent on cold-start (muted Escalations item)', async ({ page, escalationPlane, projectsStore }) => {
    // Override projectsStore to cold-start state.
    projectsStore.seed([]);
    const now = new Date().toISOString();
    escalationPlane.seed([
      { id: 'esc-1', source: 'question', urgency: 'advisory', title: 'Q1', prompt: 'p', state: 'open', created_at: now },
    ]);
    await page.goto('/settings');
    const sidebar = page.locator('aside').first();
    // Escalations item is muted (aria-disabled).
    await expect(sidebar.locator('[aria-disabled="true"]', { hasText: 'Escalations' })).toBeVisible();
    // No badge anywhere.
    await expect(sidebar.locator('[data-testid="sidebar-escalations-badge"]')).toHaveCount(0);
  });
});
