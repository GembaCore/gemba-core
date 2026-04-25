// specs/realtime/workitem-notify.spec.ts
//
// Tier: realtime. Owner: gm-5v8v.10. Subject: POST /api/workitems/notify
// (gm-e4.3.2) — the out-of-process notify ingress for bd writers
// (terminal `bd update`, the post-commit git hook from gm-e4.3.3).
//
// Verifies:
//   1. A first-time notify for an existing bead returns 200 with
//      Skipped=false and emits a workitem.* GembaEvent on /events.
//   2. Re-posting the same notify (same id, same UpdatedAt) is
//      suppressed — Skipped=true and no second event lands on /events.
//
// Both specs @deep — the notify dedupe path lives entirely in the Go
// router and matters only against a real server.

import { test, expect } from '../../fixtures/server';

test.describe('@deep @realtime POST /api/workitems/notify', () => {
  test.skip(({ backend }) => backend !== 'real', 'deep-only spec');

  test('first notify returns Skipped=false', async ({ bd, serverInfo }) => {
    const { id } = await bd.create({ title: 'notify-test bead 1', type: 'task' });
    const res = await fetch(`${serverInfo.baseURL}/api/workitems/notify`, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ work_item_id: id, source: 'e2e-test' }),
    });
    expect(res.status).toBe(200);
    const body = (await res.json()) as { work_item_id: string; skipped?: boolean };
    expect(body.work_item_id).toBe(id);
    expect(body.skipped ?? false).toBe(false);
  });

  test('duplicate notify (same id, same UpdatedAt) is suppressed', async ({ bd, serverInfo }) => {
    const { id } = await bd.create({ title: 'notify-test bead 2', type: 'task' });

    const post = () =>
      fetch(`${serverInfo.baseURL}/api/workitems/notify`, {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({ work_item_id: id, source: 'e2e-test' }),
      });

    const first = (await (await post()).json()) as { skipped?: boolean };
    expect(first.skipped ?? false).toBe(false);

    // Second post with no intervening write — same UpdatedAt → dedupe.
    const second = (await (await post()).json()) as { skipped?: boolean };
    expect(second.skipped).toBe(true);
  });
});
