// specs/sessions/escalation.spec.ts (gm-5v8v.9).
//
// Tier: route. EscalationPanel renders when the operator clicks the
// per-session escalation badge; Approve/Deny buttons fire POST
// /api/escalations/:id/respond.

import { expect, test } from '../../fixtures/server';
import { SessionsPage } from '../../pages/SessionsPage';
import { session, resetSessionIds } from '../../builders/session';
import { escalation, resetEscalationIds } from '../../builders/escalation';

test.beforeEach(() => {
  resetSessionIds();
  resetEscalationIds();
});

test('clicking the escalation badge opens EscalationPanel @sessions', async ({
  page,
  sessionPlane,
  escalationPlane,
}) => {
  sessionPlane.seed([session({ id: 'sess-A', assignment_id: 'gm-99', status: 'working' })]);
  escalationPlane.seed([
    escalation({
      id: 'esc-A',
      assignment_id: 'gm-99',
      title: 'Approve write?',
      prompt: 'Agent wants to commit',
      state: 'open',
    }),
  ]);
  const sp = new SessionsPage(page);
  await sp.goto();
  await sp.escalationsBadge('sess-A').click();
  await expect(page.getByTestId('escalation-panel')).toBeVisible();
  await expect(page.getByTestId('escalation-row-esc-A')).toBeVisible();
  await expect(page.getByTestId('escalation-row-esc-A')).toContainText(/Approve write/i);
});

test('Approve POSTs respond with kind=approve @sessions', async ({
  page,
  sessionPlane,
  escalationPlane,
}) => {
  sessionPlane.seed([session({ id: 'sess-A', assignment_id: 'gm-99', status: 'working' })]);
  escalationPlane.seed([
    escalation({ id: 'esc-A', assignment_id: 'gm-99', state: 'open' }),
  ]);
  const sp = new SessionsPage(page);
  await sp.goto();
  await sp.escalationsBadge('sess-A').click();

  const [postReq] = await Promise.all([
    page.waitForRequest(
      (req) =>
        req.method() === 'POST' &&
        /\/api\/escalations\/esc-A\/respond/.test(req.url())
    ),
    page.getByTestId('escalation-row-esc-A-approve').click(),
  ]);
  expect(postReq.postDataJSON()).toMatchObject({ kind: 'approve' });
  // Replay safety: the respond helper carries an X-GEMBA-Confirm
  // header. Pin the contract so a refactor can't silently drop it.
  expect(postReq.headers()['x-gemba-confirm']).toBeTruthy();

  // The fake fixture flips the escalation to resolved; the panel's
  // store list re-reads on the next /api/escalations fetch (driven by
  // react-query). After resolution the row's resolved_at should be
  // populated in the store — assert the round-trip via the fixture
  // handle rather than a SPA selector since the panel UI doesn't
  // rerender resolved rows in a stable way today.
  expect(escalationPlane.responses()).toContainEqual(
    expect.objectContaining({ id: 'esc-A', body: expect.objectContaining({ kind: 'approve' }) })
  );
});

test('Deny POSTs respond with kind=deny @sessions', async ({
  page,
  sessionPlane,
  escalationPlane,
}) => {
  sessionPlane.seed([session({ id: 'sess-A', assignment_id: 'gm-99', status: 'working' })]);
  escalationPlane.seed([
    escalation({ id: 'esc-A', assignment_id: 'gm-99', state: 'open' }),
  ]);
  const sp = new SessionsPage(page);
  await sp.goto();
  await sp.escalationsBadge('sess-A').click();

  const [postReq] = await Promise.all([
    page.waitForRequest(
      (req) =>
        req.method() === 'POST' &&
        /\/api\/escalations\/esc-A\/respond/.test(req.url())
    ),
    page.getByTestId('escalation-row-esc-A-deny').click(),
  ]);
  expect(postReq.postDataJSON()).toMatchObject({ kind: 'deny' });
});
