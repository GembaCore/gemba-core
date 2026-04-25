// pages/AgentsPage.ts — POM for /agents (gm-5v8v.9).
//
// Testids come from web/src/pages/AgentsPage.tsx and
// web/src/components/agents/AgentDetailDrawer.tsx. The page already
// shipped with rich data-testids in gm-e12.4, so this POM is mostly
// thin getters.

import type { Locator, Page } from '@playwright/test';
import { AppShell } from './AppShell';

export class AgentsPage extends AppShell {
  readonly grid: Locator;
  readonly empty: Locator;
  readonly loading: Locator;
  readonly error: Locator;
  readonly drawer: Locator;

  constructor(page: Page) {
    super(page);
    this.grid = page.getByTestId('agents-grid');
    this.empty = page.getByTestId('agents-empty');
    this.loading = page.getByTestId('agents-loading');
    this.error = page.getByTestId('agents-error');
    this.drawer = page.getByTestId('agent-drawer');
  }

  async goto(): Promise<void> {
    await super.goto('/agents');
    await this.expectShellRendered();
  }

  tile(id: string): Locator {
    return this.page.getByTestId(`agent-tile-${id}`);
  }

  status(id: string): Locator {
    return this.page.getByTestId(`agent-tile-${id}-status`);
  }

  idleMarker(id: string): Locator {
    return this.page.getByTestId(`agent-tile-${id}-idle`);
  }

  drawerForAgent(id: string): Locator {
    return this.page.getByTestId(`agent-drawer-body-${id}`);
  }

  drawerCloseButton(): Locator {
    return this.page.getByTestId('agent-drawer-close');
  }

  drawerSession(): Locator {
    return this.page.getByTestId('agent-drawer-session');
  }
}
