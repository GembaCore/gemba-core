// specs/rhp/help-tab.spec.ts — gm-root.22.3.
//
// Help pinned tab in the Right-Hand Panel. Status is the default
// pinned tab now, but Help remains a stable pinned tab with route-aware
// content.
//
// Coverage:
//   - Help tab rail icon is visible on every top-level route.
//   - Status is the active tab on first load.
//   - Switching routes swaps the help body content.
//   - Cold-start (no projects seeded) shows the cold-start variant.
//
// Project: chrome-fake (matched by rhp/**/*.spec.ts in playwright.config.ts).

import type { Page } from '@playwright/test';
import { test, expect } from '../../fixtures/server';

// Helper: expand the RHP if collapsed (Help tab content only visible
// when expanded). Returns after confirming the body is rendered.
async function ensureExpanded(page: Page) {
  const shell = page.getByTestId('rhp-shell');
  const collapsed = await shell.getAttribute('data-collapsed');
  if (collapsed === 'true') {
    await page.getByTestId('rhp-collapse-toggle').click();
    // Wait for collapse transition.
    await expect(shell).toHaveAttribute('data-collapsed', 'false');
  }
}

async function openHelp(page: Page) {
  await page.getByTestId('rhp-tab-help').click();
  await expect(page.getByTestId('rhp-tab-help')).toHaveAttribute('data-active', 'true');
}

test.describe('RHP Help tab @chrome', () => {
  // ---------------------------------------------------------------------------
  // Rail presence
  // ---------------------------------------------------------------------------

  test.describe('with an active project', () => {
    test.beforeEach(({ projectsStore }) => {
      projectsStore.seed([
        { name: 'demo', path: '/tmp/projects/demo', active: true },
      ]);
    });

    test('Help tab rail icon is visible on /board', async ({ page }) => {
      await page.goto('/board');
      // The help tab button has data-testid="rhp-tab-help".
      await expect(page.getByTestId('rhp-tab-help')).toBeVisible();
    });

    test('Status tab is the active tab on first load', async ({ page }) => {
      await page.goto('/board');
      const statusTab = page.getByTestId('rhp-tab-status');
      const helpTab = page.getByTestId('rhp-tab-help');
      await expect(statusTab).toBeVisible();
      await expect(helpTab).toBeVisible();
      await expect(statusTab).toHaveAttribute('data-active', 'true');
      await expect(helpTab).toHaveAttribute('data-active', 'false');
    });

    test('Help tab body shows Plan-board content on /board', async ({ page }) => {
      await page.goto('/board');
      await ensureExpanded(page);
      await openHelp(page);
      // BoardHelp has data-testid="help-board".
      await expect(page.getByTestId('help-board')).toBeVisible();
    });

    test('switching to /walk shows walk content', async ({ page }) => {
      await page.goto('/walk');
      await ensureExpanded(page);
      await openHelp(page);
      // WalkHelp has data-testid="help-walk".
      await expect(page.getByTestId('help-walk')).toBeVisible();
    });

    test('switching to /escalations shows escalations content', async ({ page }) => {
      await page.goto('/escalations');
      await ensureExpanded(page);
      await openHelp(page);
      await expect(page.getByTestId('help-escalations')).toBeVisible();
    });

    test('switching to /sessions shows sessions content', async ({ page }) => {
      await page.goto('/sessions');
      await ensureExpanded(page);
      await openHelp(page);
      await expect(page.getByTestId('help-sessions')).toBeVisible();
    });

    test('switching to /settings shows settings content', async ({ page }) => {
      await page.goto('/settings');
      await ensureExpanded(page);
      await openHelp(page);
      await expect(page.getByTestId('help-settings')).toBeVisible();
    });

    test('Help tab body title is "Help"', async ({ page }) => {
      await page.goto('/board');
      await ensureExpanded(page);
      await openHelp(page);
      // RhpShell renders the active tab's label in the body title bar.
      await expect(page.getByTestId('rhp-body-title')).toContainText('Help');
    });

    test('route navigation returns focus to Status and Help can be reopened', async ({
      page,
    }) => {
      await page.goto('/board');
      await ensureExpanded(page);
      await openHelp(page);
      await expect(page.getByTestId('help-board')).toBeVisible();

      await page.goto('/escalations');
      // Route changes reset the RHP home tab to Status. Help remains
      // pinned and can be reopened for route-aware content.
      await expect(page.getByTestId('rhp-tab-status')).toHaveAttribute(
        'data-active',
        'true'
      );
      await openHelp(page);
      await expect(page.getByTestId('help-escalations')).toBeVisible();
    });
  });

  // ---------------------------------------------------------------------------
  // Cold-start
  // ---------------------------------------------------------------------------

  test.describe('cold-start (no projects seeded)', () => {
    test.beforeEach(({ projectsStore }) => {
      projectsStore.seed([]);
    });

    test('shows cold-start content', async ({ page }) => {
      await page.goto('/board');
      await ensureExpanded(page);
      await openHelp(page);
      // ColdStartHelp has data-testid="help-cold-start".
      await expect(page.getByTestId('help-cold-start')).toBeVisible();
    });

    test('cold-start content is shown on /walk too', async ({ page }) => {
      await page.goto('/walk');
      await ensureExpanded(page);
      await openHelp(page);
      await expect(page.getByTestId('help-cold-start')).toBeVisible();
    });

    test('cold-start content links to /settings', async ({ page }) => {
      await page.goto('/board');
      await ensureExpanded(page);
      await openHelp(page);
      await expect(page.getByTestId('help-cold-start')).toBeVisible();
      const settingsLink = page
        .getByTestId('help-cold-start')
        .getByRole('link', { name: /Configure settings/i });
      await expect(settingsLink).toBeVisible();
    });

    test('cold-start content links to the Getting Started docsite', async ({
      page,
    }) => {
      await page.goto('/board');
      await ensureExpanded(page);
      await openHelp(page);
      const guideLink = page
        .getByTestId('help-cold-start')
        .getByRole('link', { name: /Getting Started guide/i });
      await expect(guideLink).toBeVisible();
      const href = await guideLink.getAttribute('href');
      expect(href).toMatch(/gembacore\.github\.io/);
    });
  });
});
