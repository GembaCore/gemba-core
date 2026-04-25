// specs/drawers/workitem-drawer.spec.ts — gm-5v8v.8
//
// Covers WorkItemDrawer's "always-on" surface: open via deep-link,
// header (id / copy / close), tab navigation, Escape-closes,
// dispatch button gated by state. The 70KB component carries far
// more (title editor, description editor, edges, evidence, DoD,
// activity, extensions); each gets its own spec under this dir.

import { test, expect } from '../../fixtures/server';
import { WorkItemDrawerPO } from '../../pages/WorkItemDrawer';
import * as build from '../../builders/workitem';

test.describe('WorkItemDrawer @route', () => {
  test('opens via /board?bead=ID', async ({ page, workPlane }) => {
    const id = 'gm-test-wi';
    workPlane.seed([build.workItem({ id, title: 'Hello bead' })]);

    const drawer = new WorkItemDrawerPO(page);
    await drawer.openByDeepLink(id);

    await drawer.expectOpenWith(id);
  });

  test('opens with id containing slashes (workspace-prefixed)', async ({ page, workPlane }) => {
    // The board route uses a `*` segment so canonical bd ids like
    // gemba/gemba/gm-1 round-trip without losing path segments.
    const id = 'gemba/gemba/gm-1';
    workPlane.seed([build.workItem({ id, title: 'Workspace-prefixed' })]);

    const drawer = new WorkItemDrawerPO(page);
    await drawer.openByDeepLink(id);

    await drawer.expectOpenWith(id);
  });

  test('Escape closes the drawer', async ({ page, workPlane }) => {
    const id = 'gm-test-esc';
    workPlane.seed([build.workItem({ id })]);

    const drawer = new WorkItemDrawerPO(page);
    await drawer.openByDeepLink(id);
    await drawer.closeViaEscape();
  });

  test('close button closes the drawer', async ({ page, workPlane }) => {
    const id = 'gm-test-x';
    workPlane.seed([build.workItem({ id })]);

    const drawer = new WorkItemDrawerPO(page);
    await drawer.openByDeepLink(id);
    await drawer.closeViaButton();
  });

  test('renders the tablist with the standard five tabs (Evidence is inline on Summary)', async ({ page, workPlane }) => {
    const id = 'gm-test-tabs';
    workPlane.seed([build.workItem({ id })]);

    const drawer = new WorkItemDrawerPO(page);
    await drawer.openByDeepLink(id);

    await expect(drawer.tabs).toBeVisible();
    // Evidence is folded into the Summary/description tab per
    // ui-spec §5.7 (gm-g9t1) — no standalone evidence tab.
    for (const tab of ['description', 'edges', 'dod', 'sprint', 'activity'] as const) {
      await expect(drawer.tab(tab)).toBeVisible();
    }
  });

  test('tab clicks update aria-selected', async ({ page, workPlane }) => {
    const id = 'gm-test-tab-switch';
    workPlane.seed([build.workItem({ id })]);

    const drawer = new WorkItemDrawerPO(page);
    await drawer.openByDeepLink(id);

    await drawer.selectTab('dod');
    await drawer.selectTab('edges');
    await drawer.selectTab('description');
  });

  test('dispatch button is disabled for terminal beads', async ({ page, workPlane }) => {
    const id = 'gm-test-completed';
    workPlane.seed([
      build.workItem({ id, state_category: 'completed', status: 'closed' }),
    ]);

    const drawer = new WorkItemDrawerPO(page);
    await drawer.openByDeepLink(id);

    await expect(drawer.dispatch).toBeDisabled();
  });

  test('error state renders when bead is not in the WorkPlane', async ({ page, workPlane }) => {
    workPlane.seed([]);

    const drawer = new WorkItemDrawerPO(page);
    await page.goto('/board?bead=gm-missing');

    // Drawer shell renders even on error; the error pane lives inside.
    await expect(drawer.content).toBeVisible();
    await expect(page.getByTestId('work-item-drawer-error')).toBeVisible();
  });

  test('back button restores the previous bead in the nav stack', async ({
    page,
    workPlane,
  }) => {
    // The drawer pushes a new bead onto its internal stack when an
    // edge target inside the Edges tab is clicked. Seed two beads
    // with a "blocks" relationship so the relgroup renders a
    // navigable target.
    const a = build.workItem({
      id: 'gm-stack-a',
      title: 'Source bead',
      relationships: [build.relationship('blocks', 'gm-stack-a', 'gm-stack-b')],
    });
    const b = build.workItem({ id: 'gm-stack-b', title: 'Target bead' });
    workPlane.seed([a, b]);

    const drawer = new WorkItemDrawerPO(page);
    await drawer.openByDeepLink('gm-stack-a');
    await expect(drawer.backButton).toHaveCount(0);

    await drawer.selectTab('edges');
    // The "blocks" relgroup wrapper carries its own testid; the
    // inner navigation buttons are anchored by their bead-id text.
    await page.getByTestId('relgroup-blocks').getByRole('button', { name: 'gm-stack-b' }).click();
    await drawer.expectOpenWith('gm-stack-b');
    await expect(drawer.backButton).toBeVisible();

    await drawer.backButton.click();
    await drawer.expectOpenWith('gm-stack-a');
    await expect(drawer.backButton).toHaveCount(0);
  });

  test('opens via the "o" hotkey on the focused card (gm-fqiw)', async ({
    page,
    workPlane,
  }) => {
    // Workitem-flat view (?view=workitem) renders WorkItemCard — its
    // keydown handler treats `o` as drawer-open (gm-fqiw). Seed a
    // single bead, focus the card, press 'o', assert the drawer
    // opens with the seeded id.
    const id = 'gm-hot-o';
    workPlane.seed([build.workItem({ id, title: 'Hotkey o target' })]);

    const drawer = new WorkItemDrawerPO(page);
    await page.goto('/board?view=workitem');
    // The card carries data-work-item-id and role=button; locate by
    // the id attribute (no card-level testid on the SPA side).
    const card = page.locator(`[data-work-item-id="${id}"]`);
    await expect(card).toBeVisible();
    await card.focus();
    await page.keyboard.press('o');
    await drawer.expectOpenWith(id);
  });
});
