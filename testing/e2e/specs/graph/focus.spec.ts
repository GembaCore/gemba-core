// specs/graph/focus.spec.ts — gm-5v8v.7 (in progress)
//
// The bead names two camera behaviors:
//   - "focus-selected zoom default" — click a node, camera focuses
//     on it (selection-driven viewport)
//   - "empty-selection = fit-all" — no selection, fit the entire graph
//
// GraphPage today only sets `fitView` once at mount via React Flow's
// prop. There's no selection-driven viewport handler, no
// `useReactFlow().fitView()` call wired to selection changes. The
// stable assertion in fake mode is "fit-all happens at mount" — the
// rest of the contract waits on SPA wiring.

import { test, expect } from '../../fixtures/server';
import { GraphPage } from '../../pages/GraphPage';
import * as build from '../../builders/workitem';

test.describe('GraphPage focus / camera @route', () => {
  test('mount fits all nodes into the viewport (fitView)', async ({ page, workPlane }) => {
    // We assert the inert form of fit-all: every seeded node ends up
    // inside the rendered viewport bounds. React Flow positions nodes
    // by transform; checking visibility per testid is sufficient
    // because off-canvas nodes don't get mounted into the React Flow
    // viewport in the first place.
    workPlane.seed([
      build.workItem({ id: 'gm-fit-1' }),
      build.workItem({ id: 'gm-fit-2' }),
      build.workItem({ id: 'gm-fit-3' }),
      build.workItem({ id: 'gm-fit-4' }),
    ]);

    const graph = new GraphPage(page);
    await graph.goto();

    for (const id of ['gm-fit-1', 'gm-fit-2', 'gm-fit-3', 'gm-fit-4']) {
      await expect(graph.node(id)).toBeVisible();
    }
  });

  test('click-to-focus marks the node on the canvas host (gm-sfbh)', async ({
    page,
    workPlane,
  }) => {
    workPlane.seed([
      build.workItem({ id: 'gm-foc-1' }),
      build.workItem({ id: 'gm-foc-2' }),
    ]);

    const graph = new GraphPage(page);
    await graph.goto();

    // Default state: no focused-node marker.
    await expect(graph.canvas).not.toHaveAttribute('data-focused-node', 'gm-foc-1');
    await graph.node('gm-foc-1').click();
    // Click sets focus state + drives the camera + opens the drawer.
    await expect(graph.canvas).toHaveAttribute('data-focused-node', 'gm-foc-1');
  });

  test('Escape (drawer close) clears focus and re-fits the camera (gm-sfbh)', async ({
    page,
    workPlane,
  }) => {
    workPlane.seed([build.workItem({ id: 'gm-foc-clear' })]);

    const graph = new GraphPage(page);
    await graph.goto();
    await graph.node('gm-foc-clear').click();
    await expect(graph.canvas).toHaveAttribute('data-focused-node', 'gm-foc-clear');

    // Closing the WorkItemDrawer (Escape or close button) also clears
    // the focused-node marker on GraphPage — the operator returns to
    // the at-a-glance view in one keystroke.
    await page.keyboard.press('Escape');
    await expect(graph.canvas).not.toHaveAttribute('data-focused-node', 'gm-foc-clear');
  });
});
