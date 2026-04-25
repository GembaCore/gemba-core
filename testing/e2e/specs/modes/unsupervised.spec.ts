// specs/modes/unsupervised.spec.ts (gm-5v8v.11).
//
// ui-spec §6.2 — Unsupervised mode confirmation UX:
//
//   "Toast-only ('Staged Epic gm-e3') after success; no pre-action
//    dialog."
//
// The SPA does not yet ship a Toaster component, a workspace-mode
// reader, or any mode-gated confirmation logic. Every test in this
// file is fixme'd against those surfaces; the file exists as the
// executable contract anchor so when the feature lands, the DoD is
// already enforceable.

import { test } from '../../fixtures/server';

test.fixme(
  'drag epic to In Progress shows a Toast and does NOT open a confirm dialog @modes',
  () => {
    /* fixme: blocked on (a) SPA Toaster component, (b) workspace-mode
       reader (e.g. useWorkspaceMode hook fed by /api/workspace), and
       (c) BoardPage gating the post-mutation Toast on mode === 'unsupervised'.
       Spec body once unblocked: set fixture mode to 'unsupervised',
       seed an epic, drag → expect getByRole('status') with the
       canonical "Staged Epic gm-eN" copy, and expect no role=dialog. */
  }
);

test.fixme(
  'multi-card bulk action emits a Toast per success without a batch summary dialog @modes',
  () => {
    /* fixme: same blocker as above + multi-select wiring (gm-5v8v.5
       fixme'd). Toast content per ui-spec L487 reads as one toast
       per resolved mutation, not a dialog summary. */
  }
);

test.fixme(
  'destructive actions (cancel In-Progress Epic) STILL require typing-guard in unsupervised @modes',
  () => {
    /* fixme: ui-spec §6.2 L491 — "Destructive actions ALWAYS require
       typing-guard, regardless of mode." Cross-mode invariant
       covered separately in typing-guard.spec.ts; pinning here so
       the unsupervised file documents the carve-out where toast-
       only does NOT apply. */
  }
);

test.fixme(
  '@deep server respects unsupervised mode in audit-log entries @modes',
  () => {
    /* fixme: deep-mode variant. The ui-spec implies the mode is the
       UI's responsibility (server is mode-agnostic, just nonce-gates
       writes), but if the server records the operator's mode for
       audit purposes the assertion lives here. Confirm with backend
       owner before flipping into a real test. */
  }
);
