// specs/pending/packs.spec.ts — gm-5v8v.14
//
// ui-spec §5.17 — Pack browser (`/packs`).
//
// Status: PENDING IMPLEMENTATION. Marketplace-grid (installed
// packs highlighted) with list-view toggle. Per-pack: icon / name /
// author / version / signed-status badge / description / install.
//
// Tracking: file a feat(spa): /packs browser bead when work starts;
// reference it here.

import { test } from '../../fixtures/server';

test.describe('@pending /packs (ui-spec §5.17)', () => {
  test.skip(true, '/packs not yet implemented — see ui-spec §5.17');

  test('marketplace-grid is the default view with list-view toggle', () => {
    // Toggle persists in URL hash so bookmarks stick.
  });

  test('installed packs are highlighted in the grid', () => {
    // Visual differentiation; clicking opens detail page.
  });

  test('signed packs install one-click; unsigned require explicit checkbox + warn', () => {
    // ui-spec §8.2 — signed-vs-unsigned safety gate.
  });

  test('per-persona-within-pack toggle in the pack detail accordion', () => {
    // Granular enable/disable per persona inside an installed pack.
  });
});
