// specs/auth/yolo.spec.ts (gm-5v8v.12).
//
// --dangerously-skip-permissions surfaces in /api/config as the
// `yolo_available` boolean. The SPA reads it to gate yolo-mode
// affordances (the dev "skip every confirmation" toggle). The flag
// must default to false (server default) and only flip true when
// the operator passes --dangerously-skip-permissions at startup.

import { test } from '../../fixtures/server';

test.describe('YOLO availability flag @auth', () => {
  test.fixme(
    '@deep /api/config returns yolo_available=false when flag is unset',
    () => {
      /* fixme: spawn gemba serve without --dangerously-skip-permissions,
         GET /api/config → assert body.yolo_available === false. The
         server's default is the safe one; this pins it. */
    }
  );

  test.fixme(
    '@deep /api/config returns yolo_available=true when flag is set',
    () => {
      /* fixme: spawn gemba serve --dangerously-skip-permissions,
         GET /api/config → body.yolo_available === true. Positive
         control. */
    }
  );

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
