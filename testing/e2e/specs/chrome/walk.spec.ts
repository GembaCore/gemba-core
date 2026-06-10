// specs/chrome/walk.spec.ts — gm-isxs (refresh after gm-i65 / gm-0tl)
//
// Gemba walk surface (ui-spec §5.4). Lives in chrome/ because the
// active-walk banner is global chrome — it shows on every route once
// a walk is active. gm-i65 refactored /walk from a two-pane to a
// three-pane + bottom-bar layout (Agenda / Chat / Right + bottom
// status strip). gm-0tl re-scoped the R/M/X/D verbs to the 'walk'
// scope (only fire while /walk is mounted) and put 'n' / Enter under
// 'walk-active' (anywhere a walk is in-flight).
//
// The fake-backend fixture (testing/e2e/fixtures/server.ts) does not
// stub /api/v1/walks/* endpoints, so each test installs its own
// page.route() handlers that maintain a tiny in-memory walk state
// and respond to the SPA's POST/PATCH/GET sequence. Behaviour parity
// with the unit-tier fetch mock in
// web/src/pages/__tests__/WalkPage.test.tsx — ratify auto-promotes,
// defer drops the item to deferred, click-to-promote demotes the
// previous active.

import type { Page, Route } from '@playwright/test';
import { test, expect } from '../../fixtures/server';

interface FakeWalk {
  id: string;
  workspace: string;
  initiated_by: string;
  primary_persona: string;
  started_at: string;
  status: 'active' | 'paused' | 'completed' | 'abandoned';
  agenda: Array<{
    id: string;
    topic: string;
    source: { kind: string; ref?: string };
    priority: number;
    status: 'queued' | 'active' | 'decided' | 'deferred' | 'dismissed';
    added_at: string;
  }>;
  decisions?: Array<{
    agenda_item_id: string;
    kind: string;
    decided_at: string;
    decided_by: string;
  }>;
  cost: { tokens_in: number; tokens_out: number; dollars: number };
  beads_touched?: string[];
  transcript?: unknown[];
  ended_at?: string;
}

function seedWalk(): FakeWalk {
  return {
    id: 'walk-001',
    workspace: 'ws-x',
    initiated_by: 'operator',
    primary_persona: 'project-manager',
    started_at: '2026-04-27T12:00:00Z',
    status: 'active',
    agenda: [
      {
        id: 'a',
        topic: 'first agenda item',
        source: { kind: 'escalation', ref: 'e1' },
        priority: 450,
        status: 'active',
        added_at: '2026-04-27T12:00:00Z',
      },
      {
        id: 'b',
        topic: 'second agenda item',
        source: { kind: 'hitl', ref: 's1' },
        priority: 300,
        status: 'queued',
        added_at: '2026-04-27T12:00:00Z',
      },
      {
        id: 'c',
        topic: 'third agenda item',
        source: { kind: 'bead_filed', ref: 'gm-1' },
        priority: 100,
        status: 'queued',
        added_at: '2026-04-27T12:00:00Z',
      },
    ],
    decisions: [],
    beads_touched: [],
    transcript: [],
    cost: { tokens_in: 0, tokens_out: 0, dollars: 0 },
  };
}

