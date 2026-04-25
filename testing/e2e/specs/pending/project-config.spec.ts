// specs/pending/project-config.spec.ts — gm-5v8v.14
//
// ui-spec §5.16 — Project config (`/project/config`).
//
// Status: PENDING IMPLEMENTATION. Single-scroll page with sticky
// section nav: Values / Guardrails / Goals / Personas / Adaptors /
// Packs / Integrations / Mode / Subscriptions / Workspace repos /
// Advanced.
//
// Tracking: file a feat(spa): /project/config surface bead when
// work starts; reference it here.

import { test } from '../../fixtures/server';

test.describe('@pending /project/config (ui-spec §5.16)', () => {
  test.skip(true, '/project/config not yet implemented — see ui-spec §5.16');

  test('sticky section nav lists all 11 sections in order', () => {
    // Sections must match ui-spec §5.16 exactly so deep-link
    // anchors stay stable.
  });

  test('Values editor is a modal with priority-ranked statement table', () => {
    // Add / edit / remove / rank up-down via DnD.
  });

  test('Adaptors section is read-only with link to .gemba/workspace.toml', () => {
    // ui-spec §8.1: "Switch-adaptor UX: forbidden via UI in v1".
  });

  test('Workspace repos section lists repositories from RepositoryRegistry', () => {
    // Per gm-26n4 — primary + sibling registry should render here.
  });
});
