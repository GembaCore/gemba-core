// Glue between WalkContext and the rest of the app (gm-uipx.2 / gm-0tl).
//
// Three pieces of cross-cutting wiring that don't belong inside
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
//   3. 'walk-active' scope (gm-0tl) — pushed whenever walk.active is
//      true so the n / d / Enter chords resolve to walk verbs from
//      anywhere in the app, not just /walk. R / M / X stay scoped
//      to /walk via WalkPage's useHotkeyScope('walk').
//
// The 'walk-active' scope is intentionally distinct from 'walk':
//   - 'walk' is pushed by /walk page mount → scopes the four
//     R/M/X/D toolbar verbs to that surface.
//   - 'walk-active' is pushed by walk lifecycle → scopes the global
//     navigation/lifecycle verbs (n, d, Enter) so they're inert
//     when no walk is in flight.

import { useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { useHotkey, useHotkeyRegistry } from '@/hotkeys';
import { usePmPanel } from '@/components/pm/PmPanelContext';
import { useWalk } from './WalkContext';

export function AppWalkBindings(): null {
  const walk = useWalk();
  const pm = usePmPanel();
  const navigate = useNavigate();
  const registry = useHotkeyRegistry();

  // Mirror walk.active onto the PM panel's walkActive flag. The
  // PM panel reads it to flip its render surface; the walk doesn't
  // depend on the panel directly so the two contexts stay
  // independent.
  useEffect(() => {
    pm.setWalkActive(walk.active);
  }, [walk.active, pm]);

  // Push the 'walk-active' scope while a walk is in flight so the
  // global n / d / Enter chords resolve to walk verbs. The pushScope
  // disposer pops on cleanup, so flipping walk.active off
  // automatically removes the scope.
  useEffect(() => {
    if (!walk.active) return;
    return registry.pushScope('walk-active');
  }, [walk.active, registry]);

  useHotkey('walk-toggle', () => {
    if (walk.active) {
      walk.end();
      return;
    }
    walk.start();
    navigate('/walk');
  });

  // gm-0tl: 'n' advances the walk to the next agenda item. When no
  // item is currently active, this is the first promotion (delegates
  // to walk.promoteNext). When something IS active, demote it to
  // 'queued' first so promoteNext's "skip if any active" guard lets
  // the next queued item come up. Backend semantics: PATCH agenda
  // items individually — the auto-promotion in walk.decide() lives
  // on the decide path, not the navigation path, so we have to
  // demote-then-promote explicitly here.
  useHotkey('walk-next', () => {
    if (!walk.active || !walk.walk) return;
    const current = walk.agenda.find((i) => i.status === 'active');
    void (async () => {
      if (current) {
        // setActiveItem demotes the current active to 'queued' before
        // promoting the new one, but it requires a target id. Fall
        // back to manual two-step: defer-via-status is the wrong
        // semantic (defer means "skip this walk"), so we use the
        // private setActiveItem path with the next queued item.
        const queued = walk.agenda.find(
          (i) => i.status === 'queued' && i.id !== current.id
        );
        if (!queued) return;
        await walk.setActiveItem(queued.id);
        return;
      }
      await walk.promoteNext();
    })();
  });

  // gm-0tl: Enter applies the focused SuggestedAction. The
  // SuggestedAction component (gm-4p6) marks its primary button with
  // [data-suggested-action] so this handler can find it without
  // needing a registry of nonce-bearing elements. When no such
  // element has focus, the binding is a no-op so default Enter
  // semantics (form submit, button activate) remain undisturbed.
  // Nonce-confirm payload (X-GEMBA-Confirm per gm-e8o) is the
  // SuggestedAction component's responsibility — clicking the
  // element is enough to trigger its existing onClick path.
  useHotkey('walk-suggested-apply', () => {
    if (typeof document === 'undefined') return;
    const focused = document.activeElement;
    if (!(focused instanceof HTMLElement)) return;
    if (focused.dataset.suggestedAction === undefined) return;
    focused.click();
  });

  return null;
}
