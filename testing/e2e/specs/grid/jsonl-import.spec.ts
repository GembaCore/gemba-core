// specs/grid/jsonl-import.spec.ts — gm-5v8v.6
//
// JsonlImportDialog (web/src/components/grid/JsonlImportDialog.tsx).
// Open the dialog, paste valid + malformed JSONL, assert the dry-run
// summary buckets rows correctly, and that commit fans out POST/PATCH.
// Persistence verification (bd actually creates the items) is @deep
// — the fake-mode handler in fixtures/server.ts pretends every POST
// succeeds with a synthetic id, so it can't catch wire-shape drift
// against the real server.

import { test, expect } from '../../fixtures/server';
import { GridPage } from '../../pages/GridPage';
import * as build from '../../builders/workitem';

test.describe('JsonlImportDialog @route', () => {
  test('open / cancel cycle', async ({ page, workPlane }) => {
    workPlane.seed([build.workItem({ id: 'gm-j-existing' })]);

    const grid = new GridPage(page);
    await grid.goto();
    await grid.openImport();

    const dialog = page.getByTestId('jsonl-import-dialog');
    await expect(dialog).toBeVisible();
    await page.getByTestId('jsonl-import-cancel').click();
    await expect(dialog).toBeHidden();
  });

  test('dry-run summary appears as soon as the textarea has parseable JSONL', async ({
    page,
    workPlane,
  }) => {
    workPlane.seed([build.workItem({ id: 'gm-j-1', title: 'before' })]);

    const grid = new GridPage(page);
    await grid.goto();
    await grid.openImport();

    const ta = page.getByTestId('jsonl-import-textarea');
    // Two lines: one create (no id) and one update (existing id).
    await ta.fill('{"title":"new item"}\n{"id":"gm-j-1","status":"in_progress"}');

    const summary = page.getByTestId('jsonl-import-summary');
    await expect(summary).toBeVisible();
    await expect(page.getByTestId('jsonl-import-stat-created')).toContainText('1');
    await expect(page.getByTestId('jsonl-import-stat-updated')).toContainText('1');
  });

  test('malformed lines surface in the skipped bucket with line numbers', async ({
    page,
    workPlane,
  }) => {
    workPlane.seed([]);

    const grid = new GridPage(page);
    await grid.goto();
    await grid.openImport();

    const ta = page.getByTestId('jsonl-import-textarea');
    await ta.fill('{"title":"ok"}\nthis is not json\n{"title":"also ok"}');

    await expect(page.getByTestId('jsonl-import-summary')).toBeVisible();
    await expect(page.getByTestId('jsonl-import-stat-skipped')).toContainText('1');
    // The skipped line carries the original line number so the operator
    // can find it in their source file.
    await expect(page.getByTestId('jsonl-import-skip-2')).toBeVisible();
  });

  test('commit reports total / succeeded against the fake backend', async ({
    page,
    workPlane,
  }) => {
    workPlane.seed([]);

    const grid = new GridPage(page);
    await grid.goto();
    await grid.openImport();

    const ta = page.getByTestId('jsonl-import-textarea');
    await ta.fill('{"title":"row a"}\n{"title":"row b"}');
    await expect(page.getByTestId('jsonl-import-stat-created')).toContainText('2');

    await page.getByTestId('jsonl-import-commit').click();

    // Fake POST handler returns a fresh fake-N id per call; both
    // creates resolve so we expect "2 / 2 committed".
    await expect(page.getByTestId('jsonl-import-result')).toContainText('2 / 2 committed');
  });

  test.fixme(
    '@deep variant verifies bd creates the items',
    async () => {
      // gm-5v8v.2 lands the deep-mode backend; this test would
      // commit the JSONL and then `bd show` each freshly created
      // item to confirm the adaptor materialised them.
    }
  );
});
