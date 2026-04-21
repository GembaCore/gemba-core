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
  useHotkey('view-2', go('/backlog'));
  useHotkey('view-3', go('/graph'));
  useHotkey('view-4', go('/insights'));
  useHotkey('view-5', go('/escalations'));
  useHotkey('capability-browser', go('/capabilities'));
  // Drift doesn't have its own route yet — live inside insights until gm-e14
  // ships it. Hotkey intentionally still fires so it's muscle-memory-stable.
  useHotkey('drift-view', go('/insights'));

  // Shell actions
  useHotkey('workspace-switch', clickTarget('workspace-switcher'));
  useHotkey('open-palette', clickTarget('command-palette'));
  useHotkey('focus-search', clickTarget('command-palette'));

  return <HelpOverlay />;
}
