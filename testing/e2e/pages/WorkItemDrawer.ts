// pages/WorkItemDrawer.ts — POM for WorkItemDrawer
// (web/src/components/board/WorkItemDrawer.tsx).
//
// Opens via /board?bead=ID. Tabs: description, edges, evidence,
// dod, sprint, activity, (extensions when item.custom is non-empty).

import { type Locator, type Page, expect } from '@playwright/test';

export type DrawerTab =
  | 'description'
  | 'edges'
  | 'evidence'
  | 'dod'
  | 'sprint'
  | 'activity'
  | 'extensions';

export class WorkItemDrawerPO {
  readonly page: Page;
  readonly overlay: Locator;
  readonly content: Locator;
  readonly idLabel: Locator;
  readonly closeButton: Locator;
  readonly copyButton: Locator;
  readonly backButton: Locator;
  readonly dispatch: Locator;
  readonly tabs: Locator;
  readonly dodBanner: Locator;

  constructor(page: Page) {
    this.page = page;
    this.overlay = page.getByTestId('work-item-drawer-overlay');
    this.content = page.getByTestId('work-item-drawer-content');
    this.idLabel = page.getByTestId('work-item-drawer-id');
    this.closeButton = page.getByTestId('work-item-drawer-close');
    this.copyButton = page.getByTestId('work-item-drawer-copy');
    this.backButton = page.getByTestId('work-item-drawer-back');
    this.dispatch = page.getByTestId('work-item-drawer-dispatch');
    this.tabs = page.getByTestId('work-item-drawer-tabs');
    this.dodBanner = page.getByTestId('work-item-dod-banner');
  }

  tab(name: DrawerTab): Locator {
    return this.page.getByTestId(`drawer-tab-${name}`);
  }

  async openByDeepLink(beadId: string): Promise<void> {
    await this.page.goto(`/board?bead=${encodeURIComponent(beadId)}`);
    await expect(this.content).toBeVisible();
  }

  async expectOpenWith(beadId: string): Promise<void> {
    await expect(this.content).toBeVisible();
    await expect(this.idLabel).toHaveText(beadId);
  }

  async closeViaButton(): Promise<void> {
    await this.closeButton.click();
    await expect(this.content).toBeHidden();
  }

  async closeViaEscape(): Promise<void> {
    await this.page.keyboard.press('Escape');
    await expect(this.content).toBeHidden();
  }

  async selectTab(name: DrawerTab): Promise<void> {
    await this.tab(name).click();
    await expect(this.tab(name)).toHaveAttribute('aria-selected', 'true');
  }
}
