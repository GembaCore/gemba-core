// Types for the /new conversational project-creation route
// (gm-root.17.3 — see docs/design/newproject.md).

import type { ConversationMessage, NewProjectState } from '@/api/newproject';

// SessionPhase tracks the route's lifecycle:
//   - 'idle'    — initial mount, before /start lands
//   - 'starting'— /start in flight
//   - 'active'  — conversation running; turns can be submitted
//   - 'ratifying' — operator opened the confirm modal
//   - 'committing' — /ratify in flight
//   - 'done'    — server returned successfully; navigating away
//   - 'error'   — /start or /ratify failed; the page surfaces the
//                  error and offers a retry
export type SessionPhase =
  | 'idle'
  | 'starting'
  | 'active'
  | 'ratifying'
  | 'committing'
  | 'done'
  | 'error';

// SessionState is the SPA-side view of the conversation. The
// authoritative state lives on the server and is replaced wholesale
// each turn; the SPA only owns the transcript and ephemeral UI flags.
export interface SessionState {
  phase: SessionPhase;
  sessionId: string | null;
  transcript: ConversationMessage[];
  // Last server-supplied state. EMPTY_STATE before /start lands.
  state: NewProjectState;
  // Last error message, if any. Cleared on successful /turn.
  error: string | null;
  // True while a turn is being processed. Distinct from phase so the
  // input disables without flipping the whole route into a loading
  // shell.
  pendingTurn: boolean;
}
