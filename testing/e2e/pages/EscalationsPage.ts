// pages/EscalationsPage.ts — POM for /escalations (gm-e11.8.1).
//
// Composes AppShell so chrome assertions come for free. Testids come
// from web/src/pages/EscalationsPage.tsx; per-card actions key off the
// escalation id (escalation-card-${id}-resolve / -agent / -workitem).

import type { Locator, Page } from '@playwright/test';
import { AppShell } from './AppShell';

export class EscalationsPage extends AppShell {
  readonly empty: Locator;
  readonly loading: Locator;
  readonly error: Locator;

  constructor(page: Page) {
    super(page);
    this.empty = page.getByTestId('escalations-empty');
    this.loading = page.getByTestId('escalations-loading');
    this.error = page.getByTestId('escalations-error');
  }

  override async goto(): Promise<void> {
    await super.goto('/escalations');
    await this.expectShellRendered();
  }

  card(id: string): Locator {
    return this.page.getByTestId(`escalation-card-${id}`);
  }

  resolveButton(id: string): Locator {
    return this.page.getByTestId(`escalation-card-${id}-resolve`);
  }

  section(severity: 'critical' | 'high' | 'medium' | 'low' | 'info'): Locator {
    return this.page.getByTestId(`escalations-section-${severity}`);
  }

  modal(): Locator {
    return this.page.getByTestId('escalation-resolve-modal');
  }

  resolveOption(kind: 'approve' | 'deny' | 'modify' | 'defer'): Locator {
    return this.page.getByTestId(`escalation-resolve-option-${kind}`);
  }

  modifyValue(): Locator {
    return this.page.getByTestId('escalation-resolve-modify-value');
  }

  confirmButton(): Locator {
    return this.page.getByTestId('escalation-resolve-confirm');
  }

  cancelButton(): Locator {
    return this.page.getByTestId('escalation-resolve-cancel');
  }
}
