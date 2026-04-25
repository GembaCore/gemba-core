// specs/auth/loopback.spec.ts (gm-5v8v.12).
//
// Token enforcement is independent of the bind interface (gm-b3 /
// gm-99g): even a server bound to 127.0.0.1 must reject unauthorized
// requests. The naive "loopback is safe" assumption is the exact
// failure mode this spec defends against. All @deep — there's no
// fake-mode equivalent because loopback vs remote-bind only matters
// against a real listener.

import { test } from '../../fixtures/server';

test.describe('Loopback auth enforcement @auth', () => {
  test.fixme(
    '@deep --listen=127.0.0.1 + --auth=token still rejects unauthenticated /api/*',
    () => {
      /* fixme: spawn gemba serve --listen=127.0.0.1 --auth=token,
         hit /api/health from the same machine without a bearer →
         expect 401, not 200. The "loopback is implicitly trusted"
         path was explicitly rejected in gm-b3 / gm-99g and is
         covered by Go-side tests in internal/server/auth_test.go;
         this is the e2e equivalent verifying the binding doesn't
         leak the policy. */
    }
  );

  test.fixme(
    '@deep --listen=127.0.0.1 + --auth=open accepts unauthenticated /api/*',
    () => {
      /* fixme: positive control for the rule — when auth is OPEN
         (the dev default), loopback access is permitted. Without
         this control, a misconfigured fixture that 401s loopback
         would let the negative test pass for the wrong reason. */
    }
  );

  test.fixme(
    '@deep --listen=0.0.0.0 + --auth=open is rejected at startup (sanity gate)',
    () => {
      /* fixme: gm-99g's spec is "open auth on a non-loopback bind
         must refuse to start" — a server-config sanity gate, not
         an HTTP runtime check. The e2e test asserts the server's
         exit code + stderr message. Belongs here because it's
         part of the loopback-vs-remote auth-enforcement story. */
    }
  );
});
