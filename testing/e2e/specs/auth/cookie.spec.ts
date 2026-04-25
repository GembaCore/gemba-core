// specs/auth/cookie.spec.ts (gm-5v8v.12).
//
// Cookie session path: POST /api/auth/login exchanges a valid bearer
// for a signed session cookie (HttpOnly + SameSite=Strict). Subsequent
// requests carrying only the cookie are accepted. Lives at
// internal/server/auth_test.go's TestLogin* — the e2e equivalent
// drives the same dance through a real browser.

import { test } from '../../fixtures/server';

test.describe('Cookie session auth @auth', () => {
  test.fixme(
    '@deep POST /api/auth/login with valid bearer returns 200 + Set-Cookie',
    () => {
      /* fixme: gm-5v8v.2 deep infra. Spec issues POST /api/auth/login
         with Authorization: Bearer <known>, asserts 200, asserts the
         response carries a Set-Cookie matching auth.SessionCookieName
         with HttpOnly + SameSite=Strict + Secure-when-https. The
         middleware test in internal/server/auth_test.go covers the
         server side; this asserts the browser sees + persists the
         cookie. */
    }
  );

  test.fixme(
    '@deep subsequent requests authenticate via cookie alone',
    () => {
      /* fixme: after login, drop the Authorization header and fire
         GET /api/health → must 200. Confirms the cookie path is
         independently sufficient. */
    }
  );

  test.fixme(
    '@deep POST /api/auth/login without bearer returns 401',
    () => {
      /* fixme: anonymous callers can't trigger an unauthenticated
         cookie mint. Pin the negative control. */
    }
  );

  test.fixme(
    '@deep cookie expires; refresh path mints a new one',
    () => {
      /* fixme: the cookie has a TTL; the design intent is a refresh
         path (re-POST /api/auth/login with the bearer) rather than
         a sliding expiry. Confirm that an expired cookie returns
         401 and that re-login works without server restart. */
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
