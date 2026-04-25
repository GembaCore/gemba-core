// specs/error/404.spec.ts — gm-5v8v.13
//
// Two halves of the 404 contract from ui-spec §6.5:
//
//   1. Unknown SPA routes render NotFoundPage inside AppShell (no
//      crash, no fallback-to-empty). Fake mode covers this — the
//      router doesn't need a real backend to dispatch.
//
//   2. Unknown /api/* and /events/* paths return a structured JSON
//      envelope ({error, path, method}) per gm-b2 / gm-xke instead
//      of chi's default text/plain. @deep — the contract lives in
//      the Go router and matters only against a real server.

import { test, expect } from '../../fixtures/server';

test.describe('@error 404 handling', () => {
  test('unknown route renders NotFoundPage', async ({ page, consoleErrors }) => {
    await page.goto('/this-route-does-not-exist');
    await expect(page.getByTestId('not-found-page')).toBeVisible();
    await expect(page.getByText('Not found', { exact: false })).toBeVisible();
    expect(consoleErrors).toEqual([]);
  });

  test('unknown route under /board still routes to NotFound (not a crash)', async ({
    page,
    consoleErrors,
  }) => {
    // The /board/* wildcard route hands the splat to BoardPage, which
    // only opens the drawer for known epic ids. Routes outside /board
    // fall through to NotFoundPage cleanly.
    await page.goto('/no-such-area/sub/path');
    await expect(page.getByTestId('not-found-page')).toBeVisible();
    expect(consoleErrors).toEqual([]);
  });

  test('@deep unknown /api/* path returns JSON envelope', async ({ serverInfo }) => {
    test.skip(serverInfo.backend !== 'real', 'deep-only: real server enforces gm-b2');

    const res = await fetch(`${serverInfo.baseURL}/api/no-such-endpoint`);
    expect(res.status).toBe(404);
    expect(res.headers.get('content-type')).toContain('application/json');
    const body = (await res.json()) as { error?: string; path?: string; method?: string };
    expect(body.error).toBe('not_found');
    expect(body.path).toBe('/api/no-such-endpoint');
    expect(body.method).toBe('GET');
  });

  test('@deep unknown /events/* path returns JSON envelope', async ({ serverInfo }) => {
    test.skip(serverInfo.backend !== 'real', 'deep-only: real server enforces gm-b2');

    const res = await fetch(`${serverInfo.baseURL}/events/no-such-stream`);
    expect(res.status).toBe(404);
    expect(res.headers.get('content-type')).toContain('application/json');
    const body = (await res.json()) as { error?: string };
    expect(body.error).toBe('not_found');
  });

  test('@deep wrong method on a real endpoint returns JSON envelope', async ({ serverInfo }) => {
    test.skip(serverInfo.backend !== 'real', 'deep-only: tests router method-not-allowed path');

    // /api/health is GET-only — POST should hit the apiNotFound /
    // MethodNotAllowed path that returns the same JSON envelope.
    const res = await fetch(`${serverInfo.baseURL}/api/health`, { method: 'POST' });
    expect(res.status).toBe(404);
    expect(res.headers.get('content-type')).toContain('application/json');
  });
});
