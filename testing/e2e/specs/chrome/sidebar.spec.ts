// specs/chrome/sidebar.spec.ts — gm-isxs (refresh after gm-e12.19+)
//
// Sidebar nav: every visible link is reachable and routes to the
// expected pathname. The Sidebar grew Sprints, Agent groups, Drift,
// and Settings since the spec was last touched (the 'Setup' tab
// referenced in the bead description has not yet shipped). Mail
// stays gated behind features.mail and is excluded from the universal
// surface check.

import { test, expect } from '../../fixtures/server';

// gm-e12.19.1: "Backlog" sidebar item still points at /backlog but
// the route is now a permanent redirect to Board's list mode +
// Backlog view. Click-then-URL assertion lands on the resolved URL.
//
// gm-uipx.17: the "Grid" sidebar entry was dropped — /grid folded
// into Board's list+power layout, so Grid is no longer a top-level
// destination. The /grid bookmark redirect is covered by the
// dedicated grid-redirect spec.
//
// gm-i65 / gm-uipx.13: /walk implicitly starts a walk on mount via
// POST /v1/walks:start. The fake backend returns {} for that
// endpoint so the WalkContext can't initialise — clicking the
// 'Gemba walk' link mid-loop leaves the page in an unfetched state
// and the next click can race the spinner. The link is asserted
// for visibility but excluded from the click-nav loop; the walk
// suite (walk.spec.ts) installs its own page.route stubs and
// covers the navigation contract there.
const NAV_LINKS = [
  { label: 'Board', path: '/board' },
  { label: 'Backlog', path: '/board\\?layout=list&view=backlog' },
  { label: 'Sprints', path: '/sprints' },
  { label: 'Sessions', path: '/sessions' },
  { label: 'Agent groups', path: '/agent-groups' },
  // Coach is rendered (asserted by the renders test below) but
  // excluded from the click-nav loop because /coach currently throws
  // on mount in the route-fake harness — investigated in its own
  // bead, not in scope for gm-isxs.
  { label: 'Coach', path: '/coach', clickNav: false },
  // Walk is rendered but click-nav-excluded — see comment above.
  { label: 'Review', path: '/walk', clickNav: false },
  { label: 'Graph', path: '/graph' },
  { label: 'Insights', path: '/insights' },
  { label: 'Escalations', path: '/escalations' },
  { label: 'Capability Browser', path: '/capabilities' },
  { label: 'Drift', path: '/drift' },
  { label: 'Settings', path: '/settings' },
] as const;

test.describe('Sidebar @chrome', () => {
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
