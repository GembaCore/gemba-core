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

  test('renders the tablist with the standard six tabs', async ({ page, workPlane }) => {
    const id = 'gm-test-tabs';
    workPlane.seed([build.workItem({ id })]);

    const drawer = new WorkItemDrawerPO(page);
    await drawer.openByDeepLink(id);

    await expect(drawer.tabs).toBeVisible();
    for (const tab of ['description', 'edges', 'evidence', 'dod', 'sprint', 'activity'] as const) {
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

  test.fixme('opens via double-click on a board card', async () => {
    // Owned by gm-5v8v.5 (board specs) — needs board-card POM and
    // fake WorkPlane seeded into the BoardPage list.
  });

  test.fixme('opens via the "o" hotkey on the focused card', async () => {
    // Owned by gm-5v8v.4 (chrome / hotkeys) — needs the AppHotkeys
    // helper to drive the chord and seed a focused selection.
  });

  test.fixme('back button restores previous bead in the nav stack', async () => {
    // Needs the board to navigate between beads in the same drawer
    // session; lands with gm-5v8v.5 (board specs) where the click
    // path that grows the stack is exercised.
  });
});
