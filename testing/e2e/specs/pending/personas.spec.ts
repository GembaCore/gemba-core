// specs/pending/personas.spec.ts — gm-5v8v.14
//
// ui-spec §5.5 — Persona roster (`/personas`).
//
// Status: PENDING IMPLEMENTATION. Grid of cards (3-4/row) with
// expandable drawers for each Coach/Manager. Edit config via TOML
// editor modal saving to `.gemba/personas/<id>.toml`.
//
// Tracking: file a feat(spa): /personas roster bead when work
// starts; reference it here.

import { test } from '../../fixtures/server';

test.describe('@pending /personas (ui-spec §5.5)', () => {
  test.skip(true, '/personas not yet implemented — see ui-spec §5.5');

  test('grid of cards lists every persona', () => {
    // Each card: icon / role / variety badge (Coach=purple,
    // Manager=navy) / enabled toggle / current model.
  });

  test('expanding a card shows personality / perspective / purview / skills / budget', () => {
    // Per ui-spec §5.5 — the closed→drawer transition.
  });

  test('TOML editor modal persists changes to .gemba/personas/<id>.toml', () => {
    // Monospace, syntax-highlighted; ratify with nonce.
  });

  test('Coach badge uses purple, Manager badge uses navy', () => {
    // Per ui-spec §1.2 palette — semantic color discipline.
  });
});
