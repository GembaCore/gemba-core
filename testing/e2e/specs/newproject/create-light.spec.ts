// specs/newproject/create-light.spec.ts — gm-root.17.13
//
// /new is now a lightweight project-creation form (was the
// conversational page). It POSTs to /api/v1/newproject/create and on
// success switches the active workspace and navigates to /board.
// No LLM, no Onboarder.

import { test, expect } from '../../fixtures/server';

test.describe('/new (lightweight create — gm-root.17.13) @route', () => {
  test('renders the form with name + description + create', async ({ page }) => {
    await page.goto('/new');
    await expect(page.getByTestId('newproject-page')).toBeVisible();
    await expect(page.getByTestId('newproject-name')).toBeVisible();
    await expect(page.getByTestId('newproject-description')).toBeVisible();
    await expect(page.getByTestId('newproject-create')).toBeVisible();
  });

  test('Create is disabled until a name is typed', async ({ page }) => {
    await page.goto('/new');
    const submit = page.getByTestId('newproject-create');
    await expect(submit).toBeDisabled();

    await page.getByTestId('newproject-name').fill('demo-light');
    await expect(submit).toBeEnabled();
  });

  test('submits to /create and lands on /board', async ({ page }) => {
    let createBody: unknown = null;
    await page.route('**/api/v1/newproject/create', async (route) => {
      createBody = JSON.parse(route.request().postData() ?? '{}');
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          project_path: '/tmp/demo-light',
          project_name: 'demo-light',
          milestone_count: 0,
          epic_count: 0,
        }),
      });
    });

    await page.goto('/new');
    await page.getByTestId('newproject-name').fill('demo-light');
    await page.getByTestId('newproject-description').fill('first try');
    await page.getByTestId('newproject-create').click();

    await page.waitForURL(/\/board$/);
    expect(createBody).toEqual({ project_name: 'demo-light', description: 'first try' });
  });

  test('renders a readable error when /create fails', async ({ page }) => {
    await page.route('**/api/v1/newproject/create', async (route) => {
      await route.fulfill({
        status: 409,
        contentType: 'application/json',
        body: JSON.stringify({
          error: 'dir_exists',
          message: 'a project named "dupe" already exists',
        }),
      });
    });

    await page.goto('/new');
    await page.getByTestId('newproject-name').fill('dupe');
    await page.getByTestId('newproject-create').click();

    await expect(page.getByTestId('newproject-error')).toBeVisible();
    // No navigation on failure.
    expect(new URL(page.url()).pathname).toBe('/new');
  });
});
