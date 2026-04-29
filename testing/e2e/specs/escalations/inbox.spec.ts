// specs/escalations/inbox.spec.ts (gm-e11.8.1).
//
// Tier: route. /escalations inbox renders escalations grouped by
// severity; the per-card Resolve button opens the nonce-confirmed modal
// and POSTs /api/escalations/:id/respond. After a successful resolve
// the escalation drops out of the list (the fake plane flips state to
// "resolved" and the SPA's react-query refetch filters it out).

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
