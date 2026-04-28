// specs/newproject/newproject.spec.ts — gm-root.17.3
//
// /new full-page conversational project creation surface (see
// docs/design/newproject.md). Two panes (conversation + plan
// preview), persistent Ratify button, nonce-confirmed commit modal,
// one-shot session.
//
// Backend handlers (`newproject` skill — gm-root.17.5; Onboarder
// persona — gm-root.17.10; atomic ratify — gm-root.17.6) DO NOT exist
// yet. The fake-mode dispatcher in fixtures/server.ts mocks the API
// surface so this spec proves the SPA flow end-to-end without the
// backend. Real-mode coverage lands when the backend beads do.

import { test, expect } from '../../fixtures/server';

test.describe('/new (newproject — gm-root.17.3) @route', () => {
  test('renders both panes and the persistent Ratify button', async ({ page }) => {
    await page.goto('/new');
    await expect(page.getByTestId('newproject-page')).toBeVisible();
    await expect(page.getByTestId('newproject-conversation-pane')).toBeVisible();
    await expect(page.getByTestId('newproject-plan-pane')).toBeVisible();
    await expect(page.getByTestId('newproject-ratify')).toBeVisible();
    // Initial state — no milestones — so Ratify is disabled.
    await expect(page.getByTestId('newproject-ratify')).toBeDisabled();
    // Plan tree paints its empty state.
    await expect(page.getByTestId('newproject-plan-empty')).toBeVisible();
  });

  test('greeting from the skill lands in the transcript on first paint', async ({ page }) => {
    await page.goto('/new');
    await expect(page.getByTestId('newproject-message-greeting')).toBeVisible();
    await expect(page.getByTestId('newproject-message-greeting')).toContainText(
      'Onboarder'
    );
  });

  test('submitting a turn populates the plan tree and enables Ratify', async ({ page }) => {
    await page.goto('/new');
    await expect(page.getByTestId('newproject-input')).toBeVisible();
    await page.getByTestId('newproject-input').fill('build a CRM for small teams');
    await page.getByTestId('newproject-send').click();
    // Plan tree paints with at least one milestone.
    await expect(page.getByTestId('newproject-milestone-0')).toBeVisible();
    // Diff badge surfaces the change summary.
    await expect(page.getByTestId('newproject-diff-badge')).toBeVisible();
    // Ratify button is now actionable.
    await expect(page.getByTestId('newproject-ratify')).toBeEnabled();
  });

  test('mid-conversation in-place edits update the plan preview', async ({ page }) => {
    await page.goto('/new');
    await page.getByTestId('newproject-input').fill('build a CRM');
    await page.getByTestId('newproject-send').click();
    await expect(page.getByTestId('newproject-milestone-0')).toBeVisible();
    // Operator edits the milestone title in-place; the value sticks
    // in the input.
    const titleInput = page.getByTestId('newproject-milestone-0-title');
    await titleInput.fill('M1 — OSS-ready');
    await expect(titleInput).toHaveValue('M1 — OSS-ready');
  });

  test('Ratify modal nonce-confirms commit and navigates to next_url', async ({ page }) => {
    // Capture the ratify POST so we can assert the nonce header is
    // present (X-GEMBA-Confirm — server-side nonce gate per
    // workItems.ts CONFIRM_HEADER).
    let confirmHeader: string | undefined;
    await page.route('**/api/v1/newproject/*/ratify', async (route) => {
      const headers = route.request().headers();
      confirmHeader = headers['x-gemba-confirm'];
      await route.fulfill({
        json: { project_path: '/tmp/p', next_url: '/board' },
      });
    });

    await page.goto('/new');
    await page.getByTestId('newproject-input').fill('build a CRM');
    await page.getByTestId('newproject-send').click();
    await expect(page.getByTestId('newproject-ratify')).toBeEnabled();

    await page.getByTestId('newproject-ratify').click();
    await expect(page.getByTestId('newproject-ratify-modal')).toBeVisible();
    await expect(page.getByTestId('newproject-ratify-tree')).toBeVisible();
    await expect(page.getByTestId('newproject-ratify-count-milestones')).toContainText('1');

    await page.getByTestId('newproject-ratify-confirm').click();
    await expect.poll(() => page.url(), { timeout: 5_000 }).toContain('/board');
    expect(confirmHeader, 'ratify POST must carry X-GEMBA-Confirm nonce').toBeTruthy();
    expect(confirmHeader!.length).toBeGreaterThan(0);
  });

  test('Cancel from the ratify modal does not commit', async ({ page }) => {
    let ratifyCalls = 0;
    await page.route('**/api/v1/newproject/*/ratify', async (route) => {
      ratifyCalls += 1;
      await route.fulfill({
        json: { project_path: '/tmp/p', next_url: '/board' },
      });
    });

    await page.goto('/new');
    await page.getByTestId('newproject-input').fill('build a CRM');
    await page.getByTestId('newproject-send').click();
    await expect(page.getByTestId('newproject-ratify')).toBeEnabled();
    await page.getByTestId('newproject-ratify').click();
    await expect(page.getByTestId('newproject-ratify-modal')).toBeVisible();
    await page.getByTestId('newproject-ratify-cancel').click();
    await expect(page.getByTestId('newproject-ratify-modal')).toBeHidden();
    expect(ratifyCalls).toBe(0);
  });

  test('refresh discards the session (one-shot persistence)', async ({ page }) => {
    await page.goto('/new');
    await page.getByTestId('newproject-input').fill('build a CRM');
    await page.getByTestId('newproject-send').click();
    await expect(page.getByTestId('newproject-milestone-0')).toBeVisible();
    // Refresh — the design doc is explicit: refresh discards the
    // session, no resume affordance, no draft autosave.
    await page.reload();
    await expect(page.getByTestId('newproject-plan-empty')).toBeVisible();
    // Greeting reappears as the fresh session's first transcript entry.
    await expect(page.getByTestId('newproject-message-greeting')).toBeVisible();
  });
});
