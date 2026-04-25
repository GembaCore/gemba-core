// specs/grid/default-views.spec.ts — gm-5v8v.6 (closed)
//
// ui-spec §6.8 calls for canonical default views in the grid:
// "Staged", "In Progress", "Blocked", "Ready to stage", "Recently
// Done". The current GridPage exposes a single state-chip filter
// (one click = one state) and a search input — there is no concept
// of a named, shareable view URL yet.
//
// Tracking: gm-5v8v.6.3 (feat(spa): GridPage named default views).
// Un-fixme each test below when that bead closes.

import { test } from '../../fixtures/server';

test.describe('Grid default views (ui-spec §6.8) @route', () => {
  test.fixme('Staged view filters to state_category=staged', async () => {});
  test.fixme('In Progress view filters to state_category=started', async () => {});
  test.fixme('Blocked view derives blocked rows from open escalations + edges', async () => {});
  test.fixme('Ready to stage view filters by derived.agent_claimable', async () => {});
  test.fixme('Recently Done view filters to state_category=completed sorted by updated_at desc', async () => {});
});
