// specs/graph/workitem-graph.spec.ts — gm-5v8v.7
//
// The bead names "dependency graph from gm-e12.16". gm-e12.16 IS
// GraphPage — render.spec.ts owns the foundational coverage and
// edges.spec.ts owns the per-kind styling. This file owns the
// dependency-shape invariants the analyser provides:
//
//   - structural edges (blocks + parent_child) drive the layout
//   - the "n edges hidden" line surfaces when an adaptor declares
//     extension edges that don't appear in the manifest's
//     edge_extensions roster
//
// Cross-references the same edges.spec.ts fixme: stamping the
// extension-edge surface needs the fake-capabilities fixture to
// grow a manifest seeder.

import { test, expect } from '../../fixtures/server';
import { GraphPage } from '../../pages/GraphPage';
import * as build from '../../builders/workitem';

test.describe('GraphPage dependency invariants @route', () => {
  test('blocks chain renders as ordered edges between adjacent beads', async ({
    page,
    workPlane,
  }) => {
    workPlane.seed([
      build.workItem({
        id: 'gm-dep-1',
        relationships: [build.relationship('blocks', 'gm-dep-1', 'gm-dep-2')],
      }),
      build.workItem({
        id: 'gm-dep-2',
        relationships: [build.relationship('blocks', 'gm-dep-2', 'gm-dep-3')],
      }),
      build.workItem({ id: 'gm-dep-3' }),
    ]);

    const graph = new GraphPage(page);
    await graph.goto();

    // SVG group visibility is bounding-box derived; use count to
    // assert presence.
    await expect(graph.edgeById('gm-dep-1', 'gm-dep-2', 'blocks')).toHaveCount(1);
    await expect(graph.edgeById('gm-dep-2', 'gm-dep-3', 'blocks')).toHaveCount(1);
    // No back-edge — the adaptor only emits the forward direction.
    await expect(graph.edgeById('gm-dep-3', 'gm-dep-2', 'blocks')).toHaveCount(0);
  });

  test('parent_child edges contribute to the structural layout', async ({
    page,
    workPlane,
  }) => {
    workPlane.seed([
      build.workItem({ id: 'gm-pc-parent' }),
      build.workItem({
        id: 'gm-pc-child-1',
        relationships: [build.parentChild('gm-pc-parent', 'gm-pc-child-1')],
      }),
      build.workItem({
        id: 'gm-pc-child-2',
        relationships: [build.parentChild('gm-pc-parent', 'gm-pc-child-2')],
      }),
    ]);

    const graph = new GraphPage(page);
    await graph.goto();

    await expect(graph.edgeById('gm-pc-parent', 'gm-pc-child-1', 'parent_child')).toHaveCount(1);
    await expect(graph.edgeById('gm-pc-parent', 'gm-pc-child-2', 'parent_child')).toHaveCount(1);
  });

  test.fixme(
    'manifest-undeclared adaptor edges surface in the "edges hidden" legend row',
    async () => {
      // Needs a custom["beads:dependencies"]-style payload on the
      // WorkItem plus a manifest with edge_extensions that doesn't
      // declare the same kind. buildGraph() drops them and bumps
      // droppedUndeclared, which the legend's
      // graph-legend-dropped row reflects. Lifts when the
      // capabilities fixture grows a manifest seeder.
    }
  );
});
