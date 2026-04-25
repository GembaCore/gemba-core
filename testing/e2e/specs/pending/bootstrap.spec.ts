// specs/pending/bootstrap.spec.ts — gm-5v8v.14
//
// ui-spec §5.15 — Bootstrap wizard (`/bootstrap`).
//
// Status: PENDING IMPLEMENTATION. 4-step wizard: Source → Analysis →
// Plan review → Ratify. Modal-style step boundaries with nonce-
// confirmed final commit.
//
// Tracking: file a feat(spa): /bootstrap wizard bead when work
// starts; reference it here.

import { test } from '../../fixtures/server';

test.describe('@pending /bootstrap (ui-spec §5.15)', () => {
  test.skip(true, '/bootstrap not yet implemented — see ui-spec §5.15');

  test('Step 1 Source tiles: Jira / Beads / Source-code / Fresh', () => {
    // Each tile has icon + 1-line description; click commits source
    // choice to wizard state.
  });

  test('Step 2 Analysis shows live progress strings', () => {
    // "Scanning 247 issues…" / "Analyzing module structure…" /
    // "Generating epic decomposition…" — drives off long-poll or
    // SSE on a /api/bootstrap/progress endpoint (TBD).
  });

  test('Step 3 Plan review: side-by-side plan + consistency report', () => {
    // Generated plan left, summary-then-details report right.
    // Top-line PASS/WARN/FAIL with drill-down per finding.
  });

  test('Step 4 Ratify nonce-confirms commit of project config', () => {
    // POST writes .gemba/project.toml + initial bd database; nonce
    // gate blocks accidental double-clicks.
  });

  test('Cancel from any step does not persist anything', () => {
    // Idempotent abort — no .gemba/ writes until step 4.
  });
});
