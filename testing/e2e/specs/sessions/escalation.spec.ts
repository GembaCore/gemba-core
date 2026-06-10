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
      // session_id must match the session's id so the fake filter
      // mirrors ListPendingRequests (gm-e11.8.6).
      session_id: 'sess-A',
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
    escalation({ id: 'esc-A', session_id: 'sess-A', assignment_id: 'gm-99', state: 'open' }),
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
    escalation({ id: 'esc-A', session_id: 'sess-A', assignment_id: 'gm-99', state: 'open' }),
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

// gm-e11.8.6: per-session filter. The fake dispatcher must read
// ?session_id= (not ?assignment_id=) to match the real Go server. Seed
// escalations belonging to two different sessions; opening the panel
// for sess-B should show only that session's escalation.
test('EscalationPanel shows only escalations belonging to the opened session @sessions', async ({
  page,
  sessionPlane,
  escalationPlane,
}) => {
  sessionPlane.seed([
    session({ id: 'sess-B', assignment_id: 'gm-100', status: 'working' }),
    session({ id: 'sess-C', assignment_id: 'gm-101', status: 'working' }),
  ]);
  escalationPlane.seed([
    // Belongs to sess-B / gm-100. session_id must match the session's
    // id so the fake's ?session_id= filter mirrors ListPendingRequests.
    escalation({ id: 'esc-B1', session_id: 'sess-B', assignment_id: 'gm-100', title: 'Session B question', state: 'open' }),
    escalation({ id: 'esc-B2', session_id: 'sess-B', assignment_id: 'gm-100', title: 'Session B blocker', state: 'open' }),
    // Belongs to sess-C / gm-101 — must NOT appear in sess-B's panel.
    escalation({ id: 'esc-C1', session_id: 'sess-C', assignment_id: 'gm-101', title: 'Session C question', state: 'open' }),
  ]);

  const sp = new SessionsPage(page);
  await sp.goto();

  // Capture the per-session escalation fetch BEFORE clicking the badge
  // so waitForRequest sees the request when it fires.
  const escalationFetchPromise = page.waitForRequest(
    (req) =>
      req.method() === 'GET' &&
      /\/api\/escalations/.test(req.url()) &&
      new URL(req.url()).searchParams.has('session_id'),
    { timeout: 10_000 },
  );

  // Open the escalation panel for sess-B and assert only B's rows are shown.
  await sp.escalationsBadge('sess-B').click();
  await expect(page.getByTestId('escalation-panel')).toBeVisible();
  await expect(page.getByTestId('escalation-row-esc-B1')).toBeVisible();
  await expect(page.getByTestId('escalation-row-esc-B2')).toBeVisible();
  // sess-C's escalation must not appear.
  await expect(page.getByTestId('escalation-row-esc-C1')).toHaveCount(0);

  // Verify the GET request carried ?session_id=sess-B (not ?assignment_id=).
  const escalationFetch = await escalationFetchPromise;
  const fetchURL = new URL(escalationFetch.url());
  expect(fetchURL.searchParams.get('session_id')).toBe('sess-B');
  expect(fetchURL.searchParams.get('assignment_id')).toBeNull();
});
