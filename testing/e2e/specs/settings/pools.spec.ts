// specs/settings/pools.spec.ts (gm-s47n.18) — Pool Dispatch editor.
//
// Smoke + flow coverage for /settings/pools. The vitest unit tests
// (web/src/pages/__tests__/PoolsPage.test.tsx, gm-s47n.16) already
// exercise the validation rules and adaptor branching against jsdom;
// this spec proves the surface renders and round-trips against a real
// browser + the route-fake fixture chain.
//
// All tests run on the route-fake project (manifest fixtures + canned
// /api responses); none require a live `gemba serve`.

import { test, expect } from '../../fixtures/server';
import { orchestrationManifest } from '../../fixtures/capabilitiesPlane';

const PERSONAS = [
  { id: 'engineer-claude', name: 'Engineer', agent_type: 'claude' },
  { id: 'pm-claude', name: 'PM', agent_type: 'claude' },
];

test.describe('Pool Dispatch editor @route', () => {
  test('native: renders heading, file path is read-only, no scope axis', async ({
    page,
    capabilitiesPlane,
    poolsPlane,
  }) => {
    capabilitiesPlane.setOrchestrationPlane(orchestrationManifest({ adaptor_id: 'native' }));
    poolsPlane.setConfig({
      path: '/.gemba/pool.toml',
      max_parallel: 4,
      reserved_for_manual: 1,
    });
    poolsPlane.setPersonas(PERSONAS);
    poolsPlane.setOrchState({
      adaptor_id: 'native',
      scopes: [{ id: 'local', kind: 'local', personas: [] }],
      polecats: [],
    });

    await page.goto('/settings/pools');

    await expect(page.getByTestId('pools-page')).toBeVisible();
    await expect(page.getByRole('heading', { name: /Pool Dispatch · Native/i })).toBeVisible();

    // The file path is rendered as a non-editable <code> element in the
    // header — no <input> the operator could edit. Asserting the
    // absence of any input under the header pins the read-only contract.
    const header = page.locator('[data-testid="pools-page"] > header');
    await expect(header).toContainText('/.gemba/pool.toml');
    await expect(header.locator('input')).toHaveCount(0);

    // Native adaptor → scope axis is hidden. The PoolCard only renders
    // a `pool-card-scope` select when scopeAxis === 'rig'. With no
    // cards seeded the dropdown shouldn't even exist; add one card to
    // make the assertion load-bearing.
    await page.getByTestId('pool-add-card').click();
    await expect(page.locator('[data-testid="pool-card-scope"]')).toHaveCount(0);
  });

  test('gastown: renders Gas Town heading and surfaces the rig (scope) axis', async ({
    page,
    capabilitiesPlane,
    poolsPlane,
  }) => {
    capabilitiesPlane.setOrchestrationPlane(orchestrationManifest({ adaptor_id: 'gastown' }));
    poolsPlane.setConfig({
      path: '/.gemba/pool.toml',
      max_parallel: 8,
      reserved_for_manual: 2,
    });
    poolsPlane.setPersonas(PERSONAS);
    poolsPlane.setOrchState({
      adaptor_id: 'gastown',
      scopes: [
        { id: 'hq', kind: 'rig', personas: ['engineer-claude'] },
        { id: 'lume', kind: 'rig', personas: ['pm-claude'] },
      ],
      agent_types: ['claude', 'shell-only'],
      polecats: [
        { scope: 'hq', persona: 'engineer-claude', name: 'obsidian', state: 'idle' },
      ],
    });

    await page.goto('/settings/pools');
    await expect(page.getByTestId('pools-page')).toBeVisible();
    await expect(page.getByRole('heading', { name: /Pool Dispatch · Gas Town/i })).toBeVisible();

    // Imported-from-gt block — rigs list + gt mutation buttons.
    await expect(page.getByTestId('gt-rigs-list')).toBeVisible();
    await expect(page.getByTestId('gt-rigs-list')).toContainText('hq');
    await expect(page.getByTestId('gt-rigs-list')).toContainText('lume');
    await expect(page.getByTestId('gt-new-rig')).toBeVisible();
    await expect(page.getByTestId('gt-new-polecat')).toBeVisible();

    // Adding a card on gastown surfaces the rig axis (scope dropdown).
    await page.getByTestId('pool-add-card').click();
    await expect(page.getByTestId('pool-card-scope')).toBeVisible();
    // The seeded rig scopes flow into the dropdown options.
    await expect(page.getByTestId('pool-card-scope').locator('option')).toHaveCount(2);
  });

  test('add pool → save → PUT /api/pool-config carries X-GEMBA-Confirm; reload persists state', async ({
    page,
    capabilitiesPlane,
    poolsPlane,
  }) => {
    capabilitiesPlane.setOrchestrationPlane(orchestrationManifest({ adaptor_id: 'native' }));
    poolsPlane.setConfig({
      path: '/.gemba/pool.toml',
      max_parallel: 4,
      reserved_for_manual: 1,
    });
    poolsPlane.setPersonas(PERSONAS);
    poolsPlane.setOrchState({
      adaptor_id: 'native',
      scopes: [{ id: 'local', kind: 'local', personas: [] }],
      polecats: [],
    });

    await page.goto('/settings/pools');
    await expect(page.getByTestId('pools-page')).toBeVisible();

    // Add a pool card. The default seeded persona is the first entry
    // (engineer-claude); the new card auto-fills with size=1, agent_type=claude.
    await page.getByTestId('pool-add-card').click();
    await expect(
      page.getByTestId('pool-card-local-engineer-claude')
    ).toBeVisible();

    // Tweak the size so we know the PUT body reflects the edit.
    const sizeInput = page.getByTestId('pool-card-size');
    await sizeInput.fill('2');

    // Click save; capture the PUT request that goes out.
    const [putReq] = await Promise.all([
      page.waitForRequest(
        (req) => req.method() === 'PUT' && req.url().endsWith('/api/pool-config')
      ),
      page.getByTestId('pools-save').click(),
    ]);

    // Nonce gate (replay safety).
    expect(putReq.headers()['x-gemba-confirm']).toBeTruthy();
    // Round-trip body shape: pools.local.engineer-claude exists with size=2.
    const body = putReq.postDataJSON() as {
      pools?: Record<string, Record<string, { size: number; agent_type?: string }>>;
    };
    expect(body.pools?.local?.['engineer-claude']?.size).toBe(2);

    // Restart banner appears after a successful save.
    await expect(page.getByTestId('pools-restart-banner')).toBeVisible();

    // The TOML preview reflects the new pool (the SPA's TOMLPreview
    // re-derives from the parsed cfg; we just need it visible).
    await expect(page.getByTestId('pools-section-pools')).toContainText('engineer-claude');

    // Reload — the dispatcher's poolsPlane.applyPut persisted the body,
    // so a fresh GET /api/pool-config replays the saved state.
    await page.reload();
    await expect(page.getByTestId('pools-page')).toBeVisible();
    await expect(
      page.getByTestId('pool-card-local-engineer-claude')
    ).toBeVisible();
    await expect(page.getByTestId('pool-card-size')).toHaveValue('2');
  });

  test('validation surfaces — floor>1, negative size, duplicate (scope, persona) all block save', async ({
    page,
    capabilitiesPlane,
    poolsPlane,
  }) => {
    capabilitiesPlane.setOrchestrationPlane(orchestrationManifest({ adaptor_id: 'native' }));
    poolsPlane.setConfig({
      path: '/.gemba/pool.toml',
      max_parallel: 4,
      reserved_for_manual: 1,
    });
    poolsPlane.setPersonas(PERSONAS);
    poolsPlane.setOrchState({
      adaptor_id: 'native',
      scopes: [{ id: 'local', kind: 'local', personas: [] }],
      polecats: [],
    });

    await page.goto('/settings/pools');
    await expect(page.getByTestId('pools-page')).toBeVisible();

    await page.getByTestId('pool-add-card').click();

    // Floor > 1 is invalid.
    await page.getByTestId('pool-card-floor').fill('1.5');
    await expect(page.getByTestId('pool-card-issue-error')).toContainText('floor');
    await expect(page.getByTestId('pools-save')).toBeDisabled();

    // Reset floor and try a negative size.
    await page.getByTestId('pool-card-floor').fill('0');
    await page.getByTestId('pool-card-size').fill('-1');
    await expect(page.getByTestId('pool-card-issue-error')).toContainText('size');
    await expect(page.getByTestId('pools-save')).toBeDisabled();

    // Reset size, then add a duplicate card with the same (scope, persona).
    await page.getByTestId('pool-card-size').fill('1');
    await expect(page.getByTestId('pools-save')).toBeEnabled();
    await page.getByTestId('pool-add-card').click();
    // Two cards, same default (local, engineer-claude) → duplicate error.
    await expect(
      page.getByTestId('pool-card-issue-error').filter({ hasText: 'duplicate' })
    ).toBeVisible();
    await expect(page.getByTestId('pools-save')).toBeDisabled();
  });

  test('clamp warning surfaces when size exceeds MaxParallel - reserved_for_manual; save still allowed', async ({
    page,
    capabilitiesPlane,
    poolsPlane,
  }) => {
    // MaxParallel=2, reserved_for_manual=1 → effective cap = 1. Size=3 trips clamp.
    capabilitiesPlane.setOrchestrationPlane(orchestrationManifest({ adaptor_id: 'native' }));
    poolsPlane.setConfig({
      path: '/.gemba/pool.toml',
      max_parallel: 2,
      reserved_for_manual: 1,
    });
    poolsPlane.setPersonas(PERSONAS);
    poolsPlane.setOrchState({
      adaptor_id: 'native',
      scopes: [{ id: 'local', kind: 'local', personas: [] }],
      polecats: [],
    });

    await page.goto('/settings/pools');
    await expect(page.getByTestId('pools-page')).toBeVisible();

    await page.getByTestId('pool-add-card').click();
    await page.getByTestId('pool-card-size').fill('3');

    await expect(page.getByTestId('pool-card-clamp-warning')).toBeVisible();
    // Save remains enabled — clamp is a warning, not an error.
    await expect(page.getByTestId('pools-save')).toBeEnabled();
  });

  test('phase 0: empty pool config renders the zero-delta editor; saving an empty config persists no pools', async ({
    page,
    capabilitiesPlane,
    poolsPlane,
  }) => {
    capabilitiesPlane.setOrchestrationPlane(orchestrationManifest({ adaptor_id: 'native' }));
    // Path-not-configured shape — server seeded with no [pool] file.
    poolsPlane.setConfig({
      path: '',
      max_parallel: 0,
      reserved_for_manual: 0,
    });
    poolsPlane.setPersonas(PERSONAS);
    // No orchestration state — editor tolerates the 404.
    poolsPlane.setOrchState(null);

    await page.goto('/settings/pools');
    await expect(page.getByTestId('pools-page')).toBeVisible();

    // No pool cards; the only Pools children are the "+ Add pool"
    // affordance.
    await expect(page.getByTestId('pool-add-card')).toBeVisible();
    await expect(page.locator('[data-testid^="pool-card-"]')).toHaveCount(0);

    // Save the empty config — the dispatcher echoes back, the SPA
    // surfaces the restart banner, and the persisted state stays empty.
    const [putReq] = await Promise.all([
      page.waitForRequest(
        (req) => req.method() === 'PUT' && req.url().endsWith('/api/pool-config')
      ),
      page.getByTestId('pools-save').click(),
    ]);
    const body = putReq.postDataJSON() as { pools?: Record<string, unknown> };
    expect(body.pools ?? {}).toEqual({});
    await expect(page.getByTestId('pools-restart-banner')).toBeVisible();

    // Reload — still no cards (Phase 0 zero-delta).
    await page.reload();
    await expect(page.getByTestId('pools-page')).toBeVisible();
    await expect(page.locator('[data-testid^="pool-card-"]')).toHaveCount(0);
  });
});
