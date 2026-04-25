// pages/GridPage.ts — POM for /grid (gm-5v8v.6).
//
// /grid hosts the WorkItemGrid (gm-e12.3.1) wrapped with column
// presets (gm-e12.3.3) and a JSONL import entry point. The grid
// virtualizes rows via @tanstack/react-virtual — only the visible
// slice is in the DOM, so spec assertions count `grid-row-*` from
// what's rendered now, not the full row set.

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
  readonly columnsToggle: Locator;
  readonly columnsMenu: Locator;
  readonly presetsToggle: Locator;
  readonly presetsMenu: Locator;
  readonly presetsSave: Locator;

  constructor(page: Page) {
    super(page);
    this.grid = page.getByTestId('work-item-grid');
    this.scroll = page.getByTestId('grid-scroll');
    this.empty = page.getByTestId('grid-empty');
    this.error = page.getByTestId('grid-error');
    this.count = page.getByTestId('grid-count');
    this.search = page.getByTestId('grid-search');
    this.importButton = page.getByTestId('grid-import-jsonl');
    this.columnsToggle = page.getByTestId('grid-columns-toggle');
    this.columnsMenu = page.getByTestId('grid-columns-menu');
    this.presetsToggle = page.getByTestId('grid-presets-toggle');
    this.presetsMenu = page.getByTestId('grid-presets-menu');
    this.presetsSave = page.getByTestId('grid-presets-save');
  }

  // goto lands on /grid and confirms the AppShell renders. Caller is
  // responsible for seeding the WorkPlane before navigating — the
  // empty state is its own assertion path.
  override async goto(path: string = '/grid'): Promise<void> {
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
    await this.page.getByTestId(`grid-state-${state}`).click();
  }
}
