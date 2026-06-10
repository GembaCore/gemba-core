// pages/AppShell.ts — POM base class.
//
// Every route POM extends AppShell so chrome assertions (sidebar,
// topbar, adaptor banner) come for free. Per-route POMs live alongside
// their tier directories (e.g. pages/BoardPage.ts for board specs).
//
// Selector strategy: prefer the existing data-testid attributes the
// SPA already ships (179 across web/src). Fall back to ARIA roles
// when no testid exists. Avoid CSS class selectors — they churn.

import { type Locator, type Page, expect } from '@playwright/test';

export class AppShell {
  readonly page: Page;
  readonly main: Locator;
  // The adaptor banner is conditional — it renders only when an
  // adaptor is degraded (see web/src/components/AdaptorBanner.tsx).
  // We expose it for richer specs; smoke just asserts <main>.
  readonly adaptorBanner: Locator;

  constructor(page: Page) {
    this.page = page;
    this.main = page.locator('main');
    this.adaptorBanner = page.getByTestId('adaptor-banner');
  }

  async goto(path: string): Promise<void> {
    const response = await this.page.goto(path);
    expect(response, `expected a response for ${path}`).not.toBeNull();
    expect(response!.status(), `${path} returned ${response!.status()}`).toBeLessThan(400);
  }

  async expectShellRendered(): Promise<void> {
    await expect(this.main).toBeVisible();
  }
}
