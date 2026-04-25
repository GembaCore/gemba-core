// fixtures/workplane.ts — STUB
//
// In gm-5v8v.2 / .5 / .6 this fixture will expose a typed handle to
// seed the WorkPlane (work items, epics, sessions, escalations) for
// the current test. Builders sit on top of it.
//
// For now it's a placeholder so child beads can land their impls
// without touching the public import surface.

export type WorkPlaneHandle = {
  reset: () => Promise<void>;
};

export const todo = (): WorkPlaneHandle => {
  throw new Error('workplane fixture not implemented — see gm-5v8v.2 / .5');
};
