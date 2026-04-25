// specs/sessions/peek.spec.ts (gm-5v8v.9).
//
// Tier: route. The peek surface (GET /sessions/:id/peek for transcript
// tail) is gm-e7.3, which hasn't fully shipped on the SPA side. The
// AgentDetailDrawer in gm-e12.4 has a transcript pane scaffold but
// the live wiring (peek polling, state.transcript_tail rendering)
// isn't in place. These specs are fixme'd until then; the contract
// is captured here so when peek ships, the tests flip on.

import { test } from '../../fixtures/server';

test.fixme(
  'GET /sessions/:id/peek returns the transcript tail @sessions @deep',
  () => {
    /* fixme: peek server endpoint is gm-e7.3; SPA consumer hasn't
       wired it. Once both land, this asserts the tail bytes flow
       into AgentDetailDrawer's transcript pane within the polling
       cadence. */
  }
);

test.fixme(
  'AgentDetailDrawer surfaces the live transcript pane when a session is selected @sessions',
  () => {
    /* fixme: AgentDetailDrawer.tsx already renders the transcript
       container (data-testid="agent-drawer-transcript") but no
       transcript copy lands today — peek consumer isn't wired. */
  }
);

test.fixme(
  'transcript tail updates within the peek polling interval @sessions @realtime',
  () => {
    /* fixme: peek can drive via short-poll OR push (gm-e4.3 SSE).
       Decide before the assertion. The DoD copy is "tail visible
       within ~250ms of bytes landing"; both delivery paths can hit
       that. */
  }
);
