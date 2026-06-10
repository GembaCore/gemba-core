// fixtures/auth.ts (gm-5v8v.12).
//
// Auth is a backend concern: the bearer-or-cookie middleware is wired
// in internal/server/auth.go (gm-b3 / gm-99g / gm-e5.2), and the
// only meaningful e2e variants are @deep tests against a real gemba
// serve instance. The SPA has no login UI yet (no LoginPage, no
// auth-mode banner); fake-mode tests are limited to the few wire
// surfaces a route() handler can fake.
//
// The handle shape below is what specs will reach for once the SPA
// grows a login form. Today the methods are best-effort: in fake
// mode they stash a token on the fixture for a future API call;
// in real mode (gm-5v8v.2 deep backend), they will exchange the
// bearer for a cookie via POST /api/auth/login. The deep wiring is
// out of scope here — this fixture exposes the surface so spec
// authors can target it without each spec reinventing the dance.

import type { Page } from '@playwright/test';

export type Role = 'overseer' | 'mayor' | 'crew' | 'witness';

export interface AuthHandle {
  /** Bearer token currently in scope, or null if anonymous. */
  bearer(): string | null;
  /**
   * Set a bearer token for subsequent requests. Specs targeting
   * the token-auth path call this BEFORE navigating; the page
   * fixture installs the Authorization header through extraHTTPHeaders.
   */
  setBearer(token: string | null): void;
  /**
   * Login the page session as a role. In fake mode this just stashes
   * the role for later assertions; in real mode (deferred), this will
   * POST /api/auth/login and capture the resulting session cookie.
   */
  loginAs(role: Role, page: Page): Promise<void>;
}

export function createAuthHandle(): AuthHandle {
  let bearer: string | null = null;
  return {
    bearer: () => bearer,
    setBearer: (token) => {
      bearer = token;
    },
    loginAs: async (_role, _page) => {
      // Real-mode wiring deferred: POST /api/auth/login carrying the
      // bearer, capture Set-Cookie. For now this is a no-op so spec
      // authors can call it without a runtime error — the auth specs
      // that depend on real cookie behaviour are all fixme'd to @deep.
    },
  };
}
