// specs/pending/checkpoints.spec.ts — gm-5v8v.14
//
// ui-spec §5.14 — Checkpoints timeline (`/checkpoints`).
//
// Status: PENDING IMPLEMENTATION. Horizontal timeline (primary) +
// calendar grid (secondary, collapsible). Restore = typing-guard
// destructive confirmation.
//
// Tracking: file a feat(spa): /checkpoints surface bead when work
// starts; reference it here.

import { test } from '../../fixtures/server';

test.describe('@pending /checkpoints (ui-spec §5.14)', () => {
  test.skip(true, '/checkpoints not yet implemented — see ui-spec §5.14');

  test('horizontal timeline is primary visualization', () => {
    // Calendar grid is secondary, collapsible.
  });

  test('per-checkpoint: label / trigger glyph / age / Restore button', () => {
    // Trigger kinds get distinct glyphs.
  });

  test('Restore opens typing-guard with the exact ui-spec §5.14 copy', () => {
    // "Type `restore` to confirm. This will roll back every git repo,
    // the Beads database, live session state, sidecar, and artifacts
    // to this checkpoint. Pushed commits cannot be un-pushed."
  });

  test('Cancel before typing closes the modal without firing restore', () => {
    // Dismiss → no PATCH/POST.
  });
});
