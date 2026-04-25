// specs/board/drag.spec.ts (gm-5v8v.5).
//
// Tier: route + integration. The bead tags this whole spec @deep
// because the meaningful assertions ride a real PATCH round-trip
// against a real adaptor (state_category transitions, UserOrder
// reorder, nonce idempotency). Today only the dnd-kit-side mechanics
// can run against fake; the rest waits for gm-5v8v.2's deep backend
// + per-spec PATCH log on the fake (a follow-up enhancement).
//
// What runs in fake (today):
//   - drag helper module imports without throwing — pins the helper
//     surface so a regression there is caught at parse time
//
// What waits for deep:
//   - cross-column drag = state_category change via PATCH
//   - within-column drag = UserOrder reorder via PATCH
//   - undo path
//   - toast on success
//   - the dispatch chain (PATCH → POST /sessions → tmux → evidence) —
//     covered end-to-end in specs/integration/dispatch-chain.spec.ts
//     (gm-5v8v.15) under the integration-deep project

import { expect, test } from '../../fixtures/server';
import { BoardPage } from '../../pages/BoardPage';
import { epic } from '../../builders/workitem';

// The flat WorkItemBoard view doesn't currently wire dnd-kit (drag
// lives on EpicView). The bead's drag specs target the Epic view's
// cards, not the flat columns. Until the flat view gains drag, this
// fake-tier test is fixme'd.
test.fixme(
  'fake: dragging a flat-view card across columns emits a PATCH with the target state_category',
  () => {
    /* fixme: WorkItemBoard (flat view) doesn't ship dnd-kit yet.
       Drag lives on EpicView only — see web/src/components/board/
       EpicView.tsx. When the flat view gains drag, fold this into
       the green path; the helper + builder already in this directory
       are sufficient. */
  }
);

test.fixme(
  'cross-column drag changes state_category via PATCH and is nonce-gated @deep',
  () => {
    /* fixme: deferred to deep mode (gm-5v8v.2). Fake mode can't
       observe the real bd→Dolt persistence; without that the spec
       just re-asserts the SPA cache. */
  }
);

test.fixme(
  'within-column drag reorders by UserOrder via PATCH @deep',
  () => {
    /* fixme: UserOrder isn't on the wire today (gm-root TODO). When
       it lands, this spec asserts a within-column drag emits a PATCH
       carrying the new ordinal. */
  }
);

test.fixme(
  'success toast renders after a successful state-change drag @deep',
  () => {
    /* fixme: toast surface not yet in BoardPage.tsx. Filed under
       gm-5v8v follow-up; the fixture is straightforward once the
       Toaster component lands. */
  }
);

test.fixme(
  'undo path reverses the PATCH within the toast lifetime @deep',
  () => {
    /* fixme: blocked on toast surface above + an undo-stack that
       owns the inverse patch. */
  }
);

test.fixme(
  'integration: dispatch-chain (drag → PATCH → session POST → tmux → evidence) @deep',
  () => {
    /* fixme: full chain lives in specs/integration/dispatch-chain.spec.ts
       (gm-5v8v.15). This stub stays so the board spec list still
       advertises that drag-to-In-Progress goes deeper than the
       PATCH the unit specs above assert. */
  }
);

// One green sanity test in fake mode: load the helper module and
// confirm the dragTo function is callable. A regression in
// helpers/dragdrop.ts (e.g. an import-time syntax error) will fail
// this test before any of the @deep specs are even reached.
test('drag helper module imports without throwing @board', async ({ page, workPlane }) => {
  workPlane.seed([epic()]);
  const board = new BoardPage(page);
  await board.gotoEpicView();
  const { dragTo } = await import('../../helpers/dragdrop');
  expect(typeof dragTo).toBe('function');
  // Either Epic view or WorkItem flat is acceptable here — the helper
  // doesn't know which it'll be asked to drag in. We just need the
  // page settled.
  await expect(board.workItemBoard.or(page.getByTestId('board-epic'))).toBeVisible();
});
