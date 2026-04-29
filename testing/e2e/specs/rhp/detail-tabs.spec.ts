// specs/rhp/detail-tabs.spec.ts — gm-root.22.4.
//
// Detail-tab plumbing for the Right-Hand Panel:
//   - Deep-link with `?rhp=<kind>:<id>(,<kind>:<id>)*` opens the right
//     tabs and pre-focuses the rightmost on first paint.
//   - Closing a detail tab removes its segment from the URL.
//   - Navigating to a different top-level route clears all detail tabs
//     and the `?rhp` param.
//
// Kind owners (.5/.6/.7) register actual content; until they do, the
// shell renders a placeholder body for popped tabs. We assert on the
// rail icons + URL state rather than body content, so this spec stays
// green as the migration progresses.

import { test, expect } from '../../fixtures/server';

test.describe('RHP detail tabs @chrome', () => {
  test('deep-link with ?rhp=... opens the right tabs', async ({ page }) => {
    // Use `:` and `,` directly in the URL — Playwright handles encoding.
    await page.goto('/board?rhp=workitem:gm-1,epic:gm-2');
    await expect(page.getByTestId('rhp-shell')).toBeVisible();
    // Both tab icons appear on the rail with stable testids.
    await expect(page.getByTestId('rhp-tab-workitem:gm-1')).toBeVisible();
    await expect(page.getByTestId('rhp-tab-epic:gm-2')).toBeVisible();
    // Rightmost is focused on first paint.
    await expect(page.getByTestId('rhp-tab-epic:gm-2')).toHaveAttribute(
      'data-active',
      'true'
    );
    await expect(page.getByTestId('rhp-tab-workitem:gm-1')).toHaveAttribute(
      'data-active',
      'false'
    );
  });

  test('navigating away and back clears the detail tabs', async ({ page }) => {
    await page.goto('/board?rhp=workitem:gm-1,epic:gm-2');
    await expect(page.getByTestId('rhp-tab-workitem:gm-1')).toBeVisible();

    // Navigate to a different top-level route. Per-current-route
    // scoping should drop the detail tabs and the ?rhp param.
    // /escalations is the canonical "different route" used elsewhere
    // in the e2e suite — fake-backend smoke covers it.
    await page.goto('/escalations');
    await expect(page.getByTestId('rhp-shell')).toBeVisible();
    await expect(page.getByTestId('rhp-tab-workitem:gm-1')).toHaveCount(0);
    await expect(page.getByTestId('rhp-tab-epic:gm-2')).toHaveCount(0);
    await expect(page).not.toHaveURL(/[?&]rhp=/);

    // Going back to /board does NOT resurrect the tabs (state is
    // route-scoped, not per-history-entry).
    await page.goto('/board');
    await expect(page.getByTestId('rhp-tab-workitem:gm-1')).toHaveCount(0);
  });

  test('closing a tab drops its segment from the URL', async ({ page }) => {
    await page.goto('/board?rhp=workitem:gm-1,epic:gm-2');
    await expect(page.getByTestId('rhp-tab-workitem:gm-1')).toBeVisible();
    await expect(page.getByTestId('rhp-tab-epic:gm-2')).toBeVisible();

    // The close button is `hidden group-hover:flex` in the rail, only
    // becoming visible on parent hover. We dispatch the click event
    // directly rather than simulating hover — this spec asserts the
    // URL-side semantics, not the hover affordance.
    await page
      .getByTestId('rhp-tab-close-epic:gm-2')
      .evaluate((el: HTMLButtonElement) => el.click());

    await expect(page.getByTestId('rhp-tab-epic:gm-2')).toHaveCount(0);
    await expect(page.getByTestId('rhp-tab-workitem:gm-1')).toBeVisible();
    // URL keeps workitem, drops epic.
    await expect(page).toHaveURL(/rhp=workitem(?::|%3A)gm-1(?:&|$)/);
    await expect(page).not.toHaveURL(/epic(?::|%3A)gm-2/);
  });
});
