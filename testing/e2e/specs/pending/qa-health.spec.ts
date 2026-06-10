// specs/pending/qa-health.spec.ts — gm-5v8v.14
//
// ui-spec §5.12 — QA Health (`/qa/health`). Note: distinct from
// the existing `/health` placeholder which exposes /api/health.
//
// Status: PENDING IMPLEMENTATION. Suites grouped by scope (adaptor /
// core / transport / ui / e2e); per-suite row shows status, coverage
// delta, flake rate, "Run now" with depth selector.
//
// Tracking: file a feat(spa): /qa/health surface bead when work
// starts; reference it here.

import { test } from '../../fixtures/server';

test.describe('@pending /qa/health (ui-spec §5.12)', () => {
  test.skip(true, '/qa/health not yet implemented — see ui-spec §5.12');

  test('suites grouped by scope (adaptor / core / transport / ui / e2e)', () => {
    // 5 collapsible groups, ordered as in the spec.
  });

  test('per-suite row: name / scope / depth / last-run / coverage delta / flake rate', () => {
    // Status pill: green / yellow / red. Coverage delta vs last run.
  });

  test('Run now button has depth selector (fast / slow / complete)', () => {
    // Dropdown-attached primary button.
  });

  test('Bulk "Run all" with depth selector', () => {
    // Top-right of page; nonce-gated.
  });
});
