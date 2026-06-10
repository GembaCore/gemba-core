// specs/pending/plan.spec.ts — gm-5v8v.14
//
// ui-spec §5.1 — Plan view (`/plan`).
//
// Status: PENDING IMPLEMENTATION. Route is unrouted today (no entry
// in web/src/App.tsx). When it ships, the page should expose a
// vertical list of Staged Epics grouped by parallel-group, with
// per-Epic priority / readiness / token budget / drag handle and an
// "Execute all Staged" primary CTA + cost preview at the top.
//
// Tracking: file a feat(spa): /plan view bead when implementation
// starts; reference it here.

import { test, expect } from '../../fixtures/server';

test.describe('@pending /plan (ui-spec §5.1)', () => {
  test.skip(true, '/plan not yet implemented — see ui-spec §5.1');

  test('Execute all Staged primary CTA at top-right with Cmd-Enter shortcut', async ({ page }) => {
    await page.goto('/plan');
    await expect(page.getByTestId('plan-execute-all')).toBeVisible();
    // Cmd-Enter dispatches the Staged set in one nonce-batched call.
  });

  test('Staged Epics grouped by parallel-group in outlined clusters', async () => {
    // Each parallel-group renders inside a bordered cluster; epics
    // inside the cluster reorder via drag.
  });

  test('cost preview reflects sum of selected Staged epics', async () => {
    // Top-of-page banner reads "Total cost estimate: $X.XX · ~Nk
    // tokens" and updates as the operator drags items in/out.
  });

  test('post-execute navigates to Board with In Progress transitions visible', async () => {
    // Click Execute → POST /api/sessions for each Staged epic →
    // navigate to /board → epics show animated transition from
    // Staged column to In Progress.
  });
});
