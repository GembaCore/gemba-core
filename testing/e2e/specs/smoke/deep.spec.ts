// specs/smoke/deep.spec.ts
//
// Tier: smoke. Backend: real. Owner: gm-5v8v.2.
//
// The minimal proof that the deep-mode fixture works:
//
//   1. spinRealServer boots `gemba serve` against a tempdir-isolated
//      bd workspace.
//   2. /api/health returns 200 against the real listener.
//   3. The SPA loads at the server's baseURL and AppShell renders.
//   4. BdClient can create + list a bead via the real bd CLI; the
//      created bead surfaces in the SPA's /api/work-items response.
//
// All four are tagged @deep so the smoke-deep project picks them up.
// The smoke-fake project ignores them (no @deep tag, but smoke-fake
// has no grep filter — actually smoke-fake matches all smoke specs;
// these specs use the bd fixture which throws under fake mode, so
// smoke-fake skips them via runtime error). To keep smoke-fake green,
// we explicitly skip when backend is not real.

import { test, expect } from '../../fixtures/server';

test.describe('@deep deep-mode smoke (gm-5v8v.2)', () => {
  test.skip(({ backend }) => backend !== 'real', 'deep-only spec');

  test('GET /api/health returns ok', async ({ serverInfo }) => {
    const res = await fetch(`${serverInfo.baseURL}/api/health`);
    expect(res.status).toBe(200);
    const body = (await res.json()) as { status?: string };
    expect(body.status).toBe('ok');
  });

  test('SPA loads at the server baseURL with AppShell visible', async ({ page, serverInfo }) => {
    await page.goto(`${serverInfo.baseURL}/board`);
    await expect(page.locator('main')).toBeVisible();
  });

  test('bd create surfaces in /api/work-items', async ({ bd, serverInfo }) => {
    const { id } = await bd.create({
      title: 'gm-5v8v.2 smoke bead',
      type: 'task',
      priority: 1,
    });
    expect(id).toMatch(/^[a-z0-9]+-/);

    const res = await fetch(`${serverInfo.baseURL}/api/work-items`);
    expect(res.status).toBe(200);
    const body = (await res.json()) as { items?: Array<{ id: string; title: string }> };
    const titles = (body.items ?? []).map((it) => it.title);
    expect(titles).toContain('gm-5v8v.2 smoke bead');
  });
});
