// specs/smoke/axe.spec.ts
//
// Tier: smoke. Runs an axe-core accessibility scan against each
// top-level route and fails on serious+ violations. Per ui-spec
// §9.1 the WCAG bar is best-effort — the smoke tier only catches
// regressions on rules the SPA already passes. New violations
// surface as test failures here; existing waivers belong in the
// IGNORED set with a beads link.
//
// Tags: @smoke. Backend-agnostic — runs in fake and (eventually)
// deep projects from the same file.

import AxeBuilder from '@axe-core/playwright';
import { test, expect } from '../../fixtures/server';
import { AppShell } from '../../pages/AppShell';

// Mirrors the smoke routes list. Keep in sync with routes.spec.ts;
// the two specs intentionally exercise the same surface so a route
// failing one tier rule (render or a11y) still surfaces.
const ROUTES = [
  '/board',
  '/backlog',
  '/grid',
  '/agents',
  '/graph',
  '/escalations',
] as const;

// Axe rules we knowingly ship violations on. Each entry MUST link a
// beads id so the waiver stays auditable. Remove the rule here once
// the linked bead closes — that turns the rule back into a smoke
// regression target.
const IGNORED_RULES: readonly string[] = [
  // gm-4chm — Tailwind neutral-500 text on neutral-100 backgrounds
  // (e.g. the ⌘K kbd hint in the topbar) lands at 4.34:1 contrast
  // where WCAG AA wants 4.5:1. Systemic across smoke routes; tracked
  // as a design-token fix in the linked bead.
  'color-contrast',
];

for (const route of ROUTES) {
  test(`${route} passes axe-core (serious+) @smoke`, async ({ page }) => {
    const shell = new AppShell(page);
    await shell.goto(route);
    await shell.expectShellRendered();

    const results = await new AxeBuilder({ page })
      .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
      .disableRules([...IGNORED_RULES])
      .analyze();

    // Fail on serious+ violations. Minor + moderate are surfaced in
    // the report but don't block the smoke tier (ui-spec §9.1
    // best-effort posture).
    const blocking = results.violations.filter(
      (v) => v.impact === 'serious' || v.impact === 'critical',
    );

    expect(
      blocking,
      `axe violations on ${route}:\n${formatViolations(blocking)}`,
    ).toEqual([]);
  });
}

function formatViolations(
  violations: { id: string; impact?: string | null; description: string; nodes: { target: unknown[] }[] }[],
): string {
  if (violations.length === 0) return '(none)';
  return violations
    .map(
      (v) =>
        `  - [${v.impact ?? '?'}] ${v.id}: ${v.description} (${v.nodes.length} node${v.nodes.length === 1 ? '' : 's'})`,
    )
    .join('\n');
}
