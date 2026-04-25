// specs/board/multi-select.spec.ts (gm-5v8v.5).
//
// Multi-select isn't wired in web/src/pages/BoardPage.tsx today. Every
// test here is fixme'd; this file pins the contract so when the
// feature lands, its DoD is already an executable spec.

import { test } from '../../fixtures/server';

test.fixme('Space-toggle adds and removes a card from the selection set @board', () => {
  /* fixme: selection state not in BoardPage.tsx. */
});

test.fixme('Shift-click selects a contiguous range within a column @board', () => {
  /* fixme: selection state not in BoardPage.tsx. */
});

test.fixme('selecting one or more cards reveals the bulk-action bar @board', () => {
  /* fixme: bulk-action bar component does not exist yet. The bar's
     job is to expose Stage / Start / Defer / Cancel as bulk PATCHes
     across the selection set. */
});

test.fixme('clicking outside the board clears the selection set @board', () => {
  /* fixme: same blocker as selection state. */
});
