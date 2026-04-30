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

// gm-root.25 — drag-to-spawn now fires for `epic`, `task`, `bug`, and
// `feature` kinds when dropped into "In Progress". The predicate lives
// in web/src/components/board/dragToRestage.ts (`shouldAutoStartSession`
// + `AUTOSTART_KINDS`); unit-level coverage in
// web/src/components/board/__tests__/EpicView.drag.test.tsx exhausts
// the matrix.
//
// The fake-mode smoke below drives the dnd-kit pointer sequence on an
// epic card to the started cell and asserts the SPA fires
// POST /api/sessions with the bead's id. This pins the existing
// epic-only drag-to-spawn path against regression now that the
// predicate has grown.
//
// Coverage for `task` / `bug` / `feature` via a real drag is blocked on
// the flat WorkItemBoard wiring dnd-kit — see the fixme at the top of
// this file. Until it lands, the predicate change is exercised at the
// unit layer (vitest matrix above) and will inherit this spec's
// scaffolding once the flat board grows draggable cards.
test('drag-to-spawn: epic dropped on In Progress fires POST /api/sessions @board', async ({
  page,
  workPlane,
}) => {
  const seeded = epic({
    id: 'demo/pc-spawn',
    title: 'Drag-to-spawn smoke',
    state_category: 'unstarted',
  });
  workPlane.seed([seeded]);

  // The fixture's PATCH /api/work-items/:id echoes the EXISTING item
  // (the comment in fixtures/server.ts at the PATCH branch flags
  // "richer fake — apply patch to store" as a follow-up). The
  // shouldAutoStartSession predicate keys off the response's
  // state_category, so a plain echo would leave it 'unstarted' and the
  // session POST never fires. Override PATCH for this spec to apply
  // the state_category from the request body before responding.
  await page.route(`**/api/work-items/${encodeURIComponent(seeded.id)}`, async (route) => {
    if (route.request().method() === 'PATCH') {
      let patch: Record<string, unknown> = {};
      try {
        patch = JSON.parse(route.request().postData() ?? '{}') as Record<string, unknown>;
      } catch {
        /* leave patch empty */
      }
      const merged = { ...seeded, ...patch };
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(merged),
      });
      return;
    }
    await route.fallback();
  });

  // Capture POST /api/sessions calls. The fake dispatcher (fixtures/
  // server.ts) responds with a synthetic session record; we just need
  // to observe the request body to assert the SPA wired the bead id.
  const sessionPosts: Array<Record<string, unknown>> = [];
  await page.route('**/api/sessions', async (route) => {
    if (route.request().method() === 'POST') {
      try {
        sessionPosts.push(JSON.parse(route.request().postData() ?? '{}'));
      } catch {
        sessionPosts.push({});
      }
    }
    // Hand off to the fake dispatcher so the SPA's mutation resolves
    // normally — page.route handlers stack, fallback() walks to the
    // default fixture handler.
    await route.fallback();
  });

  const board = new BoardPage(page);
  await board.gotoEpicView();

  // Source: the draggable epic card. Target: the started cell of this
  // epic's swimlane (epic with no parent → its own swimlane).
  const source = page.locator(`[data-epic-card="true"]`).first();
  const target = page.getByTestId(`board-epic-cell-${seeded.id}-started`);
  await expect(source).toBeVisible();
  await expect(target).toBeVisible();

  const { dragTo } = await import('../../helpers/dragdrop');
  await dragTo(page, source, target);

  // Wait for the SPA's onDragEnd → useUpdateWorkItem → useStartSession
  // chain to resolve. The mutation queue is fast under the fake
  // backend; a short polled expect keeps this resilient.
  await expect.poll(() => sessionPosts.length, { timeout: 5_000 }).toBeGreaterThan(0);
  expect(sessionPosts[0]).toMatchObject({ bead_id: seeded.id });
});