// installWalkRoutes wires page.route handlers for the /api/v1/walks/*
// surface. The fake-backend fixture catches /api/* with a default
// `{}` response; per-test page.route handlers register first and
// win, so this overlay drives the walk endpoints without touching
// the shared fixture.
async function installWalkRoutes(page: Page): Promise<void> {
  const state: { walk: FakeWalk } = { walk: seedWalk() };

  const json = (data: unknown, status = 200) => ({
    status,
    contentType: 'application/json',
    body: JSON.stringify(data),
  });

  await page.route('**/api/v1/walks:start', async (route: Route) => {
    state.walk = seedWalk();
    await route.fulfill(json(state.walk, 201));
  });

  await page.route('**/api/v1/walks/walk-001', async (route: Route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill(json(state.walk));
      return;
    }
    await route.fulfill(json(state.walk));
  });

  await page.route('**/api/v1/walks/walk-001/agenda/*', async (route: Route) => {
    const url = route.request().url();
    const itemID = decodeURIComponent(url.split('/agenda/')[1] ?? '');
    if (route.request().method() === 'PATCH') {
      const body = route.request().postDataJSON?.() ?? JSON.parse(route.request().postData() ?? '{}');
      const status = body.status as FakeWalk['agenda'][0]['status'];
      state.walk = {
        ...state.walk,
        agenda: state.walk.agenda.map((i) => (i.id === itemID ? { ...i, status } : i)),
      };
      await route.fulfill(json(state.walk));
      return;
    }
    await route.fulfill(json(state.walk));
  });

  await page.route('**/api/v1/walks/walk-001/decide/*', async (route: Route) => {
    const url = route.request().url();
    const itemID = decodeURIComponent(url.split('/decide/')[1] ?? '');
    const body = route.request().postDataJSON?.() ?? JSON.parse(route.request().postData() ?? '{}');
    const kind = body.kind as string;
    const newStatus: FakeWalk['agenda'][0]['status'] = kind === 'defer' ? 'deferred' : 'decided';
    state.walk = {
      ...state.walk,
      agenda: state.walk.agenda.map((i) =>
        i.id === itemID ? { ...i, status: newStatus } : i
      ),
      decisions: [
        ...(state.walk.decisions ?? []),
        {
          agenda_item_id: itemID,
          kind,
          decided_at: '2026-04-27T12:01:00Z',
          decided_by: 'operator',
        },
      ],
    };
    await route.fulfill(json(state.walk));
  });

  await page.route('**/api/v1/walks/walk-001/end', async (route: Route) => {
    state.walk = { ...state.walk, status: 'completed', ended_at: '2026-04-27T13:00:00Z' };
    await route.fulfill(json(state.walk));
  });

  // The right-pane open-escalations section lists separately; the
  // fixture-level /api/escalations route already returns an empty
  // envelope so no override is needed.
}

