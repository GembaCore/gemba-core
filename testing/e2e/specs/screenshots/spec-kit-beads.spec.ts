// spec-kit-beads.spec.ts
//
// End-to-end screenshot flow for Spec Kit bootstrap pack -> Gemba/Beads:
//   1. Review Spec Kit source files in the Bootstrap editor.
//   2. Review a staged draft set in Refine -> Bootstrap.
//   3. Open the bootstrap coaching interaction and use a prepared reply.
//   4. Ratify the draft through the UI.
//   5. Capture Refine, Board, Cascade, Graph, and RHP detail output.
//
// Invoked by scripts/generate-spec-kit-screenshots.sh against a real
// gemba serve instance seeded with
// testing/e2e/fixtures/spec-kit/pixel-avatar.

import { test, expect, type Page } from '@playwright/test';
import { mkdirSync } from 'node:fs';
import { join } from 'node:path';

const baseURL = process.env.GEMBA_E2E_BASE_URL;
const outDir =
  process.env.GEMBA_E2E_SCREENSHOT_OUT || join(process.cwd(), '..', '..', 'docs', 'img');

test.describe.configure({ mode: 'serial' });
test.setTimeout(240_000);

test.beforeAll(() => {
  mkdirSync(outDir, { recursive: true });
});

test.beforeEach(async ({ page }) => {
  test.skip(!baseURL, 'set GEMBA_E2E_BASE_URL=http://127.0.0.1:<port> to run this spec');
  await page.goto(`${baseURL}/__init`).catch(() => undefined);
  await page.evaluate(() => {
    localStorage.setItem('gemba-theme', 'dark');
    localStorage.setItem('gemba.rhp.collapsed', 'false');
  });
});

async function screenshot(page: Page, file: string) {
  const path = join(outDir, file);
  await page.screenshot({ path, fullPage: false });
  console.log(`wrote ${path}`);
}

async function waitForWorkItems(page: Page) {
  await expect.poll(
    async () => {
      const res = await page.request.get(`${baseURL}/api/work-items?label=source:spec-kit`);
      if (!res.ok()) return 0;
      const body = await res.json();
      return (body.items ?? []).length;
    },
    { timeout: 90_000 }
  ).toBeGreaterThan(0);
  const res = await page.request.get(`${baseURL}/api/work-items?label=source:spec-kit`);
  const body = await res.json();
  return body.items as Array<{ id: string; kind: string; title: string }>;
}

function pickDetailItem(items: Array<{ id: string; kind: string; title: string }>) {
  const task =
    items.find((item) => item.title.includes('T003')) ??
    items.find((item) => item.kind === 'task') ??
    items[0];
  if (!task) throw new Error('no source:spec-kit item available for detail screenshots');
  return task;
}

test('Spec Kit bootstrap draft to Beads screenshot flow', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });

  await page.goto(`${baseURL}/refine?view=bootstrap`, { waitUntil: 'domcontentloaded' });
  await expect(page.getByTestId('refine-view-bootstrap')).toHaveAttribute('data-active', 'true');
  await expect(page.getByTestId('spec-kit-panel')).toBeVisible();
  await expect(page.getByTestId('spec-kit-feature-005-pixel-avatar')).toBeVisible();
  await expect(page.getByTestId('spec-kit-change-counts')).toContainText('create', {
    timeout: 90_000,
  });
  await expect(page.getByTestId('spec-kit-plan-hash')).toBeVisible({ timeout: 90_000 });
  await expect(page.getByTestId('spec-kit-draft-review')).toContainText('draft items');
  await expect(page.getByTestId('spec-kit-draft-editor')).toBeVisible();
  await page.getByTestId('spec-kit-tab-files').click();
  await expect(page.getByTestId('spec-kit-file-editor')).toBeVisible();
  await expect(page.getByTestId('spec-kit-file-tab-specs/005-pixel-avatar/spec.md')).toBeVisible();
  await expect(page.getByTestId('spec-kit-file-content')).toHaveValue(/Feature Specification/, {
    timeout: 30_000,
  });
  await screenshot(page, 'spec-kit-01-spec-editor-viewer.png');

  await page.getByTestId('spec-kit-tab-draft').click();
  await expect(page.getByTestId('spec-kit-draft-review')).toContainText('draft items');
  await screenshot(page, 'spec-kit-02-bootstrap-draft-review.png');

  await page.getByTestId('spec-kit-open-coach').click();
  await expect(page.getByTestId('interaction-panel')).toBeVisible({ timeout: 30_000 });
  await expect(page.getByTestId('interaction-transcript')).toContainText('Goal: translate bootstrap input');
  await expect(page.getByTestId('interaction-transcript')).toContainText('US1');
  await expect(page.getByTestId('interaction-quick-replies')).toContainText('I want changes');
  await page.getByText('I want changes').click();
  await expect(page.getByTestId('interaction-transcript')).toContainText('batch-shaping', {
    timeout: 30_000,
  });
  await screenshot(page, 'spec-kit-03-bootstrap-coach-review.png');

  await expect(page.getByTestId('spec-kit-sync')).toBeEnabled({ timeout: 30_000 });
  const syncResponse = page.waitForResponse(
    (res) => res.url().includes('/api/spec-kit/features/005-pixel-avatar/sync-to-beads'),
    { timeout: 120_000 }
  );
  await page.getByTestId('spec-kit-sync').click();
  const syncRes = await syncResponse;
  const syncText = await syncRes.text();
  expect(syncRes.ok(), syncText).toBeTruthy();
  const syncResult = JSON.parse(syncText);
  expect(syncResult.task_count).toBeGreaterThan(0);

  const items = await waitForWorkItems(page);
  const detailItem = pickDetailItem(items);

  await page.waitForTimeout(800);
  await screenshot(page, 'spec-kit-04-refine-ratified.png');

  await page.goto(`${baseURL}/board?layout=workitem&show_backlog=1&bead=${encodeURIComponent(detailItem.id)}`, {
    waitUntil: 'domcontentloaded',
  });
  await expect(page.getByTestId('board-workitem')).toBeVisible();
  await expect(page.getByTestId('workitem-detail-id')).toHaveText(detailItem.id);
  await screenshot(page, 'spec-kit-05-board-detail.png');

  await page.goto(`${baseURL}/refine?view=hierarchy&rhp=workitem:${encodeURIComponent(detailItem.id)}`, {
    waitUntil: 'domcontentloaded',
  });
  await expect(page.getByTestId('beads-cascade')).toBeVisible();
  await expect(page.getByTestId('workitem-detail-id')).toHaveText(detailItem.id);
  await screenshot(page, 'spec-kit-06-cascade-detail.png');

  await page.goto(`${baseURL}/graph`, { waitUntil: 'domcontentloaded' });
  await expect(page.getByTestId('graph-page')).toBeVisible();
  await expect(page.getByTestId(`graph-node-${detailItem.id}`)).toBeVisible({ timeout: 20_000 });
  await page.getByTestId(`graph-node-${detailItem.id}`).click();
  await expect(page.getByTestId('workitem-detail-id')).toHaveText(detailItem.id);
  await screenshot(page, 'spec-kit-07-graph-detail.png');
});
