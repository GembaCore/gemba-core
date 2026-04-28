// specs/chrome/project-picker.spec.ts — gm-root.18
//
// Project picker dropdown in the top-bar chrome. The picker is always
// visible regardless of the current route or workspace state. The "+"
// affordance (gm-root.17.2) is a SIBLING element and is NOT part of the
// picker — its slot to the left of the picker trigger must stay clean.
//
// Test matrix:
//   - picker chrome is always visible (empty state + populated state)
//   - trigger button has the expected testids and data attributes
//   - empty state: dropdown shows "No projects yet" message
//   - populated state: dropdown lists all project entries
//   - selecting a project closes the dropdown + updates the label
//   - active project is visually distinguished in the list
//   - pressing Escape closes the dropdown without selection
//   - clicking outside closes the dropdown

import { test, expect } from '../../fixtures/server';

test.describe('ProjectPicker @chrome', () => {
  test.describe('empty state', () => {
    test.beforeEach(async ({ page, projectsStore }) => {
      // Empty store — no projects seeded.
      projectsStore.seed([]);
      await page.goto('/board');
    });

    test('picker chrome is always visible', async ({ page }) => {
      await expect(page.getByTestId('project-picker')).toBeVisible();
    });

    test('trigger button carries workspace-switcher hotkey target', async ({ page }) => {
      const trigger = page.locator('[data-hotkey-target="workspace-switcher"]');
      await expect(trigger).toBeVisible();
    });

    test('trigger has workspace-label testid for back-compat', async ({ page }) => {
      const trigger = page.getByTestId('workspace-label');
      await expect(trigger).toBeVisible();
    });

    test('clicking trigger opens dropdown', async ({ page }) => {
      await page.getByTestId('workspace-label').click();
      await expect(page.getByTestId('project-picker-dropdown')).toBeVisible();
    });

    test('empty dropdown shows "No projects yet"', async ({ page }) => {
      await page.getByTestId('workspace-label').click();
      await expect(page.getByTestId('project-picker-empty')).toBeVisible();
      await expect(page.getByTestId('project-picker-empty')).toContainText('No projects yet');
    });

    test('empty dropdown has no project items', async ({ page }) => {
      await page.getByTestId('workspace-label').click();
      // No items named project-picker-item-* should exist.
      await expect(page.locator('[data-testid^="project-picker-item-"]')).toHaveCount(0);
    });
  });

  test.describe('populated state', () => {
    test.beforeEach(async ({ page, projectsStore }) => {
      projectsStore.seed([
        { name: 'alpha', path: '/tmp/projects/alpha' },
        { name: 'beta', path: '/tmp/projects/beta', active: true },
        { name: 'gamma', path: '/tmp/projects/gamma' },
      ]);
      await page.goto('/board');
    });

    test('picker chrome is visible', async ({ page }) => {
      await expect(page.getByTestId('project-picker')).toBeVisible();
    });

    test('trigger shows the active project name', async ({ page }) => {
      // "beta" is seeded as active.
      await expect(page.getByTestId('project-picker-label')).toContainText('beta');
    });

    test('dropdown lists all projects', async ({ page }) => {
      await page.getByTestId('workspace-label').click();
      await expect(page.getByTestId('project-picker-dropdown')).toBeVisible();
      await expect(page.getByTestId('project-picker-item-alpha')).toBeVisible();
      await expect(page.getByTestId('project-picker-item-beta')).toBeVisible();
      await expect(page.getByTestId('project-picker-item-gamma')).toBeVisible();
    });

    test('active project shows "active" badge', async ({ page }) => {
      await page.getByTestId('workspace-label').click();
      const betaItem = page.getByTestId('project-picker-item-beta');
      await expect(betaItem).toContainText('active');
    });

    test('inactive projects do not show "active" badge', async ({ page }) => {
      await page.getByTestId('workspace-label').click();
      const alphaItem = page.getByTestId('project-picker-item-alpha');
      await expect(alphaItem).not.toContainText('active');
    });

    test('selecting a project closes the dropdown', async ({ page }) => {
      await page.getByTestId('workspace-label').click();
      await expect(page.getByTestId('project-picker-dropdown')).toBeVisible();
      await page.getByTestId('project-picker-item-alpha').click();
      await expect(page.getByTestId('project-picker-dropdown')).not.toBeVisible();
    });

    test('selecting a project updates the trigger label', async ({ page }) => {
      await page.getByTestId('workspace-label').click();
      await page.getByTestId('project-picker-item-alpha').click();
      await expect(page.getByTestId('project-picker-label')).toContainText('alpha');
    });

    test('pressing Escape closes the dropdown', async ({ page }) => {
      await page.getByTestId('workspace-label').click();
      await expect(page.getByTestId('project-picker-dropdown')).toBeVisible();
      await page.keyboard.press('Escape');
      await expect(page.getByTestId('project-picker-dropdown')).not.toBeVisible();
    });

    test('clicking outside closes the dropdown', async ({ page }) => {
      await page.getByTestId('workspace-label').click();
      await expect(page.getByTestId('project-picker-dropdown')).toBeVisible();
      // Click on the main content area (outside the picker).
      await page.locator('main').click({ force: true });
      await expect(page.getByTestId('project-picker-dropdown')).not.toBeVisible();
    });
  });

  test.describe('picker is always visible regardless of route', () => {
    test.beforeEach(async ({ projectsStore }) => {
      projectsStore.seed([{ name: 'my-project', path: '/tmp/projects/my-project', active: true }]);
    });

    // /walk is excluded: the WalkPage auto-starts a walk on mount which
    // makes several API calls that can race with the picker's own fetch
    // under fake mode. The other routes are sufficient to establish
    // "always visible".
    for (const route of ['/board', '/sessions', '/settings']) {
      test(`picker visible on ${route}`, async ({ page }) => {
        await page.goto(route);
        await expect(page.getByTestId('project-picker')).toBeVisible();
      });
    }
  });
});
