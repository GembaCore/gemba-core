// specs/pending/qa-gates.spec.ts — gm-5v8v.14
//
// ui-spec §5.13 — QA Gates (`/qa/gates`).
//
// Status: PENDING IMPLEMENTATION. Sorted by status (failed → stale →
// passed); per-gate row shows scope, target, condition, status,
// override dropdown.
//
// Tracking: file a feat(spa): /qa/gates surface bead when work
// starts; reference it here.

import { test } from '../../fixtures/server';

test.describe('@pending /qa/gates (ui-spec §5.13)', () => {
  test.skip(true, '/qa/gates not yet implemented — see ui-spec §5.13');

  test('sorted by status: failed → stale → passed', () => {
    // List ordering invariant.
  });

  test('per-gate row: ID / scope / target / condition / last-check / status', () => {
    // Status badge color from §1.2 palette.
  });

  test('Override dropdown reveals "Request persona consensus" + "Request user override"', () => {
    // Persona-consensus opens persona-chooser modal; user-override
    // opens typing-guard with justification textarea.
  });

  test('typing-guard is destructive-action-required for hard override', () => {
    // ui-spec §6.2 — destructive ALWAYS requires typing-guard.
  });
});
