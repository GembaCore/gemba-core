// specs/modes/unsupervised.spec.ts (gm-5v8v.11).
//
// ui-spec §6.2 — Unsupervised mode confirmation UX:
//
//   "Toast-only ('Staged Epic gm-e3') after success; no pre-action
//    dialog."
//
// The SPA does not yet ship a Toaster component, a workspace-mode
// reader, or any mode-gated confirmation logic. Every test in this
// file is fixme'd against those surfaces; the file exists as the
// executable contract anchor so when the feature lands, the DoD is
// already enforceable.

import { readFileSync } from 'node:fs';
import { test, expect } from '../../fixtures/server';

test.fixme(
  'drag epic to In Progress shows a Toast and does NOT open a confirm dialog @modes',
  () => {
    /* fixme: blocked on (a) SPA Toaster component, (b) workspace-mode
       reader (e.g. useWorkspaceMode hook fed by /api/workspace), and
       (c) BoardPage gating the post-mutation Toast on mode === 'unsupervised'.
       Spec body once unblocked: set fixture mode to 'unsupervised',
       seed an epic, drag → expect getByRole('status') with the
       canonical "Staged Epic gm-eN" copy, and expect no role=dialog. */
  }
);

test.fixme(
  'multi-card bulk action emits a Toast per success without a batch summary dialog @modes',
  () => {
    /* fixme: same blocker as above + multi-select wiring (gm-5v8v.5
       fixme'd). Toast content per ui-spec L487 reads as one toast
       per resolved mutation, not a dialog summary. */
  }
);

test.fixme(
  'destructive actions (cancel In-Progress Epic) STILL require typing-guard in unsupervised @modes',
  () => {
    /* fixme: ui-spec §6.2 L491 — "Destructive actions ALWAYS require
       typing-guard, regardless of mode." Cross-mode invariant
       covered separately in typing-guard.spec.ts; pinning here so
       the unsupervised file documents the carve-out where toast-
       only does NOT apply. */
  }
);

// gm-5v8v.11.1 — fixture-level deep test. The Go backend's audit log
// is mode-agnostic today; this spec pins the persistence contract so
// when the audit envelope grows a mode field, the file the server
// reads at boot is already known-good. See the parallel spec in
// supervised.spec.ts and managed.spec.ts.
test.describe('@deep server respects unsupervised mode @modes', () => {
  test.skip(({ backend }) => backend !== 'real', 'deep-only — needs a real listener');

  test('fixture writes unsupervised workspace.toml and gemba boots cleanly', async ({ authServer }) => {
    const srv = await authServer({ mode: 'unsupervised' });
    expect(srv.mode).toBe('unsupervised');
    expect(srv.workspaceTomlPath).toBeTruthy();
    const body = readFileSync(srv.workspaceTomlPath as string, 'utf8');
    expect(body).toMatch(/^mode = "unsupervised"$/m);

    const res = await fetch(`${srv.baseURL}/api/health`);
    expect(res.status).toBe(200);
  });
});
