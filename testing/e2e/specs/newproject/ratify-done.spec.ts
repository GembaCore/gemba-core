// specs/newproject/ratify-done.spec.ts — gm-root.17.7
//
// "Start planning" handoff screen shown after successful ratification.
// Covered scenarios:
//
//   - After ratify, the handoff screen renders in place of the two
//     panes (conversation + plan preview are gone).
//   - Start planning CTA navigates to /walk (Gemba walk).
//   - Skip CTA navigates to /gemba (dashboard).
//
// Active-workspace switch (gm-102l): no endpoint exists yet. Tests
// assert navigation happens; they do not assert a workspace-switch API
// call (that assertion lands when gm-102l is resolved).
//
// The fake-mode dispatcher in fixtures/server.ts returns
// { project_path, project_name, milestone_count, epic_count } from
// /api/v1/newproject/:id/ratify so the handoff screen renders.

import { test, expect } from '../../fixtures/server';

// Helper: navigate to /new, send a message (to populate the plan tree
// and enable Ratify), open the modal, and confirm.
async function ratifyProject(page: import('@playwright/test').Page): Promise<void> {
  await page.goto('/new');
  // Wait for the greeting from the fake skill.
  await expect(page.getByTestId('newproject-message-greeting')).toBeVisible();
  // Submit a turn to populate the plan tree + enable the Ratify button.
  await page.getByTestId('newproject-input').fill('build a CRM for small teams');
  await page.getByTestId('newproject-send').click();
  await expect(page.getByTestId('newproject-milestone-0')).toBeVisible();
  await expect(page.getByTestId('newproject-ratify')).toBeEnabled();
  // Open the modal and confirm.
  await page.getByTestId('newproject-ratify').click();
  await expect(page.getByTestId('newproject-ratify-modal')).toBeVisible();
  await page.getByTestId('newproject-ratify-confirm').click();
}

test.describe('Ratify done handoff screen (gm-root.17.7) @route', () => {
  test('handoff screen renders after successful ratify — conversation pane is gone', async ({
    page,
  }) => {
    await ratifyProject(page);
    // Handoff screen replaces the layout.
    await expect(page.getByTestId('ratify-done-screen')).toBeVisible();
    // Two-pane layout is gone.
    await expect(page.getByTestId('newproject-conversation-pane')).toBeHidden();
    await expect(page.getByTestId('newproject-plan-pane')).toBeHidden();
  });

  test('handoff screen shows the project name from the ratify response', async ({ page }) => {
    await ratifyProject(page);
    await expect(page.getByTestId('ratify-done-screen')).toBeVisible();
    // The fake dispatcher echoes "fake-new-project" as the project name.
    // The headline contains the name.
    await expect(page.getByTestId('ratify-done-project-name')).toContainText(
      'fake-new-project'
    );
  });

  test('handoff screen shows both CTAs', async ({ page }) => {
    await ratifyProject(page);
    await expect(page.getByTestId('ratify-done-start-planning')).toBeVisible();
    await expect(page.getByTestId('ratify-done-skip')).toBeVisible();
  });

  test('Start planning navigates to /walk (Gemba walk)', async ({ page }) => {
    // Wire up a fake /walk page so we can assert the navigation target.
    await page.route('**/walk**', async (route) => {
      // Let the SPA handle the route rather than intercepting network.
      await route.continue();
    });

    await ratifyProject(page);
    await expect(page.getByTestId('ratify-done-start-planning')).toBeVisible();
    await page.getByTestId('ratify-done-start-planning').click();

    // After clicking, the SPA should navigate to /walk.
    await expect.poll(() => page.url(), { timeout: 5_000 }).toContain('/walk');
  });

  test('Skip navigates to /gemba (dashboard)', async ({ page }) => {
    await ratifyProject(page);
    await expect(page.getByTestId('ratify-done-skip')).toBeVisible();
    await page.getByTestId('ratify-done-skip').click();

    // After clicking, the SPA should navigate to /gemba.
    await expect.poll(() => page.url(), { timeout: 5_000 }).toContain('/gemba');
  });

  test('handoff screen data-phase is "done"', async ({ page }) => {
    await ratifyProject(page);
    await expect(page.getByTestId('ratify-done-screen')).toBeVisible();
    // The page wrapper carries data-phase="done" (set in NewProjectPage).
    await expect(page.getByTestId('newproject-page')).toHaveAttribute('data-phase', 'done');
  });
});
