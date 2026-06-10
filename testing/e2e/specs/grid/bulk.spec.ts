// specs/grid/bulk.spec.ts — gm-5v8v.6 / gm-5v8v.6.2
//
// Selection model + bulk actions on WorkItemGrid: per-row checkbox,
// shift-click range, header select-all, Cmd+E (bulk edit dialog),
// Cmd+D (bulk defer → state_category:'backlog' PATCH per id), and
// the right-click context menu.

import type { Page } from '@playwright/test';
import { test, expect } from '../../fixtures/server';
import * as build from '../../builders/workitem';
import type { WorkPlaneStore } from '../../fixtures/workplane';

async function gotoGridWithRows(page: Page, workPlane: WorkPlaneStore, count: number) {
  workPlane.seed(
    Array.from({ length: count }, (_, i) =>
      build.workItem({
        id: `wi-${String(i + 1).padStart(2, '0')}`,
        title: `Row ${i + 1}`,
        state_category: 'unstarted',
      }),
    ),
  );
  await page.goto('/board?layout=list&power=1');
  await expect(page.getByTestId('work-item-grid')).toBeVisible();
}

test.describe('WorkItemGrid bulk actions @route', () => {
  test('clicking a row checkbox selects the row', async ({ page, workPlane }) => {
    await gotoGridWithRows(page, workPlane, 5);

    await page.getByTestId('grid-row-checkbox-wi-02').click();

    await expect(page.getByTestId('grid-row-wi-02')).toHaveAttribute(
      'data-selected',
      'true',
    );
    await expect(page.getByTestId('grid-selection-bar')).toBeVisible();
    await expect(page.getByTestId('grid-selection-count')).toHaveText('1 selected');
  });

  test('Shift+click on a checkbox extends the range from the last anchor', async ({
    page,
    workPlane,
  }) => {
    await gotoGridWithRows(page, workPlane, 6);

    await page.getByTestId('grid-row-checkbox-wi-02').click();
    await page.getByTestId('grid-row-checkbox-wi-05').click({ modifiers: ['Shift'] });

    for (const id of ['wi-02', 'wi-03', 'wi-04', 'wi-05']) {
      await expect(page.getByTestId(`grid-row-${id}`)).toHaveAttribute(
        'data-selected',
        'true',
      );
    }
    for (const id of ['wi-01', 'wi-06']) {
      await expect(page.getByTestId(`grid-row-${id}`)).not.toHaveAttribute(
        'data-selected',
        'true',
      );
    }
    await expect(page.getByTestId('grid-selection-count')).toHaveText('4 selected');
  });

  test('header "select all" toggles every visible row', async ({ page, workPlane }) => {
    await gotoGridWithRows(page, workPlane, 3);

    await page.getByTestId('grid-select-all').click();
    await expect(page.getByTestId('grid-selection-count')).toHaveText('3 selected');

    await page.getByTestId('grid-select-all').click();
    await expect(page.getByTestId('grid-selection-bar')).toHaveCount(0);
  });

  test('Cmd+E opens the bulk-edit dialog over the selected rows', async ({
    page,
    workPlane,
  }) => {
    await gotoGridWithRows(page, workPlane, 4);

    await page.getByTestId('grid-row-checkbox-wi-01').click();
    await page.getByTestId('grid-row-checkbox-wi-03').click();

    // Focus the grid container so the chord lands on its keydown
    // listener (the grid uses a tabIndex=0 container scope).
    await page.getByTestId('work-item-grid').focus();
    await page.keyboard.press('Meta+E');

    await expect(page.getByTestId('bulk-edit-dialog')).toBeVisible();
    await expect(page.getByTestId('bulk-edit-count')).toHaveText('2');
  });

  test('Cmd+D defers every selected row via PATCH state_category=backlog', async ({
    page,
    workPlane,
  }) => {
    await gotoGridWithRows(page, workPlane, 3);

    const patches: { id: string; body: unknown }[] = [];
    await page.route('**/api/work-items/*', async (route) => {
      if (route.request().method() === 'PATCH') {
        const url = new URL(route.request().url());
        const id = decodeURIComponent(url.pathname.replace(/^\/api\/work-items\//, ''));
        patches.push({ id, body: route.request().postDataJSON() });
      }
      await route.fallback();
    });

    await page.getByTestId('grid-select-all').click();
    await page.getByTestId('work-item-grid').focus();
    await page.keyboard.press('Meta+D');

    await expect.poll(() => patches.length, { timeout: 3_000 }).toBe(3);
    expect(patches.map((p) => p.id).sort()).toEqual(['wi-01', 'wi-02', 'wi-03']);
    for (const p of patches) {
      expect(p.body).toMatchObject({ state_category: 'backlog' });
    }
  });

  test('right-click on a selection opens the context menu with selection-aware items', async ({
    page,
    workPlane,
  }) => {
    await gotoGridWithRows(page, workPlane, 3);

    await page.getByTestId('grid-row-checkbox-wi-01').click();
    await page.getByTestId('grid-row-checkbox-wi-02').click();

    await page.getByTestId('grid-row-wi-02').click({ button: 'right' });

    await expect(page.getByTestId('grid-context-menu')).toBeVisible();
    await expect(page.getByTestId('grid-context-edit')).toContainText('Edit 2…');
    await expect(page.getByTestId('grid-context-defer')).toContainText('Defer 2');
  });

  test('right-click on an unselected row replaces the selection', async ({
    page,
    workPlane,
  }) => {
    await gotoGridWithRows(page, workPlane, 3);

    await page.getByTestId('grid-row-checkbox-wi-01').click();
    await page.getByTestId('grid-row-wi-03').click({ button: 'right' });

    await expect(page.getByTestId('grid-selection-count')).toHaveText('1 selected');
    await expect(page.getByTestId('grid-row-wi-01')).not.toHaveAttribute(
      'data-selected',
      'true',
    );
    await expect(page.getByTestId('grid-row-wi-03')).toHaveAttribute(
      'data-selected',
      'true',
    );
  });

  test('context-menu Defer fires PATCH for every selected id', async ({
    page,
    workPlane,
  }) => {
    await gotoGridWithRows(page, workPlane, 3);

    const patches: string[] = [];
    await page.route('**/api/work-items/*', async (route) => {
      if (route.request().method() === 'PATCH') {
        const url = new URL(route.request().url());
        patches.push(decodeURIComponent(url.pathname.replace(/^\/api\/work-items\//, '')));
      }
      await route.fallback();
    });

    await page.getByTestId('grid-select-all').click();
    await page.getByTestId('grid-row-wi-02').click({ button: 'right' });
    await page.getByTestId('grid-context-defer').click();

    await expect.poll(() => patches.length, { timeout: 3_000 }).toBe(3);
    await expect(page.getByTestId('grid-context-menu')).toHaveCount(0);
  });

  test('Escape clears the selection', async ({ page, workPlane }) => {
    await gotoGridWithRows(page, workPlane, 3);

    await page.getByTestId('grid-row-checkbox-wi-01').click();
    await page.getByTestId('grid-row-checkbox-wi-02').click();
    await expect(page.getByTestId('grid-selection-count')).toHaveText('2 selected');

    await page.getByTestId('work-item-grid').focus();
    await page.keyboard.press('Escape');

    await expect(page.getByTestId('grid-selection-bar')).toHaveCount(0);
  });
});
