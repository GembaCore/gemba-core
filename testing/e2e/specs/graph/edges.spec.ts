// specs/graph/edges.spec.ts — gm-5v8v.7
//
// Edge styling per ui-spec §5.10:
//   blocks       → red
//   parent_child → navy/sky
//   relates_to   → tan/neutral, dashed
//   extension    → muted purple, dashed
//
// We assert against the inline `style.stroke` GraphPage.tsx writes
// onto each React Flow path. This couples the test to the palette
// — when the design system swaps colors, this spec's expectations
// move with the EDGE_STYLE table in the SPA. That coupling is the
// point: visual identity of the core kinds is part of the contract.

import { test, expect } from '../../fixtures/server';
import { GraphPage } from '../../pages/GraphPage';
import * as build from '../../builders/workitem';

// Mirror EDGE_STYLE from web/src/pages/GraphPage.tsx — but expressed
// in the rgb() form the browser normalises to when reading
// `style.stroke` off a rendered element. When the SPA table changes,
// update both — the spec catches the drift.
//   #dc2626 → rgb(220, 38, 38)
//   #0284c7 → rgb(2, 132, 199)
//   #737373 → rgb(115, 115, 115)
const STROKE_BLOCKS = 'rgb(220, 38, 38)';
const STROKE_PARENT = 'rgb(2, 132, 199)';
const STROKE_RELATES = 'rgb(115, 115, 115)';

test.describe('GraphPage edge styling @route', () => {
  test('blocks edges render with the red palette', async ({ page, workPlane }) => {
    workPlane.seed([
      build.workItem({
        id: 'gm-eg-blocker',
        relationships: [build.relationship('blocks', 'gm-eg-blocker', 'gm-eg-blocked')],
      }),
      build.workItem({ id: 'gm-eg-blocked' }),
    ]);

    const graph = new GraphPage(page);
    await graph.goto();

    // SVG <g> elements often report `hidden` to Playwright's
    // visibility check because their bounding box is computed from
    // children — assert presence by count, not visibility.
    await expect(graph.edgeById('gm-eg-blocker', 'gm-eg-blocked', 'blocks')).toHaveCount(1);
    expect(await graph.edgeStroke('gm-eg-blocker', 'gm-eg-blocked', 'blocks')).toBe(
      STROKE_BLOCKS
    );
    // blocks is solid (no dasharray)
    expect(await graph.edgeDash('gm-eg-blocker', 'gm-eg-blocked', 'blocks')).toBe('');
  });

  test('parent_child edges render with the sky palette', async ({ page, workPlane }) => {
    workPlane.seed([
      build.workItem({
        id: 'gm-eg-child',
        relationships: [build.parentChild('gm-eg-parent', 'gm-eg-child')],
      }),
      build.workItem({ id: 'gm-eg-parent' }),
    ]);

    const graph = new GraphPage(page);
    await graph.goto();

    expect(
      await graph.edgeStroke('gm-eg-parent', 'gm-eg-child', 'parent_child')
    ).toBe(STROKE_PARENT);
    // parent_child is solid
    expect(await graph.edgeDash('gm-eg-parent', 'gm-eg-child', 'parent_child')).toBe('');
  });

  test('relates_to edges render dashed in the neutral palette', async ({
    page,
    workPlane,
  }) => {
    workPlane.seed([
      build.workItem({
        id: 'gm-eg-a',
        relationships: [build.relationship('relates_to', 'gm-eg-a', 'gm-eg-b')],
      }),
      build.workItem({ id: 'gm-eg-b' }),
    ]);

    const graph = new GraphPage(page);
    await graph.goto();

    expect(
      await graph.edgeStroke('gm-eg-a', 'gm-eg-b', 'relates_to')
    ).toBe(STROKE_RELATES);
    // relates_to ships with strokeDasharray='4 3'
    expect(await graph.edgeDash('gm-eg-a', 'gm-eg-b', 'relates_to')).not.toBe('');
  });

  test.fixme(
    'extension edges render dashed/muted under their adaptor heading',
    async () => {
      // Needs a WorkPlane manifest that declares an edge_extensions
      // entry plus a WorkItem custom map carrying that key. Lifts when
      // the fake-capabilities fixture grows a manifest seeder
      // (parallel work to the OrchestrationPlane fixture extension
      // tracked in agent-drawer.spec.ts).
    }
  );
});
