// pages/EpicDrawer.ts — POM for EpicDrawer (web/src/components/board/EpicDrawer.tsx)
//
// EpicDrawer is opened by visiting /board/:epicId. Test ids cover
// the drawer chrome (overlay, content, header, scroll, close, copy)
// and the action buttons (Stage / Start / Dispatch / New child).

import { type Locator, type Page, expect } from '@playwright/test';

export class EpicDrawerPO {
  readonly page: Page;
  readonly overlay: Locator;
  readonly content: Locator;
  readonly idLabel: Locator;
  readonly closeButton: Locator;
  readonly copyButton: Locator;
  readonly stage: Locator;
  readonly start: Locator;
  readonly dispatch: Locator;
  readonly newChild: Locator;
  readonly stateSection: Locator;
  readonly descriptionSection: Locator;
  readonly childrenSection: Locator;

  constructor(page: Page) {
    this.page = page;
    this.overlay = page.getByTestId('epic-drawer-overlay');
    this.content = page.getByTestId('epic-drawer-content');
    this.idLabel = page.getByTestId('epic-drawer-id');
    this.closeButton = page.getByTestId('epic-drawer-close');
    this.copyButton = page.getByTestId('epic-drawer-copy');
    this.stage = page.getByTestId('epic-drawer-stage');
    this.start = page.getByTestId('epic-drawer-start');
    this.dispatch = page.getByTestId('epic-drawer-dispatch');
    this.newChild = page.getByTestId('epic-drawer-new-child');
    this.stateSection = page.getByTestId('epic-section-state');
    this.descriptionSection = page.getByTestId('epic-section-description');
    this.childrenSection = page.getByTestId('epic-section-children');
  }

  async openByDeepLink(epicId: string): Promise<void> {
    // The board route uses a wildcard so encoded ids round-trip.
    await this.page.goto(`/board/${encodeURIComponent(epicId)}`);
    await expect(this.content).toBeVisible();
  }

  async expectOpenWith(epicId: string): Promise<void> {
    await expect(this.content).toBeVisible();
    await expect(this.idLabel).toHaveText(epicId);
  }

  async closeViaButton(): Promise<void> {
    await this.closeButton.click();
    await expect(this.content).toBeHidden();
  }

  async closeViaEscape(): Promise<void> {
    await this.page.keyboard.press('Escape');
    await expect(this.content).toBeHidden();
  }
}
