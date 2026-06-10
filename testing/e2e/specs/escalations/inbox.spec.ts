// specs/escalations/inbox.spec.ts (gm-e11.8.1 / gm-e11.8.3 / gm-e11.8.4 / gm-e11.8.5).
//
// Tier: route. /escalations inbox renders escalations grouped by
// severity; the per-card Resolve button opens the nonce-confirmed modal
// and POSTs /api/escalations/:id/respond. After a successful resolve
// the escalation drops out of the list (the fake plane flips state to
// "resolved" and the SPA's react-query refetch filters it out).
//
// gm-e11.8.3 additions: multi-select toolbar and bulk-dismiss flow.
// gm-e11.8.4 additions: per-card Hand-off → POST /api/consults; escalation
//   stays in the inbox (does NOT resolve).
// gm-e11.8.5 additions: Kind filter pill reduces visible set to matching kinds.

import { expect, test } from '../../fixtures/server';
import { EscalationsPage } from '../../pages/EscalationsPage';
import { escalation, resetEscalationIds } from '../../builders/escalation';

test.beforeEach(() => {
  resetEscalationIds();
});

test('renders escalations grouped by severity, critical before high @escalations', async ({
  page,
  escalationPlane,
}) => {
  escalationPlane.seed([
    // critical: permission_prompt + blocking
    escalation({
      id: 'esc-crit',
      source: 'permission_prompt',
      urgency: 'blocking',
      title: 'Approve write?',
      created_at: '2026-04-25T11:00:00Z',
    }),
    // high: witness_finding (advisory still escalates to high)
    escalation({
      id: 'esc-high',
      source: 'witness_finding',
      urgency: 'advisory',
      title: 'Witness disagrees',
      created_at: '2026-04-25T10:00:00Z',
    }),
  ]);

  const ep = new EscalationsPage(page);
  await ep.goto();

  await expect(ep.section('critical')).toBeVisible();
  await expect(ep.section('high')).toBeVisible();
  await expect(ep.card('esc-crit')).toBeVisible();
  await expect(ep.card('esc-high')).toBeVisible();

  // Critical section appears in the DOM before the high section.
  const sections = await page
    .locator('[data-testid^="escalations-section-"]')
    .evaluateAll((els) => els.map((e) => e.getAttribute('data-testid')));
  expect(sections.indexOf('escalations-section-critical')).toBeLessThan(
    sections.indexOf('escalations-section-high')
  );
});

test('Resolve → Confirm POSTs respond and the escalation drops out of the list @escalations', async ({
  page,
  escalationPlane,
}) => {
  escalationPlane.seed([
    escalation({
      id: 'esc-A',
      source: 'permission_prompt',
      urgency: 'blocking',
      title: 'Approve write?',
      created_at: '2026-04-25T12:00:00Z',
    }),
  ]);

  const ep = new EscalationsPage(page);
  await ep.goto();
  await expect(ep.card('esc-A')).toBeVisible();

  await ep.resolveButton('esc-A').click();
  await expect(ep.modal()).toBeVisible();

  // approve is the default — confirm directly.
  const [postReq] = await Promise.all([
    page.waitForRequest(
      (req) =>
        req.method() === 'POST' &&
        /\/api\/escalations\/esc-A\/respond/.test(req.url())
    ),
    ep.confirmButton().click(),
  ]);
  expect(postReq.postDataJSON()).toMatchObject({ kind: 'approve' });
  // The respond helper carries an X-GEMBA-Confirm nonce — keep the
  // contract pinned so a refactor can't silently drop it.
  expect(postReq.headers()['x-gemba-confirm']).toBeTruthy();

  // After resolve the row disappears from the inbox: the fake plane
  // flips state to "resolved" and the SPA's react-query refetch hides
  // non-open rows.
  await expect(ep.card('esc-A')).toHaveCount(0);
  await expect(ep.empty).toBeVisible();
});