test.describe('Gemba walk @chrome', () => {
  test.beforeEach(async ({ page }) => {
    await installWalkRoutes(page);
    await page.goto('/board');
    await page.evaluate(() => window.localStorage.clear());
    await page.reload();
  });

  test('three-pane layout + bottom bar', async ({ page }) => {
    await page.goto('/walk');
    await expect(page.getByTestId('walk-page')).toBeVisible();
    await expect(page.getByTestId('walk-agenda-pane')).toBeVisible();
    await expect(page.getByTestId('walk-chat-pane')).toBeVisible();
    await expect(page.getByTestId('walk-right-pane')).toBeVisible();
    await expect(page.getByTestId('walk-bottom-bar')).toBeVisible();
  });

  test('agenda renders all four lanes and items carry urgency badges', async ({ page }) => {
    await page.goto('/walk');
    await expect(page.getByTestId('walk-agenda-lane-queued')).toBeVisible();
    await expect(page.getByTestId('walk-agenda-lane-active')).toBeVisible();
    await expect(page.getByTestId('walk-agenda-lane-decided')).toBeVisible();
    await expect(page.getByTestId('walk-agenda-lane-deferred')).toBeVisible();

    // Seed item 'a' renders in the active lane with an urgency badge.
    const card = page.getByTestId('walk-agenda-item-a');
    await expect(card).toBeVisible();
    await expect(card).toHaveAttribute('data-lane', 'active');
    await expect(page.getByTestId('walk-agenda-item-a-urgency')).toBeVisible();
  });

  test('R hotkey ratifies the active item and auto-promotes the next', async ({ page }) => {
    await page.goto('/walk');
    await expect(page.getByTestId('walk-agenda-item-a')).toHaveAttribute('data-lane', 'active');

    // Move focus off any text input so the bare 'r' is not eaten.
    await page.locator('[data-testid="walk-chat-pane"]').click();
    // Click might land on the composer input; click the active-frame
    // header instead so focus is on a non-editable element.
    await page.locator('[data-testid="walk-chat-active-frame"]').click();
    await page.keyboard.press('r');

    await expect(page.getByTestId('walk-agenda-item-a')).toHaveAttribute('data-lane', 'decided');
    await expect(page.getByTestId('walk-agenda-item-a-decided')).toBeVisible();
    // Auto-promotion: the next queued item is now active.
    await expect(page.getByTestId('walk-agenda-item-b')).toHaveAttribute('data-lane', 'active');
  });

  test('D hotkey defers the active item (scope: walk)', async ({ page }) => {
    // gm-0tl: walk-defer was reverted to scope 'walk', so 'd' fires
    // synchronously while /walk is mounted. The walk-active scope
    // (n / Enter) is a different surface.
    await page.goto('/walk');
    await expect(page.getByTestId('walk-agenda-item-a')).toHaveAttribute('data-lane', 'active');

    await page.locator('[data-testid="walk-chat-active-frame"]').click();
    await page.keyboard.press('d');

    await expect(page.getByTestId('walk-agenda-item-a')).toHaveAttribute('data-lane', 'deferred');
  });

  test('clicking a queued card promotes it to active (and demotes the previous)', async ({ page }) => {
    await page.goto('/walk');
    await expect(page.getByTestId('walk-agenda-item-a')).toHaveAttribute('data-lane', 'active');

    await page.getByTestId('walk-agenda-item-b').click();
    await expect(page.getByTestId('walk-agenda-item-b')).toHaveAttribute('data-lane', 'active');
    await expect(page.getByTestId('walk-agenda-item-a')).toHaveAttribute('data-lane', 'queued');
  });

  test('bottom-bar fields update after a decision', async ({ page }) => {
    await page.goto('/walk');
    await expect(page.getByTestId('walk-bar-decisions')).toContainText('0');
    await expect(page.getByTestId('walk-bar-beads')).toContainText('0');
    await expect(page.getByTestId('walk-bar-cost')).toContainText('$0.00');
    await expect(page.getByTestId('walk-bar-duration')).toBeVisible();

    await page.locator('[data-testid="walk-chat-active-frame"]').click();
    await page.keyboard.press('r');

    // Decisions count ticks up after ratify.
    await expect(page.getByTestId('walk-bar-decisions')).toContainText('1');
  });

  test('right pane renders the open-escalations section even when empty', async ({ page }) => {
    await page.goto('/walk');
    const section = page.getByTestId('walk-right-escalations');
    await expect(section).toBeVisible();
    // Empty-state copy renders inline.
    await expect(section).toContainText('None outside the agenda.');
    // Budget gauge rides alongside.
    await expect(page.getByTestId('walk-right-budget-dollars')).toContainText('$0.00');
    await expect(page.getByTestId('walk-right-budget-tokens')).toBeVisible();
  });

  test('active-walk banner appears on /walk and persists on other routes', async ({ page }) => {
    await page.goto('/walk');
    await expect(page.getByTestId('walk-banner')).toBeVisible();
    await expect(page.getByTestId('walk-banner')).toContainText('Gemba walk active');
    await expect(page.getByTestId('walk-banner')).toContainText('0 of');

    // Banner persists when the operator navigates away.
    await page.goto('/board');
    await expect(page.getByTestId('walk-banner')).toBeVisible();
  });

  test('end-walk button on the banner clears the walk globally', async ({ page }) => {
    await page.goto('/walk');
    await expect(page.getByTestId('walk-banner')).toBeVisible();

    await page.goto('/board');
    await page.getByTestId('walk-banner-end').click();
    await expect(page.getByTestId('walk-banner')).toBeHidden();
  });

  test('PM panel becomes the walk chat surface when the walk is active', async ({ page }) => {
    await page.goto('/walk');
    // Open the panel from the chevron handle (gm-96l).
    await page.getByTestId('pm-panel-chevron').click();
    // The walk takeover surface renders inside the PM panel.
    await expect(page.getByTestId('pm-panel-walk')).toBeVisible();
    // Regular conversation is replaced.
    await expect(page.getByTestId('pm-panel-conversation')).toHaveCount(0);
  });
});
