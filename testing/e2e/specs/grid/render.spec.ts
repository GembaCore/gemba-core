// specs/grid/render.spec.ts — gm-5v8v.6
//
// Foundational render coverage for /grid: empty / populated states,
// the count line, search-narrows-rows, and column-visibility toggle.
// Virtualization-specific assertions live in virtualization.spec.ts.

import { test, expect } from '../../fixtures/server';
import { GridPage } from '../../pages/GridPage';
import * as build from '../../builders/workitem';

test.describe('GridPage @route', () => {
  test('empty state renders when WorkPlane has no items', async ({ page, workPlane }) => {
    workPlane.seed([]);
    const grid = new GridPage(page);
    await grid.goto();

    await expect(grid.empty).toBeVisible();
  });

  test('populated grid mounts the rows', async ({ page, workPlane }) => {
    workPlane.seed([
      build.workItem({ id: 'gm-r-1', title: 'First row' }),
      build.workItem({ id: 'gm-r-2', title: 'Second row' }),
      build.workItem({ id: 'gm-r-3', title: 'Third row' }),
    ]);

    const grid = new GridPage(page);
    await grid.goto();

    await expect(grid.grid).toBeVisible();
    await expect(grid.row('gm-r-1')).toBeVisible();
    await expect(grid.row('gm-r-2')).toBeVisible();
    await expect(grid.row('gm-r-3')).toBeVisible();
  });

  test('count line reports total + shown when search filters', async ({ page, workPlane }) => {
    workPlane.seed([
      build.workItem({ id: 'gm-s-1', title: 'apple' }),
      build.workItem({ id: 'gm-s-2', title: 'banana' }),
      build.workItem({ id: 'gm-s-3', title: 'cherry' }),
    ]);

    const grid = new GridPage(page);
    await grid.goto();
    await grid.expectCountText(/3 items/);

    await grid.search.fill('ban');
    // GridPage filters rows client-side; "3 items · 1 shown" appears
    // when the search needle matches a strict subset.
    await grid.expectCountText(/3 items.*1 shown/);
    await expect(grid.row('gm-s-2')).toBeVisible();
    await expect(grid.row('gm-s-1')).toBeHidden();
  });

  test('columns menu lets the operator hide and restore a column', async ({ page, workPlane }) => {
    workPlane.seed([build.workItem({ id: 'gm-c-1', title: 'colcheck' })]);

    const grid = new GridPage(page);
    await grid.goto();
    await grid.openColumnsMenu();

    const labelsCheckbox = page.getByTestId('grid-column-labels');
    await expect(labelsCheckbox).toBeChecked();
    await labelsCheckbox.uncheck();
    await expect(labelsCheckbox).not.toBeChecked();
    // Restoring it brings the column back without reloading.
    await labelsCheckbox.check();
    await expect(labelsCheckbox).toBeChecked();
  });
});
