// specs/pending/workspace-switcher.spec.ts — gm-5v8v.14
//
// ui-spec §2.3 (item 2) — Workspace switcher in the topbar.
//
// Status: PARTIALLY IMPLEMENTED. The Topbar.tsx already renders a
// `data-hotkey-target="workspace-switcher"` button labeled "default",
// but it has no dropdown, no list, and no actual switching behavior.
// The shell hotkey 'W' is wired in defaults.ts as 'Switch workspace'
// but currently has no handler.
//
// Tracking: file a feat(spa): workspace switcher dropdown + URL
// preservation bead when work starts; reference it here.

import { test } from '../../fixtures/server';

test.describe('@pending Workspace switcher (ui-spec §2.3.2)', () => {
  test.skip(true, 'Workspace switcher dropdown not yet implemented — see ui-spec §2.3.2');

  test('clicking the switcher opens a dropdown listing every workspace', () => {
    // Currently the button is non-functional. Add a Radix popover
    // listing workspaces from /api/workspaces (TBD endpoint).
  });

  test('selecting a workspace preserves the URL path where possible', () => {
    // Per ui-spec §2.3.2 — switching from /board on workspace A to
    // workspace B keeps you on /board.
  });

  test("'W' hotkey opens the switcher", () => {
    // Already declared in hotkeys/defaults.ts; needs a handler.
  });

  test('mode + phase + budget pills update for the selected workspace', () => {
    // Topbar pills are workspace-scoped; switching repaints them
    // from the new workspace's adaptor data.
  });
});
