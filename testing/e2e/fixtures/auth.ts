// fixtures/auth.ts — STUB
//
// gm-5v8v.12 lands real-token + cookie + middleware-enforcement
// fixtures. Until then specs that need auth just fail their preflight.

export type AuthHandle = {
  loginAs: (role: 'overseer' | 'mayor' | 'crew' | 'witness') => Promise<void>;
};

export const todo = (): AuthHandle => {
  throw new Error('auth fixture not implemented — see gm-5v8v.12');
};
