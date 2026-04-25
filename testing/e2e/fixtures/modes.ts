// fixtures/modes.ts — STUB
//
// Workspace modes (unsupervised / supervised / managed) drive the
// confirmation-UX matrix. gm-5v8v.11 lands the real fixture.

export type WorkspaceMode = 'unsupervised' | 'supervised' | 'managed';

export type ModeHandle = {
  set: (mode: WorkspaceMode) => Promise<void>;
};

export const todo = (): ModeHandle => {
  throw new Error('modes fixture not implemented — see gm-5v8v.11');
};
