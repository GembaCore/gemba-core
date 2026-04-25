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

  test.fixme(
    'click-to-focus zooms the camera onto the selected node',
    async () => {
      // GraphPage's onNodeClick opens the WorkItemDrawer. There's no
      // useReactFlow().setCenter() / fitBounds() call wired to
      // selection. SPA work needed.
    }
  );

  test.fixme(
    'clearing the selection re-fits the camera to the full graph',
    async () => {
      // Same surface — no selection-aware viewport controller in
      // GraphPage today.
    }
  );
});
