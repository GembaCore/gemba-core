// /walk page (gm-uipx.2). Two-pane layout per ui-spec §5.4 —
// agenda 360px on the left, chat pane flex on the right. Visiting
// /walk implicitly starts the walk so the operator doesn't need to
// hunt for a "begin" button; the active flag persists in the
// WalkContext so the takeover stays live as the operator clicks
// elsewhere.

import { useEffect } from 'react';
import { useHotkeyScope } from '@/hotkeys';
import { AgendaPane } from '@/components/walk/AgendaPane';
import { ChatPane } from '@/components/walk/ChatPane';
import { useWalk } from '@/components/walk/WalkContext';
import { usePmPanel } from '@/components/pm/PmPanelContext';

export function WalkPage(): JSX.Element {
  const walk = useWalk();
  const pm = usePmPanel();
  // Activate walk-scoped hotkeys (R/M/X/D) only while this page
  // is mounted. Other surfaces keep their default bindings.
  useHotkeyScope('walk');

  // Visiting /walk starts the walk if it isn't already active.
  // Mount-only effect: re-running on every walk state change would
  // fight the End-walk button (which flips active=false), so we
  // start once and let the explicit lifecycle controls own further
  // transitions. AppWalkBindings keeps PM panel walkActive in sync
  // with walk.active globally, so leaving /walk preserves the walk.
  useEffect(() => {
    if (!walk.active) walk.start();
    pm.setWalkActive(true);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return (
    <div data-testid="walk-page" className="flex h-full min-h-0">
      <AgendaPane />
      <ChatPane />
    </div>
  );
}
