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
import { workPlaneManifest } from '../../fixtures/capabilitiesPlane';

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

  test('extension edges render dashed/muted under their adaptor heading', async ({
    page,
    workPlane,
    capabilitiesPlane,
  }) => {
    // The bd adaptor's extension surface is custom["beads:dependencies"];
    // buildGraph consults manifest.adaptor_name + edge_extensions to
    // decide whether to draw them. Seed a manifest that declares
    // 'tracks' as an extension edge, then drop a tracks-row on the
    // source bead.
    capabilitiesPlane.setWorkPlane(
      workPlaneManifest({
        adaptor_name: 'beads',
        edge_extensions: [
          { name: 'tracks', directed: true, description: 'A tracks B' },
        ],
      })
    );
    workPlane.seed([
      build.workItem({
        id: 'gm-ext-from',
        custom: {
          'beads:dependencies': [
            { type: 'tracks', from_bead_id: 'gm-ext-from', to_bead_id: 'gm-ext-to' },
          ],
        },
      }),
      build.workItem({ id: 'gm-ext-to' }),
    ]);

    const graph = new GraphPage(page);
    await graph.goto();

    const edge = graph.edgeById('gm-ext-from', 'gm-ext-to', 'tracks');
    await expect(edge).toHaveCount(1);
    // EXTENSION_EDGE_STYLE in GraphPage.tsx is rgb(139, 92, 246) +
    // strokeDasharray '2 4'. Browser normalises hex to rgb on read.
    expect(await graph.edgeStroke('gm-ext-from', 'gm-ext-to', 'tracks')).toBe(
      'rgb(139, 92, 246)'
    );
    expect(await graph.edgeDash('gm-ext-from', 'gm-ext-to', 'tracks')).not.toBe('');
    // Legend grows an "Extension" header listing each declared kind.
    await expect(graph.legend).toContainText('Extension');
    await expect(graph.legend).toContainText('tracks');
  });
});
