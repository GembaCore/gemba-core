// specs/graph/render.spec.ts — gm-5v8v.7
//
// Foundational render coverage for /graph: empty state, populated
// canvas, one React Flow node per WorkItem, click-to-drawer drill-in.
// Granularity / per-zoom-level aggregation (Epic at zoom out) is on
// the bead but not in the SPA today — tracked as fixme below.

import { test, expect } from '../../fixtures/server';
import { GraphPage } from '../../pages/GraphPage';
import { WorkItemDrawerPO } from '../../pages/WorkItemDrawer';
import * as build from '../../builders/workitem';

test.describe('GraphPage @route', () => {
  test('empty state renders the call-to-action when WorkPlane is empty', async ({
    page,
    workPlane,
  }) => {
    workPlane.seed([]);
    const graph = new GraphPage(page);
    await graph.goto();

    // The empty surface lives inside the canvas host, not the
    // ReactFlow component — assert the helper text is visible and
    // no graph-node-* nodes were mounted.
    await expect(graph.canvas).toContainText('No work items');
    await expect(page.locator('[data-testid^="graph-node-"]')).toHaveCount(0);
  });

  test('one React Flow node renders per seeded WorkItem', async ({ page, workPlane }) => {
    workPlane.seed([
      build.workItem({ id: 'gm-g-1', title: 'Alpha' }),
      build.workItem({ id: 'gm-g-2', title: 'Beta' }),
      build.workItem({ id: 'gm-g-3', title: 'Gamma' }),
    ]);

    const graph = new GraphPage(page);
    await graph.goto();

    await expect(graph.node('gm-g-1')).toBeVisible();
    await expect(graph.node('gm-g-2')).toBeVisible();
    await expect(graph.node('gm-g-3')).toBeVisible();
    // Title and id show up on each node card.
    await expect(graph.node('gm-g-1')).toContainText('Alpha');
    await expect(graph.node('gm-g-1')).toContainText('gm-g-1');
  });

  test('legend renders the three core edge kinds + counts panel', async ({
    page,
    workPlane,
  }) => {
    // A graph with at least one edge is enough to mount the legend.
    workPlane.seed([
      build.workItem({
        id: 'gm-g-l-1',
        relationships: [build.relationship('blocks', 'gm-g-l-1', 'gm-g-l-2')],
      }),
      build.workItem({ id: 'gm-g-l-2' }),
    ]);

    const graph = new GraphPage(page);
    await graph.goto();

    await expect(graph.legend).toBeVisible();
    await expect(graph.legend).toContainText('blocks');
    await expect(graph.legend).toContainText('parent_child');
    await expect(graph.legend).toContainText('relates_to');
  });

  test('clicking a node opens the WorkItemDrawer for that bead', async ({
    page,
    workPlane,
  }) => {
    workPlane.seed([build.workItem({ id: 'gm-g-click', title: 'Click target' })]);

    const graph = new GraphPage(page);
    await graph.goto();

    await graph.node('gm-g-click').click();
    const drawer = new WorkItemDrawerPO(page);
    await drawer.expectOpenWith('gm-g-click');
  });

  test('granularity toggle rolls items up to enclosing Epics (gm-ndb6)', async ({
    page,
    workPlane,
  }) => {
    // Two Epics, each with one task; cross-Epic blocks edge between
    // their tasks. The bead's wording predicts a zoom-driven switch;
    // shipping pivots on an explicit toggle button instead so the
    // operator stays in control of when aggregation happens.
    workPlane.seed([
      build.epic({ id: 'gm-ep-1', title: 'Alpha epic' }),
      build.epic({ id: 'gm-ep-2', title: 'Beta epic' }),
      build.workItem({
        id: 'gm-ep-1-task',
        title: 'Alpha task',
        relationships: [
          build.parentChild('gm-ep-1', 'gm-ep-1-task'),
          build.relationship('blocks', 'gm-ep-1-task', 'gm-ep-2-task'),
        ],
      }),
      build.workItem({
        id: 'gm-ep-2-task',
        title: 'Beta task',
        relationships: [build.parentChild('gm-ep-2', 'gm-ep-2-task')],
      }),
    ]);

    const graph = new GraphPage(page);
    await graph.goto();

    // Items mode (default): every WorkItem renders.
    await expect(graph.canvas).toHaveAttribute('data-granularity', 'items');
    await expect(graph.node('gm-ep-1')).toBeVisible();
    await expect(graph.node('gm-ep-1-task')).toBeVisible();
    await expect(graph.node('gm-ep-2-task')).toBeVisible();

    // Flip to Epics. Granularity attribute reflects + only Epic
    // nodes render. The blocks edge between the tasks aggregates
    // to an Epic→Epic edge.
    await page.getByTestId('graph-toggle-granularity').click();
    await expect(graph.canvas).toHaveAttribute('data-granularity', 'epics');
    await expect(graph.node('gm-ep-1')).toBeVisible();
    await expect(graph.node('gm-ep-2')).toBeVisible();
    await expect(graph.node('gm-ep-1-task')).toHaveCount(0);
    await expect(graph.node('gm-ep-2-task')).toHaveCount(0);
    await expect(graph.edgeById('gm-ep-1', 'gm-ep-2', 'blocks')).toHaveCount(1);

    // Flip back — items reappear.
    await page.getByTestId('graph-toggle-granularity').click();
    await expect(graph.canvas).toHaveAttribute('data-granularity', 'items');
    await expect(graph.node('gm-ep-1-task')).toBeVisible();
  });
});
