// pages/WorkItemDrawer.ts — POM for the work-item detail surface.
//
// Background (gm-root.22.5/.6/.7/.8): the standalone WorkItemDrawer
// (Radix Dialog overlay rendered from BoardPage) was retired and the
// same content now lives as a Right-Hand Panel (RHP) detail tab,
// `web/src/components/rhp/details/WorkItemDetail.tsx`. The legacy
// `?bead=X` deep-link is migrated on first paint of /board to the
// canonical `?rhp=workitem:X` codec.
//
// Test-ids that move with the migration:
//   work-item-drawer-overlay   → (gone — RHP is not a modal)
//   work-item-drawer-content   → workitem-detail-scroll (visibility check)
//   work-item-drawer-id        → workitem-detail-id
//   work-item-drawer-close     → rhp-tab-close-workitem:<id>
//   work-item-drawer-copy      → workitem-detail-copy
//   work-item-drawer-back      → workitem-detail-back
//   work-item-drawer-dispatch  → workitem-detail-dispatch
//   work-item-drawer-tabs      → workitem-detail-tabs
//   drawer-tab-<name>          → detail-tab-<name>   (already aligned)
//
// Specs continue to talk to a `WorkItemDrawerPO` so the migration is
// a one-file change; the rest of the e2e tree is unaffected.

import { type Locator, type Page, expect } from '@playwright/test';

// Evidence is a Summary-tab inline section per ui-spec §5.7
// (gm-g9t1) — no standalone tab.
export type DrawerTab =
  | 'description'
  | 'edges'
  | 'dod'
  | 'sprint'
  | 'activity'
  | 'extensions';

export class WorkItemDrawerPO {
  readonly page: Page;
  readonly content: Locator;
  readonly idLabel: Locator;
  readonly closeButton: (beadId: string) => Locator;
  readonly copyButton: Locator;
  readonly backButton: Locator;
  readonly dispatch: Locator;
  readonly tabs: Locator;
  readonly dodBanner: Locator;

  constructor(page: Page) {
    this.page = page;
    this.content = page.getByTestId('workitem-detail-scroll');
    this.idLabel = page.getByTestId('workitem-detail-id');
    this.closeButton = (beadId: string) =>
      page.getByTestId(`rhp-tab-close-workitem:${beadId}`);
    this.copyButton = page.getByTestId('workitem-detail-copy');
    this.backButton = page.getByTestId('workitem-detail-back');
    this.dispatch = page.getByTestId('workitem-detail-dispatch');
    this.tabs = page.getByTestId('workitem-detail-tabs');
    this.dodBanner = page.getByTestId('work-item-dod-banner');
  }

  tab(name: DrawerTab): Locator {
    return this.page.getByTestId(`detail-tab-${name}`);
  }

  async openByDeepLink(beadId: string): Promise<void> {
    // The legacy ?bead=X deep-link is still accepted; BoardPage
    // migrates it to ?rhp=workitem:X on first paint so the RHP
    // detail tab opens.
    await this.page.goto(`/board?bead=${encodeURIComponent(beadId)}`);
    await expect(this.content).toBeVisible();
  }

  async expectOpenWith(beadId: string): Promise<void> {
    await expect(this.content).toBeVisible();
    await expect(this.idLabel).toHaveText(beadId);
  }

  async closeViaButton(beadId: string): Promise<void> {
    await this.closeButton(beadId).click();
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
