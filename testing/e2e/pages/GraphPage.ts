// pages/GraphPage.ts — POM for /graph (gm-5v8v.7).
//
// GraphPage renders a React Flow surface; we drive it via testids and
// the Reactflow-emitted CSS class names where no testid exists. Edge
// styling is asserted by reading the inline `stroke` style off the
// rendered <path>; React Flow gives every edge an `id` of
// `${from}->${to}:${kind}` which we mirror here as the locator key.

import { expect, type Locator, type Page } from '@playwright/test';
import { AppShell } from './AppShell';

export class GraphPage extends AppShell {
  readonly root: Locator;
  readonly canvas: Locator;
  readonly legend: Locator;
  readonly cyclesToggle: Locator;
  readonly criticalToggle: Locator;

  constructor(page: Page) {
    super(page);
    this.root = page.getByTestId('graph-page');
    this.canvas = page.getByTestId('graph-canvas-host');
    this.legend = page.getByTestId('graph-legend');
    this.cyclesToggle = page.getByTestId('graph-toggle-cycles');
    this.criticalToggle = page.getByTestId('graph-toggle-critical');
  }

  override async goto(path: string = '/graph'): Promise<void> {
    await super.goto(path);
    await this.expectShellRendered();
    await expect(this.root).toBeVisible();
  }

  // node locator by id. React Flow wraps each WorkItemNode in its own
  // `.react-flow__node` container; the testid on the inner card is
  // what we anchor on.
  node(id: string): Locator {
    return this.page.getByTestId(`graph-node-${id}`);
  }

  // edge locator by from / to / kind. React Flow assigns the SVG
  // <g class="react-flow__edge" data-id="..."> wrapper an attribute we
  // can target. The id we emit in GraphPage.tsx is `${from}->${to}:${kind}`.
  edgeById(from: string, to: string, kind: string): Locator {
    // React Flow stamps each edge wrapper with
    // `data-testid="rf__edge-${edge.id}"`, where edge.id is the
    // string GraphPage.tsx assembles as `${from}->${to}:${kind}`.
    // We use the testid attribute directly because React Flow does
    // NOT mirror `id` onto a `data-id` attribute on the wrapper.
    const id = `${from}->${to}:${kind}`;
    return this.page.locator(`[data-testid="rf__edge-${id}"]`);
  }

  // edgePath returns the underlying <path> element for stroke / dash
  // assertions. React Flow renders one <path class="react-flow__edge-path">
  // per edge inside the wrapper.
  edgePath(from: string, to: string, kind: string): Locator {
    return this.edgeById(from, to, kind).locator('path.react-flow__edge-path');
  }

  // edgeStroke reads the inline stroke color off the rendered path.
  // GraphPage.tsx puts the per-kind palette on `style.stroke`.
  async edgeStroke(from: string, to: string, kind: string): Promise<string | null> {
    return this.edgePath(from, to, kind).evaluate((el) => (el as SVGPathElement).style.stroke || null);
  }

  // edgeDash reads the strokeDasharray; relates_to + extension edges
  // set this to a non-empty pattern, blocks/parent_child leave it empty.
  async edgeDash(from: string, to: string, kind: string): Promise<string> {
    const v = await this.edgePath(from, to, kind).evaluate(
      (el) => (el as SVGPathElement).style.strokeDasharray || ''
    );
    return v;
  }
}
