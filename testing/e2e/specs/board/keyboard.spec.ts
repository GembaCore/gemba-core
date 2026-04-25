// specs/board/keyboard.spec.ts (gm-5v8v.5).
//
// Every test in this spec is fixme'd because the underlying SPA
// hotkeys aren't wired yet — see web/src/pages/BoardPage.tsx, which
// today only registers `view-toggle-board` (Cmd-Shift-W). The bead's
// keyboard spec list anchors here as the executable contract for the
// future implementation; once a hotkey lands, flip its fixme into a
// real test.

import { test } from '../../fixtures/server';

test.fixme('J/K navigate cards within a column @board', () => {
  /* fixme: card-focus state + J/K handlers not implemented in
     web/src/pages/BoardPage.tsx. Hotkey ids would slot into the
     existing useHotkey() registry alongside view-toggle-board. */
});

test.fixme('H/L navigate columns left/right @board', () => {
  /* fixme: column focus is not a thing yet; same blocker as J/K. */
});

test.fixme('Space toggles multi-select on the focused card @board', () => {
  /* fixme: multi-select state isn't in BoardPage.tsx; a selection set
     would live in a useState alongside the existing openWorkItemId. */
});

test.fixme('Cmd/Ctrl+A selects every card in the focused column @board', () => {
  /* fixme: blocks on multi-select state above. */
});

test.fixme('Enter on a focused card opens the WorkItemDrawer @board', () => {
  /* fixme: drawer-open via keyboard would extend the existing
     onSelect() path that today only fires from card click. */
});

test.fixme('Stage / Start chord transitions the focused card @board', () => {
  /* fixme: bead's keyboard.spec lists "Enter Stage/Start" — needs a
     defined chord (Shift+Enter? S/T?) and a PATCH wiring. Defer until
     product picks the keys. */
});
