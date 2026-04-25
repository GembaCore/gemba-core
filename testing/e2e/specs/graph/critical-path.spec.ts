// specs/graph/critical-path.spec.ts — gm-5v8v.7
//
// Critical-path toggle (top-right of the graph header). When active:
//   - the toggle button carries data-active="true"
//   - nodes on the path mark themselves data-on-critical-path
//   - edges on the path get the amber highlight (#f59e0b)
//   - the legend's "critical chain: N hops" line appears
//
// Cycles toggle is on the same row and follows the same pattern; we
// cover both since the bead lists "render / critical path / edge
// styling" and the cycles surface is the natural sibling.
//
// On-hover focus highlighting through the focused node is named in
// the bead but not implemented in GraphPage today — fixme below.

import { test, expect } from '../../fixtures/server';
import { GraphPage } from '../../pages/GraphPage';
import * as build from '../../builders/workitem';

test.describe('GraphPage critical-path mode @route', () => {
  test('toggle starts off; clicking sets data-active=true', async ({ page, workPlane }) => {
    workPlane.seed([build.workItem({ id: 'gm-cp-1' })]);

    const graph = new GraphPage(page);
    await graph.goto();

    // Default state: no data-active attribute (the SPA writes it as
    // `data-active={active || undefined}`, so an inactive button has
    // the attribute absent rather than set to "false").
    await expect(graph.criticalToggle).not.toHaveAttribute('data-active', 'true');

    await graph.criticalToggle.click();
    await expect(graph.criticalToggle).toHaveAttribute('data-active', 'true');
  });

  test('critical-path toggle highlights path nodes when a chain exists', async ({
    page,
    workPlane,
  }) => {
    // Three-bead block chain: A → B → C. The critical-path analyser
    // (web/src/components/graph/graphAnalysis.ts) walks the structural
    // edge set and the longest chain end-to-end is the critical path.
    workPlane.seed([
      build.workItem({
        id: 'gm-cp-a',
        relationships: [build.relationship('blocks', 'gm-cp-a', 'gm-cp-b')],
      }),
      build.workItem({
        id: 'gm-cp-b',
        relationships: [build.relationship('blocks', 'gm-cp-b', 'gm-cp-c')],
      }),
      build.workItem({ id: 'gm-cp-c' }),
    ]);

    const graph = new GraphPage(page);
    await graph.goto();

    // No path highlights when toggle is off.
    await expect(graph.node('gm-cp-a')).not.toHaveAttribute('data-on-critical-path', 'true');

    await graph.criticalToggle.click();

    // Each node on the chain carries the data-on-critical-path flag.
    await expect(graph.node('gm-cp-a')).toHaveAttribute('data-on-critical-path', 'true');
    await expect(graph.node('gm-cp-b')).toHaveAttribute('data-on-critical-path', 'true');
    await expect(graph.node('gm-cp-c')).toHaveAttribute('data-on-critical-path', 'true');
    // Legend surfaces the chain length when the mode is on.
    await expect(page.getByTestId('graph-legend-critical')).toBeVisible();
  });

  test('cycles toggle is on by default and surfaces the cycle row when one exists', async ({
    page,
    workPlane,
  }) => {
    // Mutual blocks creates a length-2 cycle. detectCycles() walks the
    // SCCs and exposes the count via legend text.
    workPlane.seed([
      build.workItem({
        id: 'gm-cy-a',
        relationships: [build.relationship('blocks', 'gm-cy-a', 'gm-cy-b')],
      }),
      build.workItem({
        id: 'gm-cy-b',
        relationships: [build.relationship('blocks', 'gm-cy-b', 'gm-cy-a')],
      }),
    ]);

    const graph = new GraphPage(page);
    await graph.goto();

    await expect(graph.cyclesToggle).toHaveAttribute('data-active', 'true');
    // With cycles ON and a cycle present, both nodes carry the
    // in-cycle marker.
    await expect(graph.node('gm-cy-a')).toHaveAttribute('data-in-cycle', 'true');
    await expect(graph.node('gm-cy-b')).toHaveAttribute('data-in-cycle', 'true');
    await expect(page.getByTestId('graph-legend-cycles')).toBeVisible();
  });

  test.fixme('on-hover highlights propagate through the focused node', async () => {
    // The bead names "on-hover highlights through focused node".
    // GraphPage today does not wire onNodeMouseEnter / focus
    // selectors — the canvas only reacts to onNodeClick (drawer).
    // SPA work needed before the fixme can lift.
  });
});
