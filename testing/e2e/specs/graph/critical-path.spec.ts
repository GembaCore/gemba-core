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
  // The hover spec exercises React Flow's mouseenter pipeline through
  // Playwright's hover() simulator. Under heavy parallel-worker load
  // the synthetic event occasionally drops before React renders the
  // hover-related diff. Single retry papers over the race; the
  // logic is exercised every run, just sometimes after a re-attempt.
  test.describe.configure({ retries: 1 });

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

  test('hovering a node lights up the node + its one-hop neighbours (gm-qdqu)', async ({
    page,
    workPlane,
  }) => {
    workPlane.seed([
      build.workItem({
        id: 'gm-hov-a',
        relationships: [build.relationship('blocks', 'gm-hov-a', 'gm-hov-b')],
      }),
      build.workItem({
        id: 'gm-hov-b',
        relationships: [build.relationship('blocks', 'gm-hov-b', 'gm-hov-c')],
      }),
      build.workItem({ id: 'gm-hov-c' }),
      // Unrelated node — must NOT light up.
      build.workItem({ id: 'gm-hov-d' }),
    ]);

    const graph = new GraphPage(page);
    await graph.goto();

    // Default state: no hover-related attribute on any node.
    await expect(graph.node('gm-hov-a')).not.toHaveAttribute('data-hover-related', 'true');

    // React Flow listens via onMouseEnter on the wrapping
    // .react-flow__node element. CI has a persistent flake here
    // where hover() returns before React Flow's onNodeMouseEnter
    // handler runs — the assertion's built-in polling never sees
    // data-hover-related. Wrap the hover + the host-node assertion
    // in expect.poll so we re-hover and re-check until the state
    // propagates. force:true keeps Playwright from bailing when the
    // inner card transiently overlaps the React Flow controls.
    //
    // Loaded CI runners can also drop the hover state between the
    // host-node poll above and the per-neighbour assertions below
    // (the pointer drifts onto a sibling React Flow control between
    // events). Extend the poll to cover the entire highlighted set
    // — re-hover until host + both neighbours light up at the same
    // time. The unrelated-node assertion stays a plain check because
    // it's negative and converges fast.
    // Snapshot the entire hover-related state in one poll so the
    // hover gesture and every per-id check happen inside the same
    // re-hover loop. Loaded CI runners can let the pointer drift
    // between the host-node poll and per-neighbour assertions if
    // they're separate steps; consolidating them here means the
    // poll only succeeds once host + both neighbours are lit AND
    // the unrelated node stays dark, all observed at the same time.
    const reactFlowNode = page.locator('.react-flow__node[data-id="gm-hov-b"]');
    await expect
      .poll(
        async () => {
          await reactFlowNode.hover({ force: true });
          const [a, b, c, d] = await Promise.all([
            graph.node('gm-hov-a').getAttribute('data-hover-related'),
            graph.node('gm-hov-b').getAttribute('data-hover-related'),
            graph.node('gm-hov-c').getAttribute('data-hover-related'),
            graph.node('gm-hov-d').getAttribute('data-hover-related'),
          ]);
          return { a, b, c, d };
        },
        { timeout: 10_000, intervals: [200, 500, 1000, 2000] }
      )
      .toEqual({ a: 'true', b: 'true', c: 'true', d: null });

    // Move pointer well off-canvas so onNodeMouseLeave fires.
    await page.mouse.move(0, 0);
    await expect(graph.node('gm-hov-b')).not.toHaveAttribute('data-hover-related', 'true');
  });
});
