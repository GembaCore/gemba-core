// /walk page (gm-i65). Three-pane layout per gm-3nk §UI:
//
//   ┌──────────┬─────────────────────┬──────────┐
//   │  Agenda  │    Chat / Compose   │  Open    │
//   │ (lanes)  │                     │  esc.+   │
//   │          │                     │  budget  │
//   ├──────────┴─────────────────────┴──────────┤
//   │  N decisions · M beads · $X · duration    │
//   └────────────────────────────────────────────┘
//
// Visiting /walk implicitly starts a walk if one isn't in flight, so
// the operator doesn't have to hunt for a "begin" button. The active
// walk's id persists in WalkContext (localStorage) so leaving /walk
// preserves the takeover.

import { useEffect } from 'react';
import { useHotkeyScope } from '@/hotkeys';
import { AgendaPane } from '@/components/walk/AgendaPane';
import { BottomBar } from '@/components/walk/BottomBar';
import { ChatPane } from '@/components/walk/ChatPane';
import { RightPane } from '@/components/walk/RightPane';
import { useWalk } from '@/components/walk/WalkContext';
import { usePmPanel } from '@/components/pm/PmPanelContext';

export function WalkPage(): JSX.Element {
  const walk = useWalk();
  const pm = usePmPanel();
  // Activate walk-scoped hotkeys (R/M/X/D) only while this page is
  // mounted. Other surfaces keep their default bindings.
  useHotkeyScope('walk');

  // Visiting /walk starts a walk if one isn't in flight. Mount-only
  // effect: re-running on every state change would fight an explicit
  // End (which clears the current walk id), so we kick off once and
  // let the lifecycle controls own further transitions.
  useEffect(() => {
    if (!walk.walk && !walk.loading) {
      void walk.start();
    }
    pm.setWalkActive(true);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return (
    <div data-testid="walk-page" className="flex h-full min-h-0 flex-col">
      <div className="flex min-h-0 flex-1">
        <AgendaPane />
        <ChatPane />
        <RightPane />
      </div>
      <BottomBar />
    </div>
  );
}
