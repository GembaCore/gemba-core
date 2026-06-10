// specs/error/pending.spec.ts — gm-5v8v.13 living-spec gaps.
//
// ui-spec §6.5 names five error surfacings; gm-5v8v.13's bead lists
// six specs. The two below are documented but skipped because the
// SPA / server surfaces they exercise don't exist yet. Each carries
// a tracking note so the contributor who lands the SPA / server work
// knows which spec to un-fixme.

import { test } from '../../fixtures/server';

test.describe('@error pending — surfaces not yet built', () => {
  // ui-spec §6.5: "Rate-limit | Backoff-toast with ETA; queue
  // visible in Insights (adaptor-level health section)". Neither
  // the toast UI nor the Insights health section is implemented —
  // /insights is still a placeholder (web/src/pages/placeholders.tsx).
  test.skip('rate-limit toast surfaces ETA + queue depth (ui-spec §6.5)', () => {
    // 1. Trigger a 429 from /api/work-items (page.route)
    // 2. Assert toast: data-testid="rate-limit-toast" visible
    // 3. Assert ETA shown matches Retry-After header
    // 4. Navigate to /insights → adaptor health section shows queue depth
  });

  // ui-spec §4.8 + §6.5: per-Epic fetch errors render a tiny red '!'
  // badge on the affected card; clicking reveals the detail. Today
  // BoardPage either renders the empty state, the loaded epic set,
  // or an error block that replaces the columns entirely — there is
  // no per-card error rendering.
  test.skip('per-Epic fetch error shows red ! badge on affected card (ui-spec §4.8)', () => {
    // 1. Seed three epics; mock /api/work-items/:one to 500
    // 2. Navigate to /board
    // 3. Assert the red-! badge is visible on the failing epic only
    // 4. Click the badge; detail pane shows the error message
  });

  // gm-5v8v.13 bead names this. The preamble path lives entirely
  // server-side (internal/adapter/native/start.go). The "must not
  // abort" guarantee is a server invariant: a Go integration test
  // with a stubbed WorkPlane that errors on GetWorkItem. Filing as
  // a Go-test follow-up rather than a Playwright spec — the e2e
  // surface here is "session pane spawned successfully", which is
  // already covered by the smoke-deep dispatch chain.
  test.skip('preamble GetWorkItem failure does not abort session spawn', () => {
    // Out of scope for Playwright e2e — see internal/adapter/native
    // for the integration test surface.
  });
});
