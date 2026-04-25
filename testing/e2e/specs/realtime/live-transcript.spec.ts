// specs/realtime/live-transcript.spec.ts
//
// Tier: realtime. Owner: gm-5v8v.10. Subject: AgentDetailDrawer live
// transcript pane (gm-5uqu) — pane output streams into the drawer
// via repeated GET /api/sessions/:id/peek polls (gm-e7.3) coalesced
// into a smooth scroll.
//
// Pre-req for activation: a deep-mode fixture variant that registers
// the native orchestration plane (gemba serve --orchestration=native
// --terminal=tmux) and a session running so /api/sessions/:id/peek
// can actually return a transcript. The current gm-5v8v.2 fixture
// boots gemba serve WITHOUT the orchestration plane — covering the
// full live-transcript chain needs gm-5v8v.15 (hello-world migrate,
// which sets up the tmux+orchestration variant) or a sibling fixture
// extension.
//
// Until that lands, the spec is parked here as a living spec so the
// test exists when the orchestration variant ships.

import { test } from '../../fixtures/server';

test.describe('@deep @realtime AgentDetailDrawer live transcript', () => {
  test.skip(true, 'gates on orchestration-enabled deep fixture (gm-5v8v.15 follow-up)');

  test('pane stdout surfaces in the drawer transcript pane', () => {
    // 1. Spawn a session via POST /api/sessions (orchestration=native)
    // 2. Open the AgentDetailDrawer for that session
    // 3. SendKeys output into the underlying tmux pane
    // 4. Assert the drawer's transcript pane reflects the output
    //    within the peek poll budget (~2s)
  });
});
