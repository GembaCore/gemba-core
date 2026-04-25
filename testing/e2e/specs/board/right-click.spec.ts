// specs/board/right-click.spec.ts (gm-5v8v.5).
//
// Right-click context menu isn't on the board today. Bead enumerates
// the menu items it expects to see when it lands; each fixme captures
// a single item so the eventual implementation can flip them into
// green tests one by one without rewriting the file.

import { test } from '../../fixtures/server';

test.fixme('right-click on a card opens the context menu @board', () => {
  /* fixme: no context-menu component on BoardPage.tsx today. */
});

test.fixme('context menu: Stage transitions the card @board', () => {
  /* fixme: same blocker as menu open. */
});

test.fixme('context menu: Start transitions the card @board', () => {
  /* fixme: same blocker as menu open. */
});

test.fixme('context menu: Defer transitions the card @board', () => {
  /* fixme: same blocker as menu open. */
});

test.fixme('context menu: Open in graph navigates to /graph?focus=ID @board', () => {
  /* fixme: /graph route already ships in gm-e12.16; once the context
     menu lands the cross-link is a one-liner. */
});

test.fixme('context menu: Apply checkpoint @board', () => {
  /* fixme: checkpoint feature itself is a separate epic (gm-root TODO). */
});

test.fixme('context menu: Open drawer @board', () => {
  /* fixme: drawer-open is wired via single-click today; the context-
     menu entry is redundant but explicit, useful when the operator
     wants to keep the keyboard out of it. */
});

test.fixme('context menu: Copy ID writes the bead id to the clipboard @board', () => {
  /* fixme: WorkItemDrawer already has a copy-id button (data-testid
     work-item-drawer-copy); the context-menu version is a parallel
     entry point. */
});
