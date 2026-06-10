// pages/SessionsPage.ts — POM for /sessions (gm-5v8v.9).
//
// Composes AppShell so chrome assertions come for free. Testids come
// from web/src/pages/SessionsPage.tsx; the New session button is
// targeted by `sessions-new`, the row + status + end button by
// `session-row-${id}` / `${id}-status` / `${id}-end`.

import type { Locator, Page } from '@playwright/test';
import { AppShell } from './AppShell';

export class SessionsPage extends AppShell {
  readonly empty: Locator;
  readonly loading: Locator;
  readonly error: Locator;
  readonly newSessionButton: Locator;
  readonly table: Locator;

  constructor(page: Page) {
    super(page);
    this.empty = page.getByTestId('sessions-empty');
    this.loading = page.getByTestId('sessions-loading');
    this.error = page.getByTestId('sessions-error');
    this.newSessionButton = page.getByTestId('sessions-new');
    this.table = page.getByTestId('sessions-table');
  }

  override async goto(): Promise<void> {
    await super.goto('/sessions');
    await this.expectShellRendered();
  }

  row(id: string): Locator {
    return this.page.getByTestId(`session-row-${id}`);
  }

  status(id: string): Locator {
    return this.page.getByTestId(`session-row-${id}-status`);
  }

  endButton(id: string): Locator {
    return this.page.getByTestId(`session-row-${id}-end`);
  }

  escalationsBadge(id: string): Locator {
    return this.page.getByTestId(`session-row-${id}-escalations`);
  }

  // The NewSessionDialog is mounted from this page; its testids live
  // in NewSessionDialog.tsx (new-session-dialog / -bead / -agent-type
  // / -submit / -cancel).
  newSessionDialog(): Locator {
    return this.page.getByTestId('new-session-dialog');
  }
}
