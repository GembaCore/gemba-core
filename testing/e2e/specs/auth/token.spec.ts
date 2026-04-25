// specs/auth/token.spec.ts (gm-5v8v.12).
//
// Bearer-token auth path (internal/server/auth.go, gm-b3 / gm-99g):
// every /api/* and /events request must reject before route lookup
// when a missing or invalid bearer is presented in --auth=token mode.
//
// The bead notes auth is a backend concern; the meaningful tests are
// all @deep. Fake-mode coverage is bounded — there's no LoginPage in
// the SPA today, so there's no UI-side surface to assert against.

import { test } from '../../fixtures/server';

test.describe('Bearer token auth @auth', () => {
  test.fixme(
    '@deep missing Authorization on /api/* under --auth=token returns 401',
    () => {
      /* fixme: gm-5v8v.2 deep infra wires --auth=token; the test
         spawns gemba serve with a known token, fires GET /api/health
         without a bearer, asserts 401 + envelope error="unauthorized".
         The 401 must come from middleware (before the route's handler
         runs); pin via the response code AND the absence of any route-
         specific body shape. */
    }
  );

  test.fixme(
    '@deep invalid Authorization returns 401 (token mismatch)',
    () => {
      /* fixme: same path with a wrong bearer. Distinguishes the
         "token format wrong" vs "token doesn't match" cases — both
         should land 401, neither should leak which token the server
         holds. */
    }
  );

  test.fixme(
    '@deep valid bearer on /api/* returns 200 (positive control)',
    () => {
      /* fixme: positive control for the previous two — without it,
         a misconfigured fixture that 401s every request would let
         the negative tests pass for the wrong reason. */
    }
  );

  test.fixme(
    '@deep /events stream rejects without a bearer in token mode',
    () => {
      /* fixme: the SSE endpoint is auth-mounted alongside /api/*.
         Same middleware, same 401, but EventSource clients see the
         rejection as a connection error. Confirm the wire-level
         status. */
    }
  );

  test.fixme(
    'fake: SPA carries Authorization header on every /api/* request when bearer is set',
    () => {
      /* fixme: requires the SPA to read a configured bearer from
         somewhere (login form, env, query string) and forward it
         on every request. Today api/client.ts doesn't add an
         Authorization header — auth is open in dev. The SPA-side
         surface lands when LoginPage / an auth context provider
         arrives. */
    }
  );
});
