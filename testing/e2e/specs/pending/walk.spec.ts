// specs/pending/walk.spec.ts — gm-5v8v.14
//
// ui-spec §5.4 — Gemba walk (`/walk` or main-content takeover).
//
// Status: PENDING IMPLEMENTATION. Two-pane layout (agenda left,
// chat right) with a kanban-mini sub-layout for agenda items
// (Queued / Active / Decided / Deferred). PM panel pinned to bottom.
//
// Tracking: file a feat(spa): Gemba walk surface bead when work
// starts; reference it here.

import { test } from '../../fixtures/server';

test.describe('@pending /walk (ui-spec §5.4)', () => {
  test.skip(true, '/walk not yet implemented — see ui-spec §5.4');

  test('two-pane layout: agenda 360px left, chat flex right', () => {
    // Confirm getByTestId('walk-agenda-pane') + walk-chat-pane,
    // assert agenda pane width ≈360px.
  });

  test('agenda items show source-kind icon + drag handle + decided checkmark', () => {
    // Sources: ● open escalation, ◉ HITL question, ◆ filed bead,
    // ◇ closed bead, ★ user-added.
  });

  test('Ratify / Modify / Reject / Defer hotkeys (R/M/X/D) decide active item', () => {
    // From ui-spec §6.6 — single-key decisions in the walk.
  });

  test('active-walk banner across top of content area shows progress', () => {
    // Yellow-tinted banner: "Gemba walk active · 4 of 11 decided ·
    // [resume / end]".
  });

  test('Cmd-G toggles walk on/off globally', () => {
    // Per ui-spec §6.6.
  });

  test('PM panel becomes the walk chat surface during a walk', () => {
    // Ratification per agenda item; volunteered perspectives surface
    // as inset quote-style sub-turns.
  });
});
