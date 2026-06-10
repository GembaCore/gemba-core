// shared/pool-config.ts (gm-root.27.7) — Pool config setup driver.
//
// Drives the SPA's `/settings/pools` editor end-to-end. Encapsulates
// the native flow + the gastown flow (which has the extra rig +
// polecat creation steps via RunCommandModal).
//
// SELECTOR NOTE
//   The bead description (gm-root.27.7) names selectors like
//   `pool-persona-select` / `pool-save-button` / `pool-size-input` —
//   those do NOT exist in the SPA. The actual editor (per
//   web/src/pages/PoolsPage.tsx + web/src/components/pools/PoolCard.tsx)
//   uses `pool-add-card`, `pool-card-persona`, `pool-card-size`,
//   `pool-card-floor`, `pools-save`, `pools-restart-banner`,
//   `gt-new-rig`, `gt-new-polecat`, `run-command-modal`,
//   `run-command-field-<field>`, `run-command-confirm`.
//
//   This file uses the real selectors. The bead description should be
//   updated; flagged in the implementation report.

import type { Page } from '@playwright/test';

export interface ConfigurePoolOpts {
  variant: 'native' | 'gastown';
  /** Pool scope: 'local' for native, the rig name for gastown. */
  scope: string;
  /** Persona id (e.g., 'acceptance-engineer'). */
  persona: string;
  /** Concurrency target for the pool. */
  size: number;
  /** Idle floor (0..1). */
  floor: number;
}

/**
 * configurePool drives the /settings/pools editor.
 *
 * The page must already be on /settings/pools when this is called
 * (the caller in `runAcceptance` does `goto('/settings/pools')`).
 *
 * The helper does NOT trigger the post-save server restart — it
 * returns once the restart banner appears. The caller is responsible
 * for killing + relaunching the gemba serve process so the new
 * pool.toml is picked up.
 */
export async function configurePool(page: Page, opts: ConfigurePoolOpts): Promise<void> {
  // Ensure we're on the editor — getByTestId('pools-page') will throw
  // a clear assertion error otherwise.
  await page.waitForSelector('[data-testid="pools-page"]', { timeout: 15_000 });

  if (opts.variant === 'gastown') {
    await provisionGastownPrereqs(page, opts.scope, opts.persona);
  }

  // Add a card. The default seeded persona is the first entry in the
  // persona list; we'll overwrite it via the persona dropdown if the
  // requested persona doesn't match.
  await page.getByTestId('pool-add-card').click();

  // Select the scope (gastown only — native hides the scope axis).
  if (opts.variant === 'gastown') {
    const scopeSelect = page.getByTestId('pool-card-scope');
    await scopeSelect.selectOption({ value: opts.scope }).catch(async () => {
      // Fall back to selecting by visible label if value-match misses.
      await scopeSelect.selectOption({ label: opts.scope });
    });
  }

  // Select the persona. The dropdown options are seeded from the
  // capabilities plane; if `acceptance-engineer` isn't installed yet,
  // surface a clear error.
  const personaSelect = page.getByTestId('pool-card-persona');
  try {
    await personaSelect.selectOption({ value: opts.persona });
  } catch {
    try {
      await personaSelect.selectOption({ label: opts.persona });
    } catch (err) {
      throw new Error(
        `persona "${opts.persona}" not available in the pool editor — ` +
          `is the acceptance-engineer persona installed in this project?\n` +
          `original: ${(err as Error).message}`,
      );
    }
  }

  // Size + floor.
  await page.getByTestId('pool-card-size').fill(String(opts.size));
  await page.getByTestId('pool-card-floor').fill(String(opts.floor));

  // Save. The save button is gated on validation passing; if it's
  // disabled, surface why via the page's error display.
  const save = page.getByTestId('pools-save');
  if (!(await save.isEnabled())) {
    const err = await page
      .getByTestId('pool-card-issue-error')
      .first()
      .textContent()
      .catch(() => null);
    throw new Error(`pools-save is disabled — validation issue: ${err ?? 'unknown'}`);
  }
  await save.click();

  // Restart banner is the post-save signal per gm-s47n.16.
  await page.waitForSelector('[data-testid="pools-restart-banner"]', { timeout: 15_000 });
}

// ─── Gastown prerequisites: rig + polecat ─────────────────────────

/**
 * Drive the RunCommandModal flow to create the rig and the polecat
 * the requested pool will scope to. No-op on native.
 */
async function provisionGastownPrereqs(page: Page, rigName: string, persona: string): Promise<void> {
  // 1. New rig.
  await page.getByTestId('gt-new-rig').click();
  await page.waitForSelector('[data-testid="run-command-modal"]', { timeout: 10_000 });
  // RunCommandModal renders one input per declared field — for `gt
  // rig create` the field name is the rig identifier.
  const rigField = page.getByTestId('run-command-field-name');
  await rigField.fill(rigName);
  await page.getByTestId('run-command-confirm').click();
  // Wait for completion; the modal switches to a result view with a
  // close button. We close it before continuing.
  await page.waitForSelector('[data-testid="run-command-result"]', { timeout: 60_000 });
  await page.getByTestId('run-command-close').click();

  // 2. New polecat (per persona).
  await page.getByTestId('gt-new-polecat').click();
  await page.waitForSelector('[data-testid="run-command-modal"]', { timeout: 10_000 });
  // Polecat form has at least a `name` and may have a `rig` selector.
  // We populate any field testid we can find that maps to the persona
  // / rig shape; harmless extras stay defaulted.
  await page.getByTestId('run-command-field-name').fill(`${persona}-${rigName}`);
  // Some adaptor command schemas surface a `rig` field; if absent, the
  // selectOption call below silently no-ops.
  const rigOnPolecat = page.locator('[data-testid="run-command-field-rig"]');
  if ((await rigOnPolecat.count()) > 0) {
    await rigOnPolecat.selectOption({ value: rigName }).catch(async () => {
      await rigOnPolecat.selectOption({ label: rigName });
    });
  }
  await page.getByTestId('run-command-confirm').click();
  await page.waitForSelector('[data-testid="run-command-result"]', { timeout: 60_000 });
  await page.getByTestId('run-command-close').click();
}
