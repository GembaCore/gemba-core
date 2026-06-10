// specs/sessions/end.spec.ts (gm-5v8v.9).
//
// Tier: route. The End button on a session row fires DELETE
// /api/sessions/:id with the X-GEMBA-Confirm nonce gate.

import { expect, test } from '../../fixtures/server';
import { SessionsPage } from '../../pages/SessionsPage';
import { session, resetSessionIds } from '../../builders/session';

test.beforeEach(() => {
  resetSessionIds();
});

test('End button fires DELETE /api/sessions/:id @sessions', async ({
  page,
  sessionPlane,
}) => {
  sessionPlane.seed([session({ id: 'sess-doomed', status: 'working' })]);
  const sp = new SessionsPage(page);
  await sp.goto();
  await expect(sp.row('sess-doomed')).toBeVisible();

  const [delReq] = await Promise.all([
    page.waitForRequest(
      (req) =>
        req.method() === 'DELETE' &&
        /\/api\/sessions\/sess-doomed/.test(req.url())
    ),
    sp.endButton('sess-doomed').click(),
  ]);

  // The endSession helper sends an X-GEMBA-Confirm header for nonce
  // idempotency. Pin that contract so a future helper refactor can't
  // silently drop it (which would break replay safety on retries).
  expect(delReq.headers()['x-gemba-confirm']).toBeTruthy();
});

test('End button is hidden on terminal rows @sessions', async ({ page, sessionPlane }) => {
  sessionPlane.seed([session({ id: 'sess-done', status: 'completed' })]);
  const sp = new SessionsPage(page);
  await sp.goto();
  await expect(sp.row('sess-done')).toBeVisible();
  await expect(sp.endButton('sess-done')).toHaveCount(0);
});

test.fixme(
  '@deep DELETE flows through to bd: pane dies cleanly, no orphan worktree',
  () => {
    /* fixme: deferred to deep mode. The real backend's session
       lifecycle owns this property; the fake just removes from the
       in-memory store. */
  }
);
