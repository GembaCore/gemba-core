// HelpTab — pinned Help tab for the Right-Hand Panel (gm-root.22.3).
//
// Responsibilities:
//   1. Registers itself as a pinned RHP tab on mount via useRhp().registerPinnedTab.
//      Returns the unregister function from the effect cleanup.
//   2. Registers its body renderer with the RhpPinnedContentProvider so
//      RhpShell can display the correct content when the Help tab is active.
//   3. The body renderer reads useLocation() to select the right route
//      module and forces ColdStartHelp when no project is active.
//   4. Falls back to DefaultHelp for unknown routes.
//
// HelpTab renders null — its role is registration only. Mount it once at
// the AppShell level, inside <RhpProvider>, <RhpPinnedContentProvider>,
// and <ProjectPickerProvider>.

import { useEffect } from 'react';
import { useLocation } from 'react-router-dom';
import { HelpCircle } from 'lucide-react';
import { useRhp } from './RhpContext';
import { useRhpPinnedContent } from './RhpPinnedContent';
import { useProjectPicker } from '@/components/projectpicker/ProjectPickerContext';
import {
  resolveHelpComponent,
  ColdStartHelp,
} from '@/help/index';

// HelpBody — the actual rendered content for the Help tab body.
// Kept as a named component so hooks (useLocation, useProjectPicker)
// run correctly inside the component tree, not in a callback.
export function HelpBody() {
  const { pathname } = useLocation();
  const { activeProject, isLoading } = useProjectPicker();

  // Cold-start: picker has finished loading but no project is active.
  const isColdStart = !isLoading && activeProject === null;

  if (isColdStart) {
    return <ColdStartHelp />;
  }

  const RouteHelp = resolveHelpComponent(pathname);
  return <RouteHelp />;
}

export function HelpTab() {
  const { registerPinnedTab } = useRhp();
  const { register } = useRhpPinnedContent();

  // Register the rail icon + metadata on mount; unregister on unmount.
  useEffect(() => {
    return registerPinnedTab({
      id: 'help',
      icon: HelpCircle,
      label: 'Help',
    });
  }, [registerPinnedTab]);

  // Register the body content renderer on mount; unregister on unmount.
  // The renderer returns a new <HelpBody /> each call so that route
  // changes (which update useLocation inside HelpBody) reflect correctly.
  useEffect(() => {
    return register('help', () => <HelpBody />);
  }, [register]);

  // HelpTab renders nothing itself — all visible output goes through
  // the RhpShell body via the registered content renderer.
  return null;
}
