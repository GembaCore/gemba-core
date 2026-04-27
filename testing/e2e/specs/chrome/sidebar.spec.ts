// specs/chrome/sidebar.spec.ts — gm-5v8v.4
//
// Sidebar nav: every visible link is reachable and routes to the
// expected pathname. The Sidebar is feature-gated only on /mail
// (gated by features.mail), so the universally-visible items below
// are the smoke surface — when somebody adds a top-level destination
// they update Sidebar.tsx + this list.

import { test, expect } from '../../fixtures/server';

// gm-e12.19.1: "Backlog" sidebar item still points at /backlog but
// the route is now a permanent redirect to Board's list mode +
// Backlog view. Click-then-URL assertion lands on the resolved URL.
//
// gm-uipx.17: the "Grid" sidebar entry was dropped — /grid folded
// into Board's list+power layout, so Grid is no longer a top-level
// destination. The /grid bookmark redirect is covered by the
// dedicated grid-redirect spec.
const NAV_LINKS = [
  { label: 'Board', path: '/board' },
  { label: 'Backlog', path: '/board\\?layout=list&view=backlog' },
  { label: 'Sessions', path: '/sessions' },
  { label: 'Gemba walk', path: '/walk' },
  { label: 'Graph', path: '/graph' },
  { label: 'Insights', path: '/insights' },
  { label: 'Escalations', path: '/escalations' },
  { label: 'Capability Browser', path: '/capabilities' },
  // Coach is rendered in the sidebar (asserted by the renders test
  // below) but excluded from the click-nav loop because /coach
  // currently throws on mount in the route-fake harness — investigated
  // in its own bead, not in scope for gm-uipx.17.
  { label: 'Coach', path: '/coach', clickNav: false },
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
    await page.goto('/board');
    const sidebar = page.locator('aside').first();

    for (const link of NAV_LINKS) {
      if ('clickNav' in link && link.clickNav === false) continue;
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
