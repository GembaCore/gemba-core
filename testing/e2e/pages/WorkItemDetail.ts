// pages/WorkItemDetail.ts — POM for WorkItemDetail in the RHP (gm-root.22.5).
//
// The detail tab lives inside the RHP body (data-testid="rhp-body").
// Opened via:
//   - /board?rhp=workitem:ID   — direct codec
//   - /board?bead=ID            — legacy shim translates to ?rhp=workitem:ID
//   - Card click on the board
//
// Tabs: description, edges, dod, sprint, activity, (extensions when
// item.custom is non-empty). Evidence is inline on the description tab.

import { type Locator, type Page, expect } from '@playwright/test';

export type DetailTab =
  | 'description'
  | 'edges'
  | 'dod'
  | 'sprint'
  | 'activity'
  | 'extensions';

export class WorkItemDetailPO {
  readonly page: Page;
  /** The RHP body panel that hosts the active tab content. */
  readonly rhpBody: Locator;
  readonly idLabel: Locator;
  readonly copyButton: Locator;
  readonly backButton: Locator;
  readonly dispatch: Locator;
  readonly tabs: Locator;
  readonly dodBanner: Locator;

  constructor(page: Page) {
    this.page = page;
    this.rhpBody = page.getByTestId('rhp-body');
    this.idLabel = page.getByTestId('workitem-detail-id');
    this.copyButton = page.getByTestId('workitem-detail-copy');
    this.backButton = page.getByTestId('workitem-detail-back');
    this.dispatch = page.getByTestId('workitem-detail-dispatch');
    this.tabs = page.getByTestId('workitem-detail-tabs');
    this.dodBanner = page.getByTestId('work-item-dod-banner');
  }

  tab(name: DetailTab): Locator {
    return this.page.getByTestId(`detail-tab-${name}`);
  }

  /**
   * Open the workitem detail via the legacy /board?bead=ID deep-link.
   * The shim translates this to ?rhp=workitem:ID on first paint.
   */
  async openByLegacyDeepLink(beadId: string): Promise<void> {
    await this.page.goto(`/board?bead=${encodeURIComponent(beadId)}`);
    await expect(this.rhpBody).toBeVisible();
    await expect(this.idLabel).toBeVisible();
  }

  /**
   * Open the workitem detail via the canonical ?rhp= codec.
   */
  async openByRhpDeepLink(beadId: string): Promise<void> {
    await this.page.goto(`/board?rhp=workitem:${encodeURIComponent(beadId)}`);
    await expect(this.rhpBody).toBeVisible();
    await expect(this.idLabel).toBeVisible();
  }

  async expectOpenWith(beadId: string): Promise<void> {
    await expect(this.rhpBody).toBeVisible();
    await expect(this.idLabel).toHaveText(beadId);
  }

  /** Close via the RHP rail × button for the workitem tab. */
  async closeViaRailX(beadId: string): Promise<void> {
    const closeBtn = this.page.getByTestId(`rhp-tab-close-workitem:${beadId}`);
    await closeBtn.click();
    await expect(this.idLabel).toBeHidden();
  }

  async selectTab(name: DetailTab): Promise<void> {
    await this.tab(name).click();
    await expect(this.tab(name)).toHaveAttribute('aria-selected', 'true');
  }
}
