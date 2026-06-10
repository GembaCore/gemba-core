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
import { workPlaneManifest } from '../../fixtures/capabilitiesPlane';

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

  test('manifest-undeclared adaptor edges surface in the "edges hidden" legend row', async ({
    page,
    workPlane,
    capabilitiesPlane,
  }) => {
    // Manifest declares no extension edges; the WorkItem still
    // carries a beads:dependencies row of an unknown kind. buildGraph
    // bumps droppedUndeclared per dropped row and the legend exposes
    // the count via graph-legend-dropped.
    capabilitiesPlane.setWorkPlane(workPlaneManifest({ adaptor_name: 'beads' }));
    workPlane.seed([
      build.workItem({
        id: 'gm-drop-from',
        custom: {
          'beads:dependencies': [
            { type: 'undeclared_kind', from_bead_id: 'gm-drop-from', to_bead_id: 'gm-drop-to' },
          ],
        },
      }),
      build.workItem({ id: 'gm-drop-to' }),
    ]);

    const graph = new GraphPage(page);
    await graph.goto();

    await expect(page.getByTestId('graph-legend-dropped')).toBeVisible();
    await expect(page.getByTestId('graph-legend-dropped')).toContainText('1 edge');
    // The undeclared edge must NOT be drawn — the manifest gate is
    // the whole point of droppedUndeclared.
    await expect(
      graph.edgeById('gm-drop-from', 'gm-drop-to', 'undeclared_kind')
    ).toHaveCount(0);
  });
});
