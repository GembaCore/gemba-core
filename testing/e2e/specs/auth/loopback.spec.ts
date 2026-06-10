// specs/auth/loopback.spec.ts (gm-5v8v.12 / gm-5v8v.12.1).
//
// Token enforcement is independent of the bind interface (gm-b3 /
// gm-99g): even a server bound to 127.0.0.1 must reject unauthorized
// requests. The naive "loopback is safe" assumption is the exact
// failure mode this spec defends against. All @deep — there's no
// fake-mode equivalent because loopback vs remote-bind only matters
// against a real listener.

import { test, expect } from '../../fixtures/server';

test.describe('@deep Loopback auth enforcement @auth', () => {
  test.skip(({ backend }) => backend !== 'real', 'deep-only — needs a real listener');

  test('--listen=127.0.0.1 + --auth=token still rejects unauthenticated /api/*', async ({
    authServer,
  }) => {
    const srv = await authServer({ auth: 'token', listen: '127.0.0.1' });
    const res = await fetch(`${srv.baseURL}/api/health`);
    expect(res.status).toBe(401);
  });

  test('--listen=127.0.0.1 + --auth=open accepts unauthenticated /api/*', async ({
    authServer,
  }) => {
    const srv = await authServer({ auth: 'open', listen: '127.0.0.1' });
    const res = await fetch(`${srv.baseURL}/api/health`);
    expect(res.status).toBe(200);
  });

  test('--listen=0.0.0.0 + --auth=open is rejected at startup (sanity gate)', async ({
    authServer,
  }) => {
    // gm-99g — open auth on a non-loopback bind must refuse to start.
    // A server-config sanity gate, not an HTTP runtime check.
    const srv = await authServer({
      auth: 'open',
      listen: '0.0.0.0',
      expectBootFailure: true,
    });
    expect(srv.exitCode).not.toBe(0);
    expect(srv.exitCode).not.toBeNull();
    expect(srv.stderr().toLowerCase()).toMatch(/auth|bind|loopback|listen/);
  });
});
