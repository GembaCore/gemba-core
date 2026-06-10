// fixtures/sse.ts — STUB
//
// Realtime tier (gm-5v8v.10) needs a fixture that can push synthetic
// SSE events into the page so the spec can assert react-query cache
// invalidation. Today the fake backend in fixtures/server.ts replies
// with an idle stream; this stub will let specs `emit({...})`.

export type SseHandle = {
  emit: (event: string, data: unknown) => Promise<void>;
};

export const todo = (): SseHandle => {
  throw new Error('sse fixture not implemented — see gm-5v8v.10');
};
