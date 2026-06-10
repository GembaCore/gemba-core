// specs/auth/yolo.spec.ts (gm-5v8v.12 / gm-5v8v.12.1).
//
// --dangerously-skip-permissions surfaces in /api/config as the
// `yolo_available` boolean. The SPA reads it to gate yolo-mode
// affordances. The flag must default to false (server default) and
// only flip true when the operator passes the flag at startup.

import { test, expect } from '../../fixtures/server';

test.describe('@deep YOLO availability flag @auth', () => {
  test.skip(({ backend }) => backend !== 'real', 'deep-only — needs a real listener');

  test('/api/config returns yolo_available=false when flag is unset', async ({
    authServer,
  }) => {
    const srv = await authServer({});
    const res = await fetch(`${srv.baseURL}/api/config`);
    expect(res.status).toBe(200);
    const body = (await res.json()) as { yolo_available?: boolean };
    expect(body.yolo_available).toBe(false);
  });

  test('/api/config returns yolo_available=true when flag is set', async ({
    authServer,
  }) => {
    const srv = await authServer({ dangerouslySkipPermissions: true });
    const res = await fetch(`${srv.baseURL}/api/config`);
    expect(res.status).toBe(200);
    const body = (await res.json()) as { yolo_available?: boolean };
    expect(body.yolo_available).toBe(true);
  });
});

test.describe('YOLO fake-mode @auth', () => {
  test.fixme(
    'fake: SPA hides YOLO affordances when /api/config returns yolo_available=false',
    () => {
      /* fixme: requires (a) a YOLO-mode UI affordance in the SPA
         (no such surface today) AND (b) a useConfig hook that reads
         /api/config. When both land, this spec sets the fake's
         /api/config response to {yolo_available: false} and asserts
         the affordance is hidden. */
    }
  );

  test.fixme(
    'fake: SPA exposes YOLO toggle when /api/config returns yolo_available=true',
    () => {
      /* fixme: positive control to (a) above. Same blocker. */
    }
  );
});
