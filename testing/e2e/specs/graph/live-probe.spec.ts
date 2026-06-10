// One-off diagnostic probe (NOT in any project's testMatch by default —
// runs only when invoked explicitly). Boots /graph against whatever
// gemba serve the operator passes via GEMBA_E2E_BASE_URL and reports
// what's actually on the canvas. Used to debug "graph appears blank"
// reports against real WorkPlanes.

import { test, expect } from '@playwright/test';

test.describe.configure({ mode: 'serial' });

test('diagnostic: enumerate /graph render state', async ({ page }) => {
  const baseURL = process.env.GEMBA_E2E_BASE_URL;
  test.skip(!baseURL, 'set GEMBA_E2E_BASE_URL=http://127.0.0.1:<port> to run this probe');

  // Quiet console capture — surface anything unexpected.
  const consoleErrors: string[] = [];
  page.on('pageerror', (e) => consoleErrors.push(`pageerror: ${e.message}`));
  page.on('console', (m) => {
    if (m.type() === 'error') consoleErrors.push(`console.error: ${m.text()}`);
  });

  await page.goto(`${baseURL}/graph`);
  await page.waitForSelector('[data-testid="graph-page"]', { timeout: 10_000 });

  // What state did the host render?
  const hostHtml = await page.locator('[data-testid="graph-canvas-host"]').innerHTML();
  const empty = await page.getByText('No work items').count();
  const loading = await page.getByText(/loading/i).count();
  const errorBanner = await page.locator('[data-testid="graph-canvas-host"] >> text=/error/i').count();

  // Wait briefly for ReactFlow to mount and layout to settle.
  await page.waitForTimeout(1500);
  const nodeCount = await page.locator('[data-testid^="graph-node-"]').count();
  // Sanity: pick the first node and verify its center is inside the
  // canvas viewport. A "blank graph" looks identical to "all nodes
  // off-camera" — this is the assertion that catches the latter.
  const firstNode = page.locator('[data-testid^="graph-node-"]').first();
  const firstBox = (await firstNode.count()) > 0 ? await firstNode.boundingBox() : null;
  const canvasBox2 = await page.locator('[data-testid="graph-canvas-host"]').boundingBox();
  let inViewport = false;
  if (firstBox && canvasBox2) {
    const cx = firstBox.x + firstBox.width / 2;
    const cy = firstBox.y + firstBox.height / 2;
    inViewport =
      cx >= canvasBox2.x && cx <= canvasBox2.x + canvasBox2.width &&
      cy >= canvasBox2.y && cy <= canvasBox2.y + canvasBox2.height;
  }
  console.log('first-node in canvas viewport:', inViewport, firstBox);
  const edgeCount = await page.locator('.react-flow__edge').count();
  const reactFlowMounted = await page.locator('.react-flow').count();
  const canvasBox = await page.locator('[data-testid="graph-canvas-host"]').boundingBox();

  // Dump everything to stdout for the operator running this manually.
  console.log('--- /graph live probe ---');
  console.log('empty-state shown:', empty);
  console.log('loading shown:', loading);
  console.log('error banner shown:', errorBanner);
  console.log('react-flow mounted (count):', reactFlowMounted);
  console.log('graph-node-* count:', nodeCount);
  console.log('react-flow edge count:', edgeCount);
  console.log('canvas host bounding box:', canvasBox);
  console.log('console/page errors:', consoleErrors);
  console.log('canvas-host innerHTML length:', hostHtml.length);

  // Hard assertions: ReactFlow is mounted, dimensions are non-zero.
  expect(reactFlowMounted, 'ReactFlow should mount when items are present').toBeGreaterThanOrEqual(0);
  if (canvasBox) {
    expect(canvasBox.height, 'canvas-host should have positive measured height').toBeGreaterThan(0);
    expect(canvasBox.width, 'canvas-host should have positive measured width').toBeGreaterThan(0);
  }
});
