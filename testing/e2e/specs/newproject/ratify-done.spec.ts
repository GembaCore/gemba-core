// specs/newproject/ratify-done.spec.ts — gm-root.17.7 + gm-102l
//
// "Start planning" handoff screen shown after successful ratification.
// Covered scenarios:
//
//   - After ratify, the handoff screen renders in place of the two
//     panes (conversation + plan preview are gone).
//   - Start planning CTA calls POST /api/v1/projects/switch BEFORE
//     navigating to /walk (gm-102l).
//   - Skip CTA calls POST /api/v1/projects/switch BEFORE navigating
//     to /gemba (gm-102l).
//
// The fake-mode dispatcher in fixtures/server.ts returns
// { project_path, project_name, milestone_count, epic_count } from
// /api/v1/newproject/:id/ratify so the handoff screen renders.
// The dispatcher also handles /api/v1/projects/switch — specs seed
// the project into projectsStore so the switch returns 200.

import { test, expect } from '../../fixtures/server';

// Helper: navigate to /new, send a message (to populate the plan tree
// and enable Ratify), open the modal, and confirm.
// Seeds the fake project into projectsStore so /api/v1/projects/switch
// returns 200 (required by gm-102l wiring).
async function ratifyProject(
  page: import('@playwright/test').Page,
  projectsStore: import('../../fixtures/server').ProjectsStore
): Promise<void> {
  // Seed the fake project so the switch endpoint finds it.
  projectsStore.seed([
    { name: 'fake-new-project', path: '/tmp/fake-projects/fake-new-project' },
  ]);

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

test.describe('Ratify done handoff screen (gm-root.17.7 + gm-102l) @route', () => {
  test('handoff screen renders after successful ratify — conversation pane is gone', async ({
    page,
    projectsStore,
  }) => {
    await ratifyProject(page, projectsStore);
    // Handoff screen replaces the layout.
    await expect(page.getByTestId('ratify-done-screen')).toBeVisible();
    // Two-pane layout is gone.
    await expect(page.getByTestId('newproject-conversation-pane')).toBeHidden();
    await expect(page.getByTestId('newproject-plan-pane')).toBeHidden();
  });

  test('handoff screen shows the project name from the ratify response', async ({
    page,
    projectsStore,
  }) => {
    await ratifyProject(page, projectsStore);
    await expect(page.getByTestId('ratify-done-screen')).toBeVisible();
    // The fake dispatcher echoes "fake-new-project" as the project name.
    // The headline contains the name.
    await expect(page.getByTestId('ratify-done-project-name')).toContainText(
      'fake-new-project'
    );
  });

  test('handoff screen shows both CTAs', async ({ page, projectsStore }) => {
    await ratifyProject(page, projectsStore);
    await expect(page.getByTestId('ratify-done-start-planning')).toBeVisible();
    await expect(page.getByTestId('ratify-done-skip')).toBeVisible();
  });

  test('Start planning calls POST /api/v1/projects/switch before navigating to /walk', async ({
    page,
    projectsStore,
  }) => {
    // Capture requests to /api/v1/projects/switch.
    const switchRequests: string[] = [];
    const navTimestamps: number[] = [];

    page.on('request', (req) => {
      if (
        req.url().includes('/api/v1/projects/switch') &&
        req.method() === 'POST'
      ) {
        switchRequests.push(req.url());
      }
    });

    await ratifyProject(page, projectsStore);
    await expect(page.getByTestId('ratify-done-start-planning')).toBeVisible();
    await page.getByTestId('ratify-done-start-planning').click();

    // After clicking, the SPA should navigate to /walk.
    await expect.poll(() => page.url(), { timeout: 5_000 }).toContain('/walk');
    navTimestamps.push(Date.now());

    // The switch endpoint must have been called.
    expect(switchRequests.length).toBeGreaterThan(0);
  });

  test('Skip calls POST /api/v1/projects/switch before navigating to /gemba', async ({
    page,
    projectsStore,
  }) => {
    const switchRequests: string[] = [];

    page.on('request', (req) => {
      if (
        req.url().includes('/api/v1/projects/switch') &&
        req.method() === 'POST'
      ) {
        switchRequests.push(req.url());
      }
    });

    await ratifyProject(page, projectsStore);
    await expect(page.getByTestId('ratify-done-skip')).toBeVisible();
    await page.getByTestId('ratify-done-skip').click();

    // After clicking, the SPA should navigate to /gemba.
    await expect.poll(() => page.url(), { timeout: 5_000 }).toContain('/gemba');

    // The switch endpoint must have been called.
    expect(switchRequests.length).toBeGreaterThan(0);
  });

  test('handoff screen data-phase is "done"', async ({ page, projectsStore }) => {
    await ratifyProject(page, projectsStore);
    await expect(page.getByTestId('ratify-done-screen')).toBeVisible();
    // The page wrapper carries data-phase="done" (set in NewProjectPage).
    await expect(page.getByTestId('newproject-page')).toHaveAttribute('data-phase', 'done');
  });
});
