// specs/auth/cookie.spec.ts (gm-5v8v.12 / gm-5v8v.12.1).
//
// Cookie session path: POST /api/auth/login exchanges a valid bearer
// for a signed session cookie. Subsequent requests carrying only the
// cookie are accepted. Server-side coverage lives in
// internal/server/auth_test.go's TestLogin*; this is the e2e
// equivalent driving the same dance through HTTP.

import { test, expect } from '../../fixtures/server';

test.describe('@deep Cookie session auth @auth', () => {
  test.skip(({ backend }) => backend !== 'real', 'deep-only — needs a real listener');

  test('POST /api/auth/login with valid bearer returns 200 + Set-Cookie', async ({
    authServer,
  }) => {
    const srv = await authServer({ auth: 'token' });
    const res = await fetch(`${srv.baseURL}/api/auth/login`, {
      method: 'POST',
      headers: { Authorization: `Bearer ${srv.authToken}` },
    });
    expect(res.status).toBe(200);
    const setCookie = res.headers.get('set-cookie') ?? '';
    expect(setCookie).toMatch(/HttpOnly/i);
    expect(setCookie.toLowerCase()).toMatch(/samesite=strict/);
  });

  test('POST /api/auth/login without bearer returns 401', async ({ authServer }) => {
    const srv = await authServer({ auth: 'token' });
    const res = await fetch(`${srv.baseURL}/api/auth/login`, { method: 'POST' });
    expect(res.status).toBe(401);
  });

  test('subsequent requests authenticate via cookie alone', async ({ authServer }) => {
    const srv = await authServer({ auth: 'token' });

    const login = await fetch(`${srv.baseURL}/api/auth/login`, {
      method: 'POST',
      headers: { Authorization: `Bearer ${srv.authToken}` },
    });
    expect(login.status).toBe(200);
    const setCookie = login.headers.get('set-cookie');
    expect(setCookie).toBeTruthy();

    // Strip the directives; we just want name=value for the next call.
    const cookie = (setCookie ?? '').split(';')[0] ?? '';
    expect(cookie).toBeTruthy();

    // No Authorization header — the cookie must be sufficient on its own.
    const res = await fetch(`${srv.baseURL}/api/health`, {
      headers: { Cookie: cookie },
    });
    expect(res.status).toBe(200);
  });
});

test.describe('Cookie expiry / SPA persistence @auth', () => {
  test.fixme(
    '@deep cookie expires; refresh path mints a new one',
    () => {
      /* fixme: the cookie has a TTL; the design intent is a refresh
         path (re-POST /api/auth/login with the bearer) rather than a
         sliding expiry. Needs a server-side knob for the cookie's
         max-age small enough to assert in e2e — out of scope until
         that lands. */
    }
  );

  test.fixme(
    'fake: SPA persists session cookie across navigations',
    () => {
      /* fixme: the LoginPage hasn't shipped — once it does, this
         spec drives the form, confirms the cookie lands, navigates
         to /board, and confirms the SPA carries the cookie via
         Playwright's BrowserContext (no manual storage). */
    }
  );
});
