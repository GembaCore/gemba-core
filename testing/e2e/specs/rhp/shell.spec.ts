// specs/rhp/shell.spec.ts — gm-root.22.2.
//
// The Right-Hand Panel shell renders to the right of <main> on every
// top-level route. Tab content (Help, detail tabs) lands in sibling
// beads .3 / .4; this spec covers the shell chrome only:
//
//   - Shell renders on every top-level route.
//   - Collapse persists across reload (localStorage key
//     `gemba.rhp.collapsed`).

import { test, expect } from '../../fixtures/server';

test.describe('RHP shell @chrome', () => {
  test('renders on every top-level route', async ({ page }) => {
    // Sidebar's primary panes per gm-e12.19 (Plan / Review / Triage /
    // Sessions / Settings). Cold-start collapses the rail to 40px;
    // we assert presence, not width.
    //
    // /walk is excluded here because BottomBar reads
    // `walk.cost.dollars` directly (no optional chain on `cost`), and
    // the fake-backend default doesn't seed walk state — the page
    // throws before the shell paints. The walk-spec fixture seeds
    // walk state for the dedicated walk specs; an RHP-on-/walk
    // assertion belongs in those once BottomBar gains a cost guard.
    for (const route of ['/board', '/escalations', '/sessions', '/settings']) {
      await page.goto(route);
      await expect(page.getByTestId('rhp-shell')).toBeVisible();
      await expect(page.getByTestId('rhp-rail')).toBeVisible();
    }
  });

  test('collapse persists across reload', async ({ page }) => {
    await page.goto('/board');
    const shell = page.getByTestId('rhp-shell');
    await expect(shell).toBeVisible();

    // Read current collapsed state. Cold-start with no pinned tabs
    // registered is collapsed; once Help (.3) lands the auto-expand
    // branch flips the default. Either way, click the toggle to land
    // on a deterministic non-default state and persist.
    const initialCollapsed = await shell.getAttribute('data-collapsed');
    await page.getByTestId('rhp-collapse-toggle').click();
    const flipped = initialCollapsed === 'true' ? 'false' : 'true';
    await expect(shell).toHaveAttribute('data-collapsed', flipped);

    await page.reload();
    await expect(page.getByTestId('rhp-shell')).toHaveAttribute('data-collapsed', flipped);
  });
});
