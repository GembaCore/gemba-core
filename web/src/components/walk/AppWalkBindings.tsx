// Glue between WalkContext and the rest of the app (gm-uipx.2).
//
// Two pieces of cross-cutting wiring that don't belong inside
// WalkContext (which should stay decoupled from PmPanel + routing)
// or inside individual pages (which would each duplicate the
// effect):
//
//   1. PM panel takeover sync — when walk.active flips, mirror it
//      onto pm.setWalkActive so the PM panel switches to the walk
//      chat surface globally, not just on /walk.
//   2. Global Cmd-G hotkey — toggles the walk and (when starting)
//      navigates to /walk so the operator lands on the agenda
//      surface immediately.

import { useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { useHotkey } from '@/hotkeys';
import { usePmPanel } from '@/components/pm/PmPanelContext';
import { useWalk } from './WalkContext';

export function AppWalkBindings(): null {
  const walk = useWalk();
  const pm = usePmPanel();
  const navigate = useNavigate();

  // Mirror walk.active onto the PM panel's walkActive flag. The
  // PM panel reads it to flip its render surface; the walk doesn't
  // depend on the panel directly so the two contexts stay
  // independent.
  useEffect(() => {
    pm.setWalkActive(walk.active);
  }, [walk.active, pm]);

  useHotkey('walk-toggle', () => {
    if (walk.active) {
      walk.end();
      return;
    }
    walk.start();
    navigate('/walk');
  });
  return null;
}
