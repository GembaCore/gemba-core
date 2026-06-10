// pages/EpicDetail.ts — POM for EpicDetail (web/src/components/rhp/details/EpicDetail.tsx)
//
// EpicDetail is opened by visiting /board/:epicId which calls popDetail({kind:'epic',...})
// on mount, causing the RHP to render the detail tab. Test ids use the
// epic-detail-* prefix (migrated from epic-drawer-*).

import { type Locator, type Page, expect } from '@playwright/test';

export class EpicDetailPO {
  readonly page: Page;
  readonly idLabel: Locator;
  readonly copyButton: Locator;
  readonly stage: Locator;
  readonly start: Locator;
  readonly dispatch: Locator;
  readonly newChild: Locator;
  readonly stateSection: Locator;
  readonly descriptionSection: Locator;
  readonly childrenSection: Locator;
  readonly scrollArea: Locator;

  constructor(page: Page) {
    this.page = page;
    this.idLabel = page.getByTestId('epic-detail-id');
    this.copyButton = page.getByTestId('epic-detail-copy');
    this.stage = page.getByTestId('epic-detail-stage');
    this.start = page.getByTestId('epic-detail-start');
    this.dispatch = page.getByTestId('epic-detail-dispatch');
    this.newChild = page.getByTestId('epic-detail-new-child');
    this.stateSection = page.getByTestId('epic-section-state');
    this.descriptionSection = page.getByTestId('epic-section-description');
    this.childrenSection = page.getByTestId('epic-section-children');
    this.scrollArea = page.getByTestId('epic-detail-scroll');
  }

  async openByDeepLink(epicId: string): Promise<void> {
    // The board route uses a wildcard so encoded ids round-trip.
    await this.page.goto(`/board/${encodeURIComponent(epicId)}`);
    await expect(this.idLabel).toBeVisible();
  }

  async expectOpenWith(epicId: string): Promise<void> {
    await expect(this.idLabel).toBeVisible();
    await expect(this.idLabel).toHaveText(epicId);
  }
}