test('multi-select 2 escalations, bulk-dismiss, both drop out of the inbox @escalations', async ({
  page,
  escalationPlane,
}) => {
  escalationPlane.seed([
    escalation({
      id: 'esc-bulk-1',
      source: 'permission_prompt',
      urgency: 'blocking',
      title: 'Bulk item one',
      created_at: '2026-04-25T12:00:00Z',
    }),
    escalation({
      id: 'esc-bulk-2',
      source: 'permission_prompt',
      urgency: 'blocking',
      title: 'Bulk item two',
      created_at: '2026-04-25T11:00:00Z',
    }),
  ]);

  const ep = new EscalationsPage(page);
  await ep.goto();

  // Both cards visible, toolbar absent.
  await expect(ep.card('esc-bulk-1')).toBeVisible();
  await expect(ep.card('esc-bulk-2')).toBeVisible();
  await expect(ep.bulkToolbar()).toHaveCount(0);

  // Select both.
  await ep.checkbox('esc-bulk-1').check();
  await expect(ep.bulkToolbar()).toBeVisible();
  await ep.checkbox('esc-bulk-2').check();

  // Bulk dismiss — capture both POST requests.
  const [req1, req2] = await Promise.all([
    page.waitForRequest(
      (req) =>
        req.method() === 'POST' &&
        /\/api\/escalations\/esc-bulk-[12]\/respond/.test(req.url())
    ),
    page.waitForRequest(
      (req) =>
        req.method() === 'POST' &&
        /\/api\/escalations\/esc-bulk-[12]\/respond/.test(req.url())
    ),
    ep.bulkDismissButton().click(),
  ]);

  // Both requests carry kind='defer'.
  expect(req1.postDataJSON()).toMatchObject({ kind: 'defer' });
  expect(req2.postDataJSON()).toMatchObject({ kind: 'defer' });

  // After the fake plane marks both as resolved, the SPA refetches and
  // both cards drop out of the open inbox.
  await expect(ep.card('esc-bulk-1')).toHaveCount(0);
  await expect(ep.card('esc-bulk-2')).toHaveCount(0);
  await expect(ep.empty).toBeVisible();
});

test('Hand-off → pick persona → confirm → /api/consults POSTs; escalation row stays @escalations', async ({
  page,
  escalationPlane,
}) => {
  escalationPlane.seed([
    escalation({
      id: 'esc-ho',
      source: 'question',
      urgency: 'advisory',
      title: 'Which approach?',
      created_at: '2026-04-25T12:00:00Z',
    }),
  ]);

  const ep = new EscalationsPage(page);
  await ep.goto();
  await expect(ep.card('esc-ho')).toBeVisible();

  // Open hand-off modal.
  await ep.handoffButton('esc-ho').click();
  await expect(ep.handoffModal()).toBeVisible();

  // Confirm with whatever persona is pre-selected — capture the POST.
  const [consultReq] = await Promise.all([
    page.waitForRequest(
      (req) =>
        req.method() === 'POST' &&
        /\/api\/consults$/.test(new URL(req.url()).pathname)
    ),
    ep.handoffConfirmButton().click(),
  ]);

  // The consult POST body carries the expected fields.
  const body = consultReq.postDataJSON() as Record<string, unknown>;
  expect(typeof body.persona_id).toBe('string');
  expect(body.skill_id).toBe('escalation_handoff');
  expect(body.workspace).toBe('default');
  // Nonce header required.
  expect(consultReq.headers()['x-gemba-confirm']).toBeTruthy();

  // Success banner appears inside the modal.
  await expect(ep.handoffSuccess()).toBeVisible();

  // The escalation card is still in the inbox (hand-off does NOT resolve).
  await expect(ep.card('esc-ho')).toBeVisible();
});

test('Hand-off → Cancel closes modal without POSTing @escalations', async ({
  page,
  escalationPlane,
}) => {
  escalationPlane.seed([
    escalation({
      id: 'esc-hc',
      source: 'question',
      urgency: 'advisory',
      title: 'Just curious',
      created_at: '2026-04-25T11:00:00Z',
    }),
  ]);

  const ep = new EscalationsPage(page);
  await ep.goto();
  await expect(ep.card('esc-hc')).toBeVisible();

  await ep.handoffButton('esc-hc').click();
  await expect(ep.handoffModal()).toBeVisible();

  // Track any consult POST that might fire after cancel.
  let consultFired = false;
  page.on('request', (req) => {
    if (req.method() === 'POST' && /\/api\/consults/.test(req.url())) {
      consultFired = true;
    }
  });

  await ep.handoffCancelButton().click();
  await expect(ep.handoffModal()).toHaveCount(0);

  // No consult POST fired.
  expect(consultFired).toBe(false);

  // Escalation card still in the inbox.
  await expect(ep.card('esc-hc')).toBeVisible();
});

// ── gm-e11.8.5: filter bar ────────────────────────────────────────────────────

test('Kind filter pill: selecting one kind hides non-matching escalations @escalations', async ({
  page,
  escalationPlane,
}) => {
  escalationPlane.seed([
    escalation({
      id: 'esc-blocker',
      source: 'blocker',
      urgency: 'advisory',
      title: 'A blocker item',
      created_at: '2026-04-25T12:00:00Z',
    }),
    escalation({
      id: 'esc-question',
      source: 'question',
      urgency: 'advisory',
      title: 'Just a question',
      created_at: '2026-04-25T11:00:00Z',
    }),
  ]);

  const ep = new EscalationsPage(page);
  await ep.goto();

  // Both cards visible before filtering.
  await expect(ep.card('esc-blocker')).toBeVisible();
  await expect(ep.card('esc-question')).toBeVisible();

  // Open the Kind popover and select 'blocker'.
  await ep.kindFilterTrigger().click();
  await ep.kindFilterOption('blocker').click();

  // Only the blocker card should now be visible.
  await expect(ep.card('esc-blocker')).toBeVisible();
  await expect(ep.card('esc-question')).toHaveCount(0);
});
