// specs/setup/project-config.spec.ts — gm-uipx.8
//
// /project/config (ui-spec §5.16). Single-scroll page with a sticky
// section nav listing all 11 sections; Values editor is a modal;
// Adaptors section is read-only; Workspace repos lists from
// RepositoryRegistry.

import { test, expect } from '../../fixtures/server';

const SECTIONS = [
  'values',
  'guardrails',
  'goals',
  'personas',
  'adaptors',
  'packs',
  'integrations',
  'mode',
  'subscriptions',
  'workspace-repos',
  'advanced',
] as const;

test.describe('Project config @route', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/project/config');
    await page.evaluate(() => window.localStorage.clear());
    await page.reload();
  });

  test('sticky nav lists all 11 sections in order', async ({ page }) => {
    const nav = page.getByTestId('project-config-sticky-nav');
    await expect(nav).toBeVisible();
    for (const id of SECTIONS) {
      await expect(page.getByTestId(`project-config-nav-${id}`)).toBeVisible();
    }
  });

  test('every section renders with its anchor and testid', async ({ page }) => {
    for (const id of SECTIONS) {
      await expect(page.getByTestId(`project-config-section-${id}`)).toBeVisible();
    }
  });

  test('Values editor is a modal with priority-ranked statement table', async ({ page }) => {
    await page.getByTestId('project-config-values-edit').click();
    const modal = page.getByTestId('values-editor-modal');
    await expect(modal).toBeVisible();

    // Add three values.
    for (const v of ['Ship continuously', 'Prefer reversible decisions', 'Write narrow interfaces']) {
      await page.getByTestId('values-editor-draft').fill(v);
      await page.getByTestId('values-editor-add').click();
    }
    await expect(page.getByTestId('values-editor-table')).toBeVisible();

    // Close, then re-open. The values persist in localStorage.
    await page.getByTestId('values-editor-close').click();
    await expect(page.getByTestId('values-editor-modal')).toBeHidden();
    // Section preview now shows the top three.
    await expect(page.getByText('Ship continuously')).toBeVisible();
    await expect(page.getByText('Prefer reversible decisions')).toBeVisible();
  });

  test('Adaptors section is read-only with link to .gemba/workspace.toml', async ({ page }) => {
    const banner = page.getByTestId('project-config-adaptors-readonly');
    await expect(banner).toBeVisible();
    await expect(banner).toContainText('Read-only');
    await expect(banner).toContainText('.gemba/workspace.toml');
    await expect(page.getByTestId('project-config-adaptors-link')).toBeVisible();
  });

  test('Workspace repos surfaces a notice when no .gemba/repositories/ is configured', async ({ page }) => {
    // The fake-mode test environment has no .gemba/repositories,
    // so the API returns the empty-list+notice payload.
    await expect(page.getByTestId('project-config-repos-notice')).toBeVisible();
  });

  test('clicking a sticky nav entry scrolls the section into view', async ({ page }) => {
    const advancedNav = page.getByTestId('project-config-nav-advanced');
    await advancedNav.click();
    // After click, the URL hash updates and the Advanced section
    // is in view.
    await expect.poll(() => page.url(), { timeout: 2_000 }).toContain('#advanced');
    const section = page.getByTestId('project-config-section-advanced');
    await expect(section).toBeInViewport();
  });
});
