// pages/GridPage.ts — POM for the Board's list+power layout
// (gm-uipx.17). Originally pointed at the standalone /grid route
// (gm-5v8v.6); after gm-uipx.17 /grid was folded into
// /board?layout=list&power=1 — same WorkItemGrid component, same
// column-presets + bulk-action + JSONL-import features, just hosted
// inside Board's chrome instead of its own page.
//
// Class name kept as GridPage so spec imports stay stable; the
// `goto()` default and chrome-level locators were rewritten to point
// at Board's list view + the new ?power=1 flag.
//
// The grid virtualizes rows via @tanstack/react-virtual — only the
// visible slice is in the DOM, so spec assertions count `grid-row-*`
// from what's rendered now, not the full row set.

import { expect, type Locator, type Page } from '@playwright/test';
import { AppShell } from './AppShell';
import type { StateCategory } from '../../../web/src/types/core.gen';

export class GridPage extends AppShell {
  readonly grid: Locator;
  readonly scroll: Locator;
  readonly empty: Locator;
  readonly error: Locator;
  readonly count: Locator;
  readonly search: Locator;
  readonly importButton: Locator;
  readonly powerToggle: Locator;
  readonly columnsToggle: Locator;
  readonly columnsMenu: Locator;
  readonly presetsToggle: Locator;
  readonly presetsMenu: Locator;
  readonly presetsSave: Locator;

  constructor(page: Page) {
    super(page);
    this.grid = page.getByTestId('work-item-grid');
    this.scroll = page.getByTestId('grid-scroll');
    // Chrome-level testids are board-list-* — rendered by
    // BoardListView (the host of the WorkItemGrid in list mode).
    this.empty = page.getByTestId('board-list-empty');
    this.error = page.getByTestId('board-list-error');
    this.count = page.getByTestId('board-list-count');
    this.search = page.getByTestId('board-list-search');
    // Power toggle and the import button are exposed by BoardPage /
    // BoardListView only when ?power=1 is set.
    this.importButton = page.getByTestId('grid-import-jsonl');
    this.powerToggle = page.getByTestId('board-power-toggle');
    // WorkItemGrid component testids stay grid-* — the component is
    // unchanged; only its host moved.
    this.columnsToggle = page.getByTestId('grid-columns-toggle');
    this.columnsMenu = page.getByTestId('grid-columns-menu');
    this.presetsToggle = page.getByTestId('grid-presets-toggle');
    this.presetsMenu = page.getByTestId('grid-presets-menu');
    this.presetsSave = page.getByTestId('grid-presets-save');
  }

  // goto lands on Board's list+power layout. The /grid bookmark
  // still resolves (App.tsx Navigate) but specs use the canonical URL
  // so a regression in the redirect surfaces in its own dedicated
  // spec, not as a failure here.
  override async goto(path: string = '/board?layout=list&power=1'): Promise<void> {
    await super.goto(path);
    await this.expectShellRendered();
  }

  // row locator by id. Only present when the row is in the rendered
  // virtual window — for off-screen rows, scroll first.
  row(id: string): Locator {
    return this.page.getByTestId(`grid-row-${id}`);
  }

  // visibleRowIds returns the ids of rows currently in the DOM. With
  // virtualization, this is the subset @tanstack/react-virtual chose
  // to mount, not the full row set.
  async visibleRowIds(): Promise<string[]> {
    const handles = await this.scroll.locator('[data-testid^="grid-row-"]').all();
    const ids: string[] = [];
    for (const h of handles) {
      const id = await h.getAttribute('data-testid');
      if (id) ids.push(id.replace(/^grid-row-/, ''));
    }
    return ids;
  }

  // expectCountText asserts the count line at the top of the grid
  // (e.g. "10 items" or "10 items · 3 shown" when search filters).
  async expectCountText(re: RegExp): Promise<void> {
    await expect(this.count).toContainText(re);
  }

  // openColumnsMenu / closeColumnsMenu wrap the toggle so specs read
  // as the operator action, not the underlying click.
  async openColumnsMenu(): Promise<void> {
    await this.columnsToggle.click();
    await expect(this.columnsMenu).toBeVisible();
  }

  async openPresetsMenu(): Promise<void> {
    await this.presetsToggle.click();
    await expect(this.presetsMenu).toBeVisible();
  }

  // applyPreset clicks a preset row's apply target. Built-in preset
  // ids are "default" and "compact"; user presets carry "user:..." ids.
  async applyPreset(presetId: string): Promise<void> {
    await this.openPresetsMenu();
    await this.page.getByTestId(`grid-preset-apply-${presetId}`).click();
  }

  // openImport opens JsonlImportDialog. Use jsonl-import-* testids
  // for assertions inside.
  async openImport(): Promise<void> {
    await this.importButton.click();
    await expect(this.page.getByTestId('jsonl-import-dialog')).toBeVisible();
  }

  // toggleStateChip toggles a state-category filter chip.
  async toggleStateChip(state: StateCategory): Promise<void> {
    await this.page.getByTestId(`board-list-state-${state}`).click();
  }
}
