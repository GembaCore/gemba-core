// specs/sessions/escalation-bulk.spec.ts (gm-5v8v.9).
//
// Bulk-action surface from ui-spec §5.8 (ack / dismiss / move-to-Gemba-walk).
// The SPA hasn't shipped this surface yet — EscalationPanel today
// surfaces single-row Approve / Deny / Custom. Each fixme captures a
// future bulk-action contract so the spec list is the executable
// roadmap when the feature lands.

import { test } from '../../fixtures/server';

test.fixme('bulk: ack moves a selection set out of the open list @sessions', () => {
  /* fixme: ui-spec §5.8 lists ack as a bulk action; today the panel
     has only per-row Approve / Deny. Selection state + bulk bar are
     a future surface. */
});

test.fixme('bulk: dismiss closes selected escalations without a resolution @sessions', () => {
  /* fixme: same blocker as ack — bulk selection isn't wired. */
});

test.fixme('bulk: move-to-Gemba-walk transfers escalations to the walk view @sessions', () => {
  /* fixme: Gemba walk view itself is a separate epic (gm-root TODO);
     bulk move is the cross-link surface that lands once both ends
     ship. */
});
