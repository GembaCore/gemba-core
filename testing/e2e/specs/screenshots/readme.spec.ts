// readme.spec.ts (gm-57p6)
//
// Standalone screenshot generator — NOT part of the regular test
// matrix. Invoked by scripts/generate-readme-screenshots.sh against a
// gemba serve instance pointed at the examples/my-project sample rig.
//
// What it does:
//   - sets localStorage['gemba-theme'] = 'dark' before navigation so
//     the SPA boots in dark mode (no UI toggle needed)
//   - visits /board, /graph, /walk
//   - captures a viewport screenshot of each, written to
//     $GEMBA_E2E_SCREENSHOT_OUT/screenshot-{board,graph,walk}.png
//
// The spec is a no-op when GEMBA_E2E_BASE_URL is unset, so it doesn't
// pollute the default suite.

import { test, expect } from '@playwright/test';
import { mkdirSync } from 'node:fs';
import { join } from 'node:path';

const baseURL = process.env.GEMBA_E2E_BASE_URL;
const outDir =
  process.env.GEMBA_E2E_SCREENSHOT_OUT || join(process.cwd(), '..', '..', 'docs', 'img');

test.describe.configure({ mode: 'serial' });

test.beforeAll(() => {
  mkdirSync(outDir, { recursive: true });
});

test.beforeEach(async ({ page }) => {
  test.skip(!baseURL, 'set GEMBA_E2E_BASE_URL=http://127.0.0.1:<port> to run this spec');
  // Set dark mode + collapse any first-run banners BEFORE the SPA
  // first paints. Goto a 404 first to get a same-origin context for
  // localStorage, then nav to the real page.
  await page.goto(`${baseURL}/__init`).catch(() => undefined);
  await page.evaluate(() => {
    localStorage.setItem('gemba-theme', 'dark');
  });
});

async function captureSurface(page: import('@playwright/test').Page, route: string, file: string) {
  await page.setViewportSize({ width: 1440, height: 900 });
  // Gemba's SSE endpoint keeps the network "active" forever — using
  // `networkidle` waits past the test timeout. `domcontentloaded`
  // covers HTML+CSS+JS readiness; the explicit settle below covers
  // React Query / layout reflow.
  await page.goto(`${baseURL}${route}`, { waitUntil: 'domcontentloaded' });
  await page.waitForTimeout(2500);
  const path = join(outDir, file);
  await page.screenshot({ path, fullPage: false });
  console.log(`wrote ${path}`);
}

test('board screenshot', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto(`${baseURL}/board`, { waitUntil: 'domcontentloaded' });
  // Default board view is "Epic" — for the screenshot we want the
  // Item view so the BACKLOG / NEXT UP / IN PROGRESS / DONE columns
  // populate with concrete cards (more representative for a reader).
  await page.waitForTimeout(1500);
  const itemToggle = page.getByRole('button', { name: /^Item$/ });
  if ((await itemToggle.count()) > 0) {
    await itemToggle.click().catch(() => undefined);
  }
  await page.waitForTimeout(1500);
  await page.screenshot({
    path: join(outDir, 'screenshot-board.png'),
    fullPage: false,
  });
  console.log('wrote screenshot-board.png');
  await expect(page.locator('body')).toBeVisible();
});

test('graph screenshot', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto(`${baseURL}/graph`, { waitUntil: 'domcontentloaded' });
  // Default granularity is 'items' — for a 30+ item sample that
  // produces a wide layered layout that fits-to-view as
  // microscopic dots. Toggle to 'epics' (gm-ndb6) so the ~6 epic
  // nodes render at a more readable size.
  await page.waitForTimeout(1500);
  const granToggle = page.getByTestId('graph-toggle-granularity');
  if ((await granToggle.count()) > 0) {
    await granToggle.click().catch(() => undefined);
  }
  await page.waitForTimeout(1500);
  // Even at Epic granularity, the layered layout's column widths
  // produce a wide fit. Click the React Flow Controls' zoom-in
  // button to land at a label-readable scale. The Controls widget
  // is rendered with .react-flow__controls and its zoom-in button
  // carries .react-flow__controls-zoomin.
  const zoomIn = page.locator('button.react-flow__controls-zoomin');
  if ((await zoomIn.count()) > 0) {
    for (let i = 0; i < 5; i++) {
      await zoomIn.click({ timeout: 1000 }).catch(() => undefined);
      await page.waitForTimeout(120);
    }
  }
  await page.waitForTimeout(500);
  await page.screenshot({
    path: join(outDir, 'screenshot-graph.png'),
    fullPage: false,
  });
  console.log('wrote screenshot-graph.png');
  await expect(page.locator('[data-testid="graph-page"]')).toBeVisible();
});

test('walk screenshot', async ({ page }) => {
  await captureSurface(page, '/walk', 'screenshot-walk.png');
  await expect(page.locator('[data-testid="walk-page"]')).toBeVisible();
});
