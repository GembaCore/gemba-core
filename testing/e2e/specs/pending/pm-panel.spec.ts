// specs/pending/pm-panel.spec.ts — gm-5v8v.14
//
// ui-spec §2.4 — PM panel (bottom drawer).
//
// Status: PENDING IMPLEMENTATION. Bottom drawer that overlays content
// on expand; persists state across reloads; Cmd-P / Ctrl-P toggles.
// Active conversation shows terminal-style chat with explicit speaker
// labels; persona dropdown above input; cost display in header.
//
// Tracking: file a feat(spa): PM panel surface bead when work
// starts; reference it here.

import { test } from '../../fixtures/server';

test.describe('@pending PM panel (ui-spec §2.4)', () => {
  test.skip(true, 'PM panel not yet implemented — see ui-spec §2.4');

  test('Cmd-P / Ctrl-P toggles the panel from collapsed to expanded', () => {
    // Hotkey global; works on every route.
  });

  test('default state is open-last-state on reload', () => {
    // Storage persistence across page navigations + reloads.
  });

  test('Escape collapses without losing conversation state', () => {
    // Per ui-spec §2.4 — non-destructive collapse.
  });

  test('persona dropdown above input switches active Coach/Manager', () => {
    // Dropdown lists currently-enabled personas; switching mid-
    // conversation persists per-persona context.
  });

  test('cost display in header shows running session total + per-turn on hover', () => {
    // "$0.34 this session · ~$0.003/turn"; per-turn on hover.
  });

  test('SuggestedActions group renders at bottom of relevant turn', () => {
    // Each action is an apply-or-dismiss control; ExecutedActions
    // get a green-check + "applied" badge.
  });

  test('Gemba walk takes over the panel when active, restores on exit', () => {
    // ui-spec §5.4 — walk takes the chat surface; exit returns the
    // last non-walk conversation.
  });
});
