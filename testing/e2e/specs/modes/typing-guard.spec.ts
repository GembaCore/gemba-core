// specs/modes/typing-guard.spec.ts (gm-5v8v.11).
//
// ui-spec §6.2 L491 — cross-mode invariant:
//
//   "Destructive actions ALWAYS require typing-guard, regardless of
//    mode: restore checkpoint, force-close (convoy), hard-override
//    (gate), delete (pack), cancel Epic (if In Progress or later)."
//
// The SPA does not yet ship a typing-guard component. Every test
// in this file is fixme'd against that surface; this file is the
// executable contract for the cross-mode invariant.
//
// "Typing-guard" pattern: a destructive dialog requires the operator
// to TYPE the bead id (or a fixed phrase like "DELETE") into a text
// input before the confirm button enables. Catches accidental clicks
// past muscle-memory.

import { readFileSync } from 'node:fs';
import { test, expect } from '../../fixtures/server';

const DESTRUCTIVE_ACTIONS = [
  'restore checkpoint',
  'force-close convoy',
  'hard-override gate',
  'delete pack',
  'cancel In Progress Epic',
] as const;

for (const action of DESTRUCTIVE_ACTIONS) {
  test.fixme(`${action}: typing-guard dialog opens before any mutation @modes`, () => {
    /* fixme: typing-guard component does not exist. Once it does,
       this exercises: trigger the action → role=dialog opens → input
       field expects the bead id (or fixed phrase) → confirm button
       is disabled until the typed value matches → PATCH/DELETE only
       fires after a successful match. */
  });

  test.fixme(`${action}: typed mismatch keeps the confirm button disabled @modes`, () => {
    /* fixme: same blocker. Pinning the negative path so a future
       implementation can't ship a typing-guard that accepts
       any input. */
  });
}

test.fixme(
  'typing-guard fires in unsupervised mode (mode does not bypass guard) @modes',
  () => {
    /* fixme: ui-spec L491 invariant — destructive actions require
       typing-guard in EVERY mode. Cross-references unsupervised.spec.ts
       which says toast-only for normal mutations; this test pins
       that destructive actions are the explicit carve-out. */
  }
);

test.fixme(
  'typing-guard fires in managed mode (guard layered on top of mode dialog) @modes',
  () => {
    /* fixme: managed mode already shows a confirm dialog before
       every mutation (managed.spec.ts). Destructive actions in
       managed get the typing-guard ON TOP of the mode dialog,
       not instead of it — the guard is per-action, the mode dialog
       is per-mutation. */
  }
);

// gm-5v8v.11.1 — fixture-level deep test. typing-guard is a cross-mode
// invariant ("destructive actions require typing-guard regardless of
// mode"). The server-side teeth (a destructive-confirm header on
// destructive endpoints) are not built. The fixture-level promise we
// CAN pin: every workspace mode the SPA can run under boots cleanly
// against the same `gemba serve` infra and persists its mode. When
// the destructive-confirm gate ships, the new assertions land
// alongside this anchor.
test.describe('@deep server enforces destructive-action gate on every mode @modes', () => {
  test.skip(({ backend }) => backend !== 'real', 'deep-only — needs a real listener');

  for (const mode of ['unsupervised', 'supervised', 'managed'] as const) {
    test(`workspace.toml persists mode=${mode} and gemba boots cleanly`, async ({ authServer }) => {
      const srv = await authServer({ mode });
      expect(srv.mode).toBe(mode);
      expect(srv.workspaceTomlPath).toBeTruthy();
      const body = readFileSync(srv.workspaceTomlPath as string, 'utf8');
      expect(body).toMatch(new RegExp(`^mode = "${mode}"$`, 'm'));

      const res = await fetch(`${srv.baseURL}/api/health`);
      expect(res.status).toBe(200);
    });
  }
});
