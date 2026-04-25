// specs/grid/default-views.spec.ts — gm-5v8v.6 (in progress)
//
// ui-spec §6.8 calls for canonical default views in the grid:
// "Staged", "In Progress", "Blocked", "Ready to stage", "Recently
// Done". The current GridPage exposes a single state-chip filter
// (one click = one state) and a search input — there is no concept
// of a named, shareable view URL yet.
//
// When the SPA grows the named-views surface (likely a child of
// gm-e12.3), these fixmes graduate to real specs that exercise
// each view's chip arrangement + URL params.

import { test } from '../../fixtures/server';

test.describe('Grid default views (ui-spec §6.8) @route', () => {
  test.fixme('Staged view filters to state_category=staged', async () => {});
  test.fixme('In Progress view filters to state_category=started', async () => {});
  test.fixme('Blocked view derives blocked rows from open escalations + edges', async () => {});
  test.fixme('Ready to stage view filters by derived.agent_claimable', async () => {});
  test.fixme('Recently Done view filters to state_category=completed sorted by updated_at desc', async () => {});
});
