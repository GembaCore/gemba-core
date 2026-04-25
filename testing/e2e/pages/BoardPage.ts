// pages/BoardPage.ts — POM for /board (gm-5v8v.5).
//
// Composes AppShell so chrome assertions come for free. Specs read as
// prose via the high-level methods (gotoEpicView / gotoWorkItemView /
// expectColumnOrder / expectColumnCount); raw locators are exposed
// only for the cases POM methods can't naturally express.

import { expect, type Locator, type Page } from '@playwright/test';
import { AppShell } from './AppShell';

// COLUMN_ORDER mirrors web/src/types/core.gen.ts STATE_CATEGORIES.
// The bead's spec calls out the LABEL order ("Backlog → Next Up → …")
// — keep in lockstep with web/src/pages/BoardPage.tsx COLUMN_LABELS.
export const COLUMN_ORDER = [
  'backlog',
  'unstarted',
  'staged',
  'started',
  'completed',
  'canceled',
] as const;
export type ColumnId = (typeof COLUMN_ORDER)[number];

export const COLUMN_LABEL: Record<ColumnId, string> = {
  backlog: 'Backlog',
  unstarted: 'Next Up',
  staged: 'Staged',
  started: 'In Progress',
  completed: 'Done',
  canceled: 'Canceled',
};

export class BoardPage extends AppShell {
  // The flat WorkItem grid (?view=workitem). Lives in BoardPage.tsx
  // under data-testid="board-workitem".
  readonly workItemBoard: Locator;
  readonly viewToggleEpic: Locator;
  readonly viewToggleWorkItem: Locator;
  readonly empty: Locator;
  readonly loading: Locator;
  readonly error: Locator;
  readonly newWorkItemButton: Locator;
  readonly swimlaneSwitcher: Locator;

  constructor(page: Page) {
    super(page);
    this.workItemBoard = page.getByTestId('board-workitem');
    this.viewToggleEpic = page.getByTestId('view-toggle-epic');
    this.viewToggleWorkItem = page.getByTestId('view-toggle-workitem');
    this.empty = page.getByTestId('board-empty');
    this.loading = page.getByTestId('board-loading');
    this.error = page.getByTestId('board-error');
    this.newWorkItemButton = page.getByTestId('board-new-workitem');
    this.swimlaneSwitcher = page.getByTestId('swimlane-switcher');
  }

  // gotoEpicView lands on the default /board surface (Epic view +
  // by-parent-epic swimlane). Confirms the AppShell renders before
  // returning so a flake in the dev-server boot doesn't mask a real
  // render bug downstream.
  async gotoEpicView(): Promise<void> {
    await this.goto('/board');
    await this.expectShellRendered();
  }

  // gotoWorkItemView is the M1 flat-board variant: /board?view=workitem.
  // Spec L293 in the ui-spec calls this the "alternate" view; the
  // default is Epic-primary.
  async gotoWorkItemView(): Promise<void> {
    await this.goto('/board?view=workitem');
    await this.expectShellRendered();
    await expect(this.workItemBoard).toBeVisible();
  }

  // column locator by state category. Both views render the columns
  // with the same testid pattern (`board-column-${cat}`).
  column(cat: ColumnId): Locator {
    return this.page.getByTestId(`board-column-${cat}`);
  }

  // expectColumnOrder asserts the six columns render in the canonical
  // sequence the bead pins (Backlog → Next Up → Staged → In Progress →
  // Done → Canceled).
  async expectColumnOrder(): Promise<void> {
    // Match `board-column-<state>` exactly — there's a sibling
    // `board-column-<state>-count` testid that the prefix selector
    // would otherwise capture.
    const want = COLUMN_ORDER.map((c) => `board-column-${c}`);
    const ids = await this.workItemBoard
      .locator('[data-testid^="board-column-"]')
      .evaluateAll((nodes) =>
        nodes
          .map((n) => (n as HTMLElement).dataset.testid ?? '')
          .filter((id) => /^board-column-(?!.*-count$)/.test(id))
      );
    expect(ids).toEqual(want);
  }

  // expectColumnCount reads the count badge inside a column header.
  // The badge has its own data-testid (`board-column-${cat}-count`)
  // so this never collides with the per-card spans nested below.
  async expectColumnCount(cat: ColumnId, n: number): Promise<void> {
    const badge = this.page.getByTestId(`board-column-${cat}-count`);
    const text = (await badge.textContent()) ?? '0';
    expect(Number(text.trim()), `column ${cat} count`).toBe(n);
  }
}
