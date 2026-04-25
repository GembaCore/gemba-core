// specs/modes/managed.spec.ts (gm-5v8v.11).
//
// ui-spec §6.2 — Managed mode confirmation UX:
//
//   "Blocking dialog before every mutation: 'You are about to
//    [Stage gm-e3 + 2 Epics]. Confirm?' Summary-then-confirm
//    pattern mandatory."
//
// All tests fixme'd; the SPA doesn't currently gate any mutation
// behind a managed-mode confirmation dialog.

import { test } from '../../fixtures/server';

test.fixme(
  'every drag mutation opens a summary-then-confirm blocking dialog @modes',
  () => {
    /* fixme: blocked on a managed-mode confirmation dialog component
       that intercepts mutations BEFORE the PATCH fires. The "summary"
       half ("You are about to [...]") needs a deterministic
       formatter shared with the supervised batch summary —
       implementations of those two surfaces should share the
       formatter so a v.next mode-switch doesn't drift the copy. */
  }
);

test.fixme(
  'dismissing the dialog cancels the mutation (no PATCH fires) @modes',
  () => {
    // fixme: "cancel button → no PATCH" is the property that
    // distinguishes managed from supervised — supervised's
    // destructive dialog also confirms but mutation is already in
    // flight. Pin via page.waitForRequest matching /api/work-items/...
    // with method=PATCH timeout=1000 and assert it times out.
  }
);

test.fixme(
  'managed mode applies the gate to every mutation kind @modes',
  () => {
    /* fixme: ui-spec L489 says "every mutation". The gate must cover
       drag (state change), inline edit (title / priority / assignee),
       new-workitem creation, JSONL import — every PATCH and POST
       through the SPA. Spec exercises one of each kind once the
       gate ships. */
  }
);

test.fixme(
  'mode pill in the chrome reads "managed" with a red surface @modes',
  () => {
    /* fixme: ui-spec §1 L90 — red pill for managed mode. */
  }
);

test.fixme(
  'the summary reads aloud the bead ids being mutated @modes',
  () => {
    /* fixme: aria-live region carrying the summary text. Important
       for accessibility; managed-mode operators are explicitly
       opting into review of every action and the assistive-tech
       experience should mirror that. */
  }
);

test.fixme(
  '@deep managed mode survives a server restart (mode persists in workspace.toml) @modes',
  () => {
    /* fixme: deep-mode property — workspace mode is configured in
       .gemba/workspace.toml at gemba serve startup. Assert that
       restarting the per-worker realServer with the same workspace
       preserves mode=managed. */
  }
);
