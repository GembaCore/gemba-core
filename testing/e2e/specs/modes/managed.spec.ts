// specs/modes/managed.spec.ts (gm-5v8v.11).
//
// ui-spec §6.2 — Managed mode confirmation UX:
//
//   "Blocking dialog before every mutation: 'You are about to
//    [Stage gm-e3 + 2 Epics]. Confirm?' Summary-then-confirm
//    pattern mandatory."
//
// All tests fixme'd; the SPA doesn't currently gate any mutation
// behind a managed-mode confirmation dialog.

import { readFileSync } from 'node:fs';
import { test, expect } from '../../fixtures/server';

test.fixme(
  'every drag mutation opens a summary-then-confirm blocking dialog @modes',
  () => {
    /* fixme: blocked on a managed-mode confirmation dialog component
       that intercepts mutations BEFORE the PATCH fires. The "summary"
       half ("You are about to [...]") needs a deterministic
       formatter shared with the supervised batch summary —
       implementations of those two surfaces should share the
       formatter so a v.next mode-switch doesn't drift the copy. */
  }
);

test.fixme(
  'dismissing the dialog cancels the mutation (no PATCH fires) @modes',
  () => {
    // fixme: "cancel button → no PATCH" is the property that
    // distinguishes managed from supervised — supervised's
    // destructive dialog also confirms but mutation is already in
    // flight. Pin via page.waitForRequest matching /api/work-items/...
    // with method=PATCH timeout=1000 and assert it times out.
  }
);

test.fixme(
  'managed mode applies the gate to every mutation kind @modes',
  () => {
    /* fixme: ui-spec L489 says "every mutation". The gate must cover
       drag (state change), inline edit (title / priority / assignee),
       new-workitem creation, JSONL import — every PATCH and POST
       through the SPA. Spec exercises one of each kind once the
       gate ships. */
  }
);

test.fixme(
  'mode pill in the chrome reads "managed" with a red surface @modes',
  () => {
    /* fixme: ui-spec §1 L90 — red pill for managed mode. */
  }
);

test.fixme(
  'the summary reads aloud the bead ids being mutated @modes',
  () => {
    /* fixme: aria-live region carrying the summary text. Important
       for accessibility; managed-mode operators are explicitly
       opting into review of every action and the assistive-tech
       experience should mirror that. */
  }
);

// gm-5v8v.11.1 — fixture-level deep test. The Go backend doesn't yet
// READ workspace.toml (mode-gated nonce + audit-chain behavior is
// unbuilt), so the contract this spec pins is the persistence side:
// the fixture writes .gemba/workspace.toml in the per-test tempdir
// BEFORE gemba boots, the server tolerates the file, and a second
// spawn against the same workspace sees the same persisted mode.
// When the backend grows mode-gated behavior, this spec stays as the
// boot-and-persist anchor; richer assertions land alongside it.
test.describe('@deep managed mode persists in workspace.toml @modes', () => {
  test.skip(({ backend }) => backend !== 'real', 'deep-only — needs a real listener');

  test('fixture writes managed workspace.toml and gemba boots cleanly', async ({ authServer }) => {
    const srv = await authServer({ mode: 'managed' });
    expect(srv.mode).toBe('managed');
    expect(srv.workspaceTomlPath).toBeTruthy();
    const body = readFileSync(srv.workspaceTomlPath as string, 'utf8');
    expect(body).toMatch(/^mode = "managed"$/m);

    // /api/health going green proves gemba booted with the file present
    // — i.e. didn't reject the unknown-key TOML (today) and won't
    // regress when the parser lands.
    const res = await fetch(`${srv.baseURL}/api/health`);
    expect(res.status).toBe(200);
  });

  test('mode survives a server restart against the same workspace', async ({ authServer }) => {
    const first = await authServer({ mode: 'managed' });
    const tomlPath = first.workspaceTomlPath as string;
    const before = readFileSync(tomlPath, 'utf8');
    expect(before).toMatch(/^mode = "managed"$/m);

    // Each authServer() call spins its OWN tempdir, so cross-server
    // file persistence isn't what we're testing (a true "restart in
    // place" needs a kill-without-cleanup hook the fixture doesn't
    // expose). What we CAN pin: a fresh server with mode=managed
    // produces the same persisted shape regardless of which tempdir
    // it lands in. Combined with the file-content assertion above,
    // this guards the fixture-level promise: every modes-deep run
    // sees a workspace.toml that says exactly what the spec asked
    // for, byte-for-byte, every spawn.
    const second = await authServer({ mode: 'managed' });
    const after = readFileSync(second.workspaceTomlPath as string, 'utf8');
    expect(after).toMatch(/^mode = "managed"$/m);
    expect(second.mode).toBe('managed');
  });
});
