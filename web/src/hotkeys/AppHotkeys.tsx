import { useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { useHotkey } from './hooks';
import { HelpOverlay } from './HelpOverlay';

// AppHotkeys wires the default global hotkeys to concrete application actions
// that the shell can handle today (routing, palette, workspace switch). Grid /
// drawer / bulk shortcuts still fire their default window events so downstream
// components (gm-e12.3 grid, gm-e12.7 board) can bind without touching this
// file.
export function AppHotkeys() {
  const navigate = useNavigate();

  const go = useCallback((to: string) => () => navigate(to), [navigate]);
  const clickTarget = useCallback(
    (id: string) => () => {
      const el = document.querySelector<HTMLElement>(`[data-hotkey-target="${id}"]`);
      el?.click();
      el?.focus();
    },
    []
  );

  // Routes (1-5 numeric + named jumps)
  useHotkey('view-1', go('/board'));
  // 2 lands on Board's list mode with the Backlog preset — the
  // collapsed surface that absorbed the standalone /backlog page
  // (gm-e12.19.1).
  useHotkey('view-2', go('/board?view=list&preset=backlog'));
  useHotkey('view-3', go('/graph'));
  useHotkey('view-4', go('/insights'));
  useHotkey('view-5', go('/escalations'));
  useHotkey('capability-browser', go('/capabilities'));
  useHotkey('sessions-view', go('/sessions'));
  // New-session is a two-step: route to /sessions then fire the
  // page's "New session" button. The button sets data-hotkey-target
  // so clickTarget finds it regardless of scroll / overlay state.
  useHotkey('new-session', () => {
    navigate('/sessions');
    // Defer the click one tick so the route mounts and the button
    // lands in the DOM before clickTarget queries.
    setTimeout(() => {
      const el = document.querySelector<HTMLElement>('[data-hotkey-target="new-session"]');
      el?.click();
      el?.focus();
    }, 0);
  });
  // Drift doesn't have its own route yet — live inside insights until gm-e14
  // ships it. Hotkey intentionally still fires so it's muscle-memory-stable.
  useHotkey('drift-view', go('/insights'));

  // Shell actions
  useHotkey('workspace-switch', clickTarget('workspace-switcher'));
  useHotkey('open-palette', clickTarget('command-palette'));
  useHotkey('focus-search', clickTarget('command-palette'));

  return <HelpOverlay />;
}
