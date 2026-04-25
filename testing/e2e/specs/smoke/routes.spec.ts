// specs/smoke/routes.spec.ts
//
// Tier: smoke. Asserts the SPA's top-level routes mount inside the
// AppShell with no console errors and no page errors. The fake
// backend fixture intercepts /api + /events so these run with no
// `gemba serve` required. The same specs run on the real backend
// in the `*-deep` projects (gm-5v8v.2 wires that up).
//
// Tags: @smoke. (No @deep — these specs are intentionally backend-
// agnostic; the matrix runs them again under deep projects.)

import { test, expect } from '../../fixtures/server';
import { AppShell } from '../../pages/AppShell';

// Top-level routes from web/src/App.tsx. /agents currently falls
// through to NotFoundPage inside AppShell — that's still a valid
// smoke target (AppShell renders, no console error).
//
// Excluded:
//   /sessions — React "Maximum update depth exceeded" in
//               NewSessionDialog (gm-dt9y). Re-add here once
//               that bead closes.
const ROUTES = [
  '/board',
  '/backlog',
  '/grid',
  // '/sessions', // re-enable after gm-dt9y
  '/agents',
  '/graph',
  '/capabilities',
  '/escalations',
] as const;

// /board/* deep-link variant — react-router uses a wildcard so
// workspace-prefixed bead ids ("gemba/gemba/gm-e1") match end-to-end
// (web/src/App.tsx:29). The smoke check is "shell renders, no
// console errors" — it's the routing layer, not the workitem load,
// that this spec exercises. The fake backend's empty-state
// /api/work-items is enough for the page to settle.
const DEEP_LINKS = [
  '/board/gemba/gemba/gm-e1',
] as const;

// Known-bug allowlist: errors we've already filed beads against and
// don't want to block the smoke tier on. Each entry MUST cite the
// bead so a reviewer can see exactly what's being suppressed and
// remove the entry when the bead closes. Keep this list short — the
// whole point of smoke is to surface unfiltered console noise.
const KNOWN_BUGS: Array<{ bead: string; match: (msg: string) => boolean }> = [
  {
    // CapabilitiesPage's useQuery({queryKey: ['adaptors'], enabled: false})
    // surfaces a "No queryFn was passed" console.error from React
    // Query 5. Tracked separately; affects /capabilities only.
    bead: 'gm-pz1b',
    match: (msg) => msg.includes('"adaptors"') && msg.includes('No queryFn was passed'),
  },
];

function filterKnown(errors: string[]): string[] {
  return errors.filter((msg) => !KNOWN_BUGS.some((b) => b.match(msg)));
}

for (const route of ROUTES) {
  test(`${route} mounts AppShell with no console errors @smoke`, async ({
    page,
    consoleErrors,
  }) => {
    const shell = new AppShell(page);
    await shell.goto(route);
    await shell.expectShellRendered();
    const errors = filterKnown(consoleErrors);
    expect(
      errors,
      `console errors on ${route}: ${errors.join(' | ')}`,
    ).toEqual([]);
  });
}

for (const route of DEEP_LINKS) {
  test(`${route} (deep-link) mounts AppShell with no console errors @smoke`, async ({
    page,
    consoleErrors,
    workPlane,
  }) => {
    // Seed the fake WorkPlane with the deep-linked id so the drawer
    // fetch resolves cleanly. Without this seed, the fake fixture
    // returns 404 for the work-item GET (correct behaviour — gm-e1
    // doesn't exist in fake state) and that surfaces as a console
    // error masking the routing-layer assertion this test cares about.
    const id = decodeURIComponent(route.replace(/^\/board\//, ''));
    workPlane.seed([
      {
        id,
        kind: 'task',
        title: 'smoke deep-link target',
        status: 'open',
        state_category: 'unstarted',
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
      },
    ]);

    const shell = new AppShell(page);
    await shell.goto(route);
    await shell.expectShellRendered();
    const errors = filterKnown(consoleErrors);
    expect(
      errors,
      `console errors on ${route}: ${errors.join(' | ')}`,
    ).toEqual([]);
  });
}
