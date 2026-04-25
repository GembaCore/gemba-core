// specs/modes/supervised.spec.ts (gm-5v8v.11).
//
// ui-spec §6.2 — Supervised mode (default) confirmation UX:
//
//   "Inline confirm for single actions; dialog for destructive ones;
//    batch summary at end of batch actions."
//
// SPA hasn't shipped any of inline-confirm, mode-gated destructive
// dialogs, or batch summaries yet. Every test fixme'd; this file
// documents the supervised-mode contract.

import { test } from '../../fixtures/server';

test.fixme(
  'single-card drag shows an inline confirm chip on the row @modes',
  () => {
    /* fixme: blocked on a "confirm" inline component (a small chip
       that overlays the dropped card, asking the operator to commit
       or undo within ~2s before the PATCH fires). The undo path
       hasn't been designed; ui-spec §6.2 just says "inline confirm
       for single". Define the testid + interaction model with the
       UI owner before flipping. */
  }
);

test.fixme(
  'destructive single action (cancel epic) opens a blocking dialog @modes',
  () => {
    /* fixme: a destructive-single dialog component doesn't exist.
       Once it does, this asserts: cancel an epic → role=dialog
       opens with the bead id + reason field; confirm sends a PATCH
       with state_category=canceled; cancel button leaves the bead
       unchanged. */
  }
);

test.fixme(
  'batch action shows a summary dialog at end of batch @modes',
  () => {
    /* fixme: requires (a) multi-select state (gm-5v8v.5 fixme),
       (b) bulk-action bar, (c) per-action result aggregation, and
       (d) a summary dialog component. Per ui-spec §6.2 the summary
       reads "Staged 12, failed 1; see details". */
  }
);

test.fixme(
  'mode pill in the chrome reads "supervised" by default @modes',
  () => {
    /* fixme: ui-spec §1 L90 ("Mode indicator — color-coded pill: green
       = unsupervised, blue = supervised, red = managed"). Topbar
       doesn't render the pill yet; once it does, this is a one-line
       getByText('Supervised') assertion. */
  }
);

test.fixme(
  '@deep server respects supervised mode in nonce + audit chain @modes',
  () => {
    /* fixme: deep-mode parity check — same as unsupervised, the
       server today is mode-agnostic. Defer until the backend owner
       confirms whether mode is part of the audit envelope. */
  }
);
