// specs/auth/token.spec.ts (gm-5v8v.12 / gm-5v8v.12.1).
//
// Bearer-token auth path (internal/server/auth.go, gm-b3 / gm-99g):
// every /api/* and /events request must reject before route lookup
// when a missing or invalid bearer is presented in --auth=token mode.
//
// gm-5v8v.12.1 lands the authServer fixture: each test spins a fresh
// gemba serve --auth=token in its own tempdir, captures the
// bootstrapped token from stderr, and exercises the middleware
// against a known-good credential.

import { test, expect } from '../../fixtures/server';

test.describe('@deep Bearer token auth @auth', () => {
  test.skip(({ backend }) => backend !== 'real', 'deep-only — needs a real listener');

  test('missing Authorization on /api/* under --auth=token returns 401', async ({ authServer }) => {
    const srv = await authServer({ auth: 'token' });
    const res = await fetch(`${srv.baseURL}/api/health`);
    expect(res.status).toBe(401);
  });

  test('invalid Authorization returns 401 (token mismatch)', async ({ authServer }) => {
    const srv = await authServer({ auth: 'token' });
    const res = await fetch(`${srv.baseURL}/api/health`, {
      headers: { Authorization: 'Bearer not-the-right-token' },
    });
    expect(res.status).toBe(401);
  });

  test('valid bearer on /api/* returns 200 (positive control)', async ({ authServer }) => {
    const srv = await authServer({ auth: 'token' });
    expect(srv.authToken).toBeTruthy();
    const res = await fetch(`${srv.baseURL}/api/health`, {
      headers: { Authorization: `Bearer ${srv.authToken}` },
    });
    expect(res.status).toBe(200);
  });

  test('/events stream rejects without a bearer in token mode', async ({ authServer }) => {
    // The SSE endpoint sits under the same auth middleware as /api/*.
    // Confirm the wire-level status; EventSource clients see 401 as a
    // connection error, but raw fetch lets us pin the actual code.
    const srv = await authServer({ auth: 'token' });
    const res = await fetch(`${srv.baseURL}/events`);
    expect(res.status).toBe(401);
    await res.body?.cancel().catch(() => {});
  });
});

test.describe('Bearer token fake-mode @auth', () => {
  test.fixme(
    'fake: SPA carries Authorization header on every /api/* request when bearer is set',
    () => {
      /* fixme: requires the SPA to read a configured bearer from
         somewhere (login form, env, query string) and forward it on
         every request. Today api/client.ts doesn't add an
         Authorization header — auth is open in dev. The SPA-side
         surface lands when LoginPage / an auth context provider
         arrives. */
    }
  );
});
