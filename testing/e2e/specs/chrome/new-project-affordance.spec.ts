// specs/chrome/new-project-affordance.spec.ts — gm-root.17.2
//
// The "+" New project affordance — a button rendered immediately to the
// LEFT of the ProjectPicker in the top-bar chrome. Always visible
// regardless of the current route or workspace state.
//
// Test matrix:
//   - button is visible across representative routes
//   - button is visible in the empty-state (no projects)
//   - button carries the correct title text "Create new project"
//   - button carries aria-label="Create new project"
//   - clicking the button navigates to /new
//   - the affordance renders immediately before the picker in DOM order

import { test, expect } from '../../fixtures/server';

test.describe('NewProjectAffordance @chrome', () => {
  test.describe('always visible', () => {
    test.beforeEach(async ({ projectsStore }) => {
      // Seed a project so the picker has a populated state; the
      // affordance must be visible in both states (see empty-state suite).
      projectsStore.seed([
        { name: 'my-project', path: '/tmp/projects/my-project', active: true },
      ]);
    });

    for (const route of ['/board', '/sessions', '/settings']) {
      test(`affordance is visible on ${route}`, async ({ page }) => {
        await page.goto(route);
        await expect(page.getByTestId('new-project-affordance')).toBeVisible();
      });
    }
  });

  test.describe('empty state (no projects)', () => {
    test.beforeEach(async ({ page, projectsStore }) => {
      projectsStore.seed([]);
      await page.goto('/board');
    });

    test('affordance is visible even when no projects exist', async ({ page }) => {
      await expect(page.getByTestId('new-project-affordance')).toBeVisible();
    });

    test('picker is also visible alongside affordance in empty state', async ({ page }) => {
      await expect(page.getByTestId('project-picker')).toBeVisible();
    });
  });

  test.describe('attributes', () => {
    test.beforeEach(async ({ page, projectsStore }) => {
      projectsStore.seed([]);
      await page.goto('/board');
    });

    test('has title "Create new project"', async ({ page }) => {
      const btn = page.getByTestId('new-project-affordance');
      await expect(btn).toHaveAttribute('title', 'Create new project');
    });

    test('has aria-label "Create new project"', async ({ page }) => {
      const btn = page.getByTestId('new-project-affordance');
      await expect(btn).toHaveAttribute('aria-label', 'Create new project');
    });

    test('is reachable by role + accessible name', async ({ page }) => {
      await expect(
        page.getByRole('button', { name: 'Create new project' })
      ).toBeVisible();
    });
  });

  test.describe('navigation', () => {
    test.beforeEach(async ({ page, projectsStore }) => {
      projectsStore.seed([]);
      await page.goto('/board');
    });

    test('clicking navigates to /new', async ({ page }) => {
      await page.getByTestId('new-project-affordance').click();
      await expect(page).toHaveURL(/\/new$/);
    });
  });

  test.describe('DOM order', () => {
    test.beforeEach(async ({ page, projectsStore }) => {
      projectsStore.seed([]);
      await page.goto('/board');
    });

    test('affordance renders immediately before the project picker', async ({ page }) => {
      const affordance = page.getByTestId('new-project-affordance');
      const picker = page.getByTestId('project-picker');

      await expect(affordance).toBeVisible();
      await expect(picker).toBeVisible();

      // Verify DOM order: affordance must precede picker.
      const isAffordanceBeforePicker = await page.evaluate(() => {
        const a = document.querySelector('[data-testid="new-project-affordance"]');
        const p = document.querySelector('[data-testid="project-picker"]');
        if (!a || !p) return false;
        // DOCUMENT_POSITION_FOLLOWING (4) set means p comes after a.
        return Boolean(a.compareDocumentPosition(p) & Node.DOCUMENT_POSITION_FOLLOWING);
      });

      expect(isAffordanceBeforePicker).toBe(true);
    });
  });
});
