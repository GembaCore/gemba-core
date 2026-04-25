// fixtures/modes.ts (gm-5v8v.11).
//
// Workspace modes (unsupervised / supervised / managed) drive the
// confirmation-UX matrix from ui-spec §6.2:
//
//   * Unsupervised — toast-only after success, no pre-action dialog.
//   * Supervised   — inline confirm for single, dialog for destructive,
//                    batch summary at end of batch actions. (Default.)
//   * Managed      — blocking dialog before every mutation; summary-
//                    then-confirm pattern.
//
// Today the SPA reads NONE of this — there is no WorkspaceMode type
// in web/src, no Toaster, no mode-gated confirmation dialog, no
// typing-guard. The specs in specs/modes/**.spec.ts are all fixme'd
// against those surfaces; this fixture is the wire those specs will
// reach for once the SPA grows the surface.
//
// In fake mode: setMode() persists to a local variable. The fake-
// backend dispatcher will eventually surface the value via a
// workspace-config endpoint (currently no SPA caller exists, so the
// value is not read).
//
// In real mode: setMode() will write to workspace.toml in the
// realServer's beadsDir before the spec navigates. Implementation
// is deferred to gm-5v8v.2's deep-mode infra growing a "rewrite the
// workspace config" hook.

export type WorkspaceMode = 'unsupervised' | 'supervised' | 'managed';

// DEFAULT_MODE matches ui-spec §6.2 ("Supervised (default)"). Specs
// that don't override mode get the canonical operator experience.
export const DEFAULT_MODE: WorkspaceMode = 'supervised';

export interface ModeHandle {
  /** Current mode for the test. */
  current(): WorkspaceMode;
  /**
   * Switch the mode mid-test. Specs with multi-mode assertions (a
   * supervised → managed transition, say) can call this; most specs
   * just construct the handle with the desired mode and never set.
   *
   * In fake mode this is a synchronous local update. In real mode
   * (gm-5v8v.2 + future workspace-config rewrite hook) this will be
   * async because the gemba server has to re-read workspace.toml.
   */
  set(next: WorkspaceMode): void | Promise<void>;
}

export function createModeHandle(initial: WorkspaceMode = DEFAULT_MODE): ModeHandle {
  let current = initial;
  return {
    current: () => current,
    set: (next) => {
      current = next;
    },
  };
}
