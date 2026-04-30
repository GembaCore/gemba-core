// gm-root.27.14 — native variant teardown.
//
// Idempotent wrapper around ProjectHandle.cleanup() (gm-root.27.3) that
// adds structured logging + survives partial-success states (e.g., gemba
// died mid-test, the tempdir was already removed). Composed by the
// gastown teardown (gm-root.27.17) AFTER its UI-driven rig/polecat
// removal so every variant ends in the same hermetic empty state.
//
// We deliberately delegate the hard part (kill children, free ports, rm
// tempdir) to the bootstrap handle's cleanup — duplicating that logic
// here would drift over time. This wrapper's contract is purely:
// "called once: works; called again: no-op; threw mid-cleanup: logged
// and swallowed".

import type { ProjectHandle } from '../../shared/helpers/bootstrap';

export type NativeHandle = {
  /** Returned by bootstrapProject(). */
  project: ProjectHandle;
  /** Sink for cleanup actions; surfaces in the test report. */
  log?: (msg: string) => void;
};

/**
 * Tear down everything bootstrapProject brought up. Safe to call from
 * t.Cleanup and safe to call twice.
 */
export async function cleanupNative(handle: NativeHandle): Promise<void> {
  const log = handle.log ?? ((m: string) => console.log(`[cleanupNative] ${m}`));
  const state = stateOf(handle);
  if (state.disposed) {
    log('already disposed — skipping');
    return;
  }
  state.disposed = true;
  try {
    await handle.project.cleanup();
    log(`disposed project ${handle.project.projectName} (${handle.project.projectDir})`);
  } catch (err) {
    log(`project.cleanup() threw: ${(err as Error).message}`);
  }
}

const DISPOSED = new WeakMap<NativeHandle, { disposed: boolean }>();
function stateOf(h: NativeHandle): { disposed: boolean } {
  let s = DISPOSED.get(h);
  if (!s) {
    s = { disposed: false };
    DISPOSED.set(h, s);
  }
  return s;
}
