// specs/sessions/list.spec.ts (gm-5v8v.9).
//
// Tier: route. SessionsPage list rendering — empty state, populated
// rows, status pills, escalation badges. Status transition specs that
// depend on SSE pushes are fixme'd until gm-5v8v.10 lands the realtime
// fixture; here we cover the rendering surface and the cache-driven
// re-render path.

import { expect, test } from '../../fixtures/server';
import { SessionsPage } from '../../pages/SessionsPage';
import { session, resetSessionIds } from '../../builders/session';
import { escalation, resetEscalationIds } from '../../builders/escalation';

test.beforeEach(() => {
  resetSessionIds();
  resetEscalationIds();
});

test('empty state renders when no sessions exist @sessions', async ({
  page,
  sessionPlane,
}) => {
  sessionPlane.seed([]);
  const sp = new SessionsPage(page);
  await sp.goto();
  await expect(sp.empty).toBeVisible();
  await expect(sp.empty).toContainText(/No live sessions/i);
});

test('one row renders per seeded session with status pill @sessions', async ({
  page,
  sessionPlane,
}) => {
  sessionPlane.seed([
    session({ id: 'sess-A', status: 'ready', assignment_id: 'gm-1' }),
    session({ id: 'sess-B', status: 'working', assignment_id: 'gm-2' }),
    session({ id: 'sess-C', status: 'prompting', assignment_id: 'gm-3' }),
  ]);
  const sp = new SessionsPage(page);
  await sp.goto();
  await expect(sp.row('sess-A')).toBeVisible();
  await expect(sp.row('sess-B')).toBeVisible();
  await expect(sp.row('sess-C')).toBeVisible();
  await expect(sp.status('sess-A')).toContainText('ready');
  await expect(sp.status('sess-B')).toContainText('working');
  await expect(sp.status('sess-C')).toContainText('prompting');
});

test('row carries data-status matching the session.status @sessions', async ({
  page,
  sessionPlane,
}) => {
  // Pinning the data-status attribute lets a downstream visual-regression
  // test target a row by status without parsing pill text. Cheap to
  // maintain — the SPA writes it once per row.
  sessionPlane.seed([session({ id: 'sess-A', status: 'working' })]);
  const sp = new SessionsPage(page);
  await sp.goto();
  await expect(sp.row('sess-A')).toHaveAttribute('data-status', 'working');
});

test('open escalations show a count badge on the row @sessions', async ({
  page,
  sessionPlane,
  escalationPlane,
}) => {
  sessionPlane.seed([session({ id: 'sess-A', assignment_id: 'gm-99', status: 'working' })]);
  escalationPlane.seed([
    escalation({ id: 'esc-1', assignment_id: 'gm-99', state: 'open' }),
    escalation({ id: 'esc-2', assignment_id: 'gm-99', state: 'open' }),
    // resolved → does NOT count
    escalation({ id: 'esc-3', assignment_id: 'gm-99', state: 'resolved' }),
  ]);
  const sp = new SessionsPage(page);
  await sp.goto();
  await expect(sp.escalationsBadge('sess-A')).toBeVisible();
  await expect(sp.escalationsBadge('sess-A')).toContainText('2');
});

test('terminal sessions sort below live ones @sessions', async ({ page, sessionPlane }) => {
  sessionPlane.seed([
    session({
      id: 'sess-old',
      status: 'completed',
      started_at: '2026-04-25T08:00:00Z',
      ended_at: '2026-04-25T09:00:00Z',
    }),
    session({ id: 'sess-live', status: 'working', started_at: '2026-04-25T07:00:00Z' }),
  ]);
  const sp = new SessionsPage(page);
  await sp.goto();
  // The live row must come before the completed one in DOM order.
  const ids = await sp.table
    .locator('tbody tr')
    .evaluateAll((rows) => rows.map((r) => (r as HTMLElement).dataset.testid ?? ''));
  expect(ids).toEqual(['session-row-sess-live', 'session-row-sess-old']);
});

test.fixme(
  'live session.transition events reorder rows within event lag @sessions @realtime',
  () => {
    /* fixme: realtime SSE invalidation lives in gm-5v8v.10. The
       sessions list refetches on session.transition / session.state_reported
       — when the fake fixture grows an event-emit method, this
       becomes a real test. */
  }
);
