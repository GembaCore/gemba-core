// specs/chrome/sidebar.spec.ts — gm-5v8v.4
//
// Sidebar nav: every visible link is reachable and routes to the
// expected pathname. The Sidebar is feature-gated only on /mail
// (gated by features.mail), so the universally-visible items below
// are the smoke surface — when somebody adds a top-level destination
// they update Sidebar.tsx + this list.

import { test, expect } from '../../fixtures/server';

const NAV_LINKS = [
  { label: 'Board', path: '/board' },
  { label: 'Backlog', path: '/backlog' },
  { label: 'Grid', path: '/grid' },
  { label: 'Sessions', path: '/sessions' },
  { label: 'Agents', path: '/agents' },
  { label: 'Graph', path: '/graph' },
  { label: 'Insights', path: '/insights' },
  { label: 'Escalations', path: '/escalations' },
  { label: 'Capability Browser', path: '/capabilities' },
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

    for (const { label, path } of NAV_LINKS) {
      await sidebar.getByRole('link', { name: label }).click();
      await expect(page).toHaveURL(new RegExp(`${path}(?:[?/].*)?$`));
    }
  });

  test('the active link reflects the current route', async ({ page }) => {
    await page.goto('/grid');
    const grid = page.locator('aside').first().getByRole('link', { name: 'Grid' });
    // NavLink stamps an `active` className when the route matches; we
    // assert the visual cue (bg-neutral-200 dark variant) only via
    // attribute, not the class string itself which churns with Tailwind.
    await expect(grid).toHaveAttribute('href', '/grid');
    // Active-link is the only link inside the aside whose computed
    // background isn't the hover transparent. Use aria-current as the
    // semantic check — react-router sets it on the active NavLink.
    await expect(grid).toHaveAttribute('aria-current', 'page');
  });
});
