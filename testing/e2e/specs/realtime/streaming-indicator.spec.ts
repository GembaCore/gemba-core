// specs/realtime/streaming-indicator.spec.ts
//
// Tier: realtime. Owner: gm-5v8v.10. Subject: ui-spec §6.3 "small
// live-dot in top bar" — a per-SSE-frame indicator that the stream
// is alive.
//
// Status: PENDING IMPLEMENTATION. The Topbar component
// (web/src/components/Topbar.tsx) does not yet expose a streaming-
// indicator element. ui-spec §6.3 specifies:
//
//   - small live-dot in top bar
//   - subtle fade-in when data arrives
//   - no aggressive re-flow (user was looking at something)
//
// File a sibling SPA bead to add this dot, then un-skip below. Use
// data-testid="streaming-indicator" so the spec can target it
// without reaching for class selectors.

import { test } from '../../fixtures/server';

test.describe('@realtime Topbar streaming indicator (ui-spec §6.3)', () => {
  test.skip(true, 'top-bar live-dot not implemented yet — file SPA bead');

  test('lights up when an SSE frame arrives, fades after idle', () => {
    // 1. Navigate to /board (any route — the indicator is global)
    // 2. Assert getByTestId('streaming-indicator') is rendered, dim
    // 3. Trigger a server-side event (bd create in @deep, or a
    //    fixture-pushed event in fake)
    // 4. Assert the indicator transitions to the active style
    //    within ~250ms (ui-spec §1.4 motion budget)
    // 5. After ~1.5s of idle, assert the indicator returns to dim
  });
});
