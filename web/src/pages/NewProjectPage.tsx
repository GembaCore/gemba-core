// /new — full-page conversational project creation (gm-root.17.3).
//
// Two-pane layout per docs/design/newproject.md "Surfaces" section:
//
//   ┌──────────────────────┬───────────────────────┐
//   │  Conversation pane   │  Plan preview pane    │
//   │  (skill transcript)  │  (Milestones → Epics  │
//   │                      │   → Beads + draft md) │
//   │                      │                       │
//   └──────────────────────┴───────────────────────┘
//                                       [ Ratify ]
//
// After successful ratification the page transitions into the "Start
// planning" handoff screen (gm-root.17.7) — a full-page replacement
// of the two-pane layout with two CTAs:
//
//   Start planning → /walk (Gemba walk surface, gm-3nk)
//   Skip           → /gemba (dashboard)
//
// One-shot persistence — refresh / disconnect / restart loses the
// session. The route does NOT autosave or attempt to resume; the
// design doc is explicit.
//
// Backend wiring: the `newproject` skill (gm-root.17.5), the
// Onboarder persona (gm-root.17.10), and the atomic-ratify backend
// (gm-root.17.6) do NOT exist yet. The route POSTs to:
//
//   POST /api/v1/newproject/start
//   POST /api/v1/newproject/:id/turn
//   POST /api/v1/newproject/:id/ratify
//
// The fake-mode dispatcher in testing/e2e/fixtures/server.ts mocks
// these endpoints so the route renders + transitions + commits
// end-to-end. When the backend lands, no SPA changes are required.

import { useCallback, useEffect, useRef, useState } from 'react';
import { Sparkles } from 'lucide-react';
import {
  EMPTY_STATE,
  ratifyNewProject,
  startNewProject,
  submitTurn,
  type ConversationMessage,
  type NewProjectState,
  type RatifyResponse,
} from '@/api/newproject';
import { ConversationPane } from '@/components/newproject/ConversationPane';
import { PlanPreviewPane } from '@/components/newproject/PlanPreviewPane';
import { RatifyModal } from '@/components/newproject/RatifyModal';
import { RatifyDoneScreen } from '@/components/newproject/RatifyDoneScreen';
import type { SessionState } from '@/components/newproject/types';

const INITIAL: SessionState = {
  phase: 'idle',
  sessionId: null,
  transcript: [],
  state: EMPTY_STATE,
  error: null,
  pendingTurn: false,
};

export function NewProjectPage(): JSX.Element {
  const [session, setSession] = useState<SessionState>(INITIAL);
  // Track in-place plan edits the operator has made since the last
  // /turn so the next message carries them as `edits`. Cleared after
  // every successful turn.
  const pendingEditsRef = useRef<NewProjectState | null>(null);
  // Bumped after every successful turn so the input clears.
  const [resetToken, setResetToken] = useState(0);
  // Sticky nonce per ratify attempt — pinned by the modal but the
  // host owns the network call so we hold onto it.
  const ratifyNonceRef = useRef<string | null>(null);
  // Set to the ratify response once the commit succeeds. When set,
  // the page renders the RatifyDoneScreen in place of the two-pane
  // layout (gm-root.17.7).
  const [ratifyResult, setRatifyResult] = useState<RatifyResponse | null>(null);

  // Start a fresh session on mount. One-shot — refresh starts a new
  // session because the server's in-memory state went away with the
  // old request lifecycle.
  useEffect(() => {
    let cancelled = false;
    setSession((s) => ({ ...s, phase: 'starting', error: null }));
    startNewProject()
      .then((res) => {
        if (cancelled) return;
        const transcript: ConversationMessage[] = res.greeting
          ? [
              {
                id: 'greeting',
                role: 'assistant',
                content: res.greeting,
                at: new Date().toISOString(),
              },
            ]
          : [];
        setSession({
          phase: 'active',
          sessionId: res.session_id,
          transcript,
          state: res.state ?? EMPTY_STATE,
          error: null,
          pendingTurn: false,
        });
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        setSession((s) => ({
          ...s,
          phase: 'error',
          error: err instanceof Error ? err.message : String(err),
        }));
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const onPlanEdit = useCallback((next: NewProjectState) => {
    setSession((s) => ({ ...s, state: next }));
    pendingEditsRef.current = next;
  }, []);

  const onSend = useCallback(
    async (message: string) => {
      if (!session.sessionId || session.pendingTurn) return;
      const userMsg: ConversationMessage = {
        id: `user-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
        role: 'user',
        content: message,
        at: new Date().toISOString(),
      };
      // Optimistic transcript update + lock the input.
      setSession((s) => ({
        ...s,
        transcript: [...s.transcript, userMsg],
        pendingTurn: true,
        error: null,
      }));
      const edits = pendingEditsRef.current
        ? { state: pendingEditsRef.current }
        : undefined;
      try {
        const res = await submitTurn(session.sessionId, { message, edits });
        const assistantMsg: ConversationMessage = {
          id: res.reply_id || `assistant-${Date.now()}`,
          role: 'assistant',
          content: res.reply,
          at: res.reply_at || new Date().toISOString(),
        };
        setSession((s) => ({
          ...s,
          transcript: [...s.transcript, assistantMsg],
          state: res.state ?? s.state,
          pendingTurn: false,
        }));
        pendingEditsRef.current = null;
        setResetToken((n) => n + 1);
      } catch (err) {
        setSession((s) => ({
          ...s,
          pendingTurn: false,
          error: err instanceof Error ? err.message : String(err),
        }));
      }
    },
    [session.sessionId, session.pendingTurn]
  );

  const openRatify = useCallback(() => {
    setSession((s) => ({ ...s, phase: 'ratifying', error: null }));
  }, []);

  const closeRatify = useCallback(() => {
    setSession((s) => (s.phase === 'committing' ? s : { ...s, phase: 'active' }));
  }, []);

  const onConfirmRatify = useCallback(
    async (nonce: string) => {
      if (!session.sessionId) return;
      ratifyNonceRef.current = nonce;
      setSession((s) => ({ ...s, phase: 'committing', error: null }));
      try {
        const res = await ratifyNewProject(
          session.sessionId,
          { state: session.state },
          { nonce }
        );
        setSession((s) => ({ ...s, phase: 'done' }));
        // Per design doc (gm-root.17.7): show the "Start planning"
        // handoff screen. The ratify response carries the fields needed
        // to render it (project_name, project_path, milestone_count,
        // epic_count). The handoff screen owns navigation from here.
        setRatifyResult(res);
      } catch (err) {
        setSession((s) => ({
          ...s,
          phase: 'ratifying',
          error: err instanceof Error ? err.message : String(err),
        }));
      }
    },
    [session.sessionId, session.state]
  );

  const inputDisabled =
    session.phase !== 'active' || session.pendingTurn || !session.sessionId;
  const ratifyDisabled =
    session.phase !== 'active' ||
    session.pendingTurn ||
    !session.sessionId ||
    session.state.Milestones.length === 0;

  // After successful ratification, replace the two-pane layout with
  // the "Start planning" handoff screen (gm-root.17.7). The screen
  // owns navigation from here — no further state needed from the
  // conversation session.
  if (ratifyResult) {
    // Count epics across all milestones for the summary line.
    const totalEpics =
      ratifyResult.epic_count ??
      session.state.Milestones.reduce((acc, m) => acc + m.Epics.length, 0);
    return (
      <div
        data-testid="newproject-page"
        data-phase="done"
        className="flex h-full min-h-0 flex-col"
      >
        <RatifyDoneScreen
          projectName={ratifyResult.project_name || session.state.ProjectName}
          projectPath={ratifyResult.project_path}
          milestoneCount={ratifyResult.milestone_count ?? session.state.Milestones.length}
          epicCount={totalEpics}
        />
      </div>
    );
  }

  // Starting / error states render a centered banner so the operator
  // sees what happened before the panes paint.
  if (session.phase === 'starting') {
    return (
      <div
        data-testid="newproject-page"
        data-phase="starting"
        className="flex h-full items-center justify-center"
      >
        <p
          data-testid="newproject-starting"
          className="text-sm italic text-neutral-500 dark:text-neutral-400"
        >
          Starting a new conversation with the Onboarder…
        </p>
      </div>
    );
  }

  if (session.phase === 'error' && !session.sessionId) {
    return (
      <div
        data-testid="newproject-page"
        data-phase="error"
        className="flex h-full items-center justify-center"
      >
        <div className="max-w-md rounded border border-rose-300 bg-rose-50 px-4 py-3 text-sm text-rose-800 dark:border-rose-800 dark:bg-rose-950/40 dark:text-rose-200">
          <p data-testid="newproject-start-error" className="font-semibold">
            Couldn't start a new project conversation.
          </p>
          <p className="mt-1 text-xs">{session.error}</p>
        </div>
      </div>
    );
  }

  return (
    <div
      data-testid="newproject-page"
      data-phase={session.phase}
      className="flex h-full min-h-0 flex-col"
    >
      <header className="flex items-center justify-between border-b border-neutral-200 px-4 py-2 dark:border-neutral-800">
        <div className="flex items-center gap-2">
          <Sparkles className="h-4 w-4 text-sky-600 dark:text-sky-400" />
          <div>
            <h1 className="text-sm font-semibold">New project</h1>
            <p className="text-[10px] text-neutral-500 dark:text-neutral-400">
              One-shot conversation · refresh discards the session.
            </p>
          </div>
        </div>
      </header>

      {session.error && session.sessionId ? (
        <div
          data-testid="newproject-turn-error"
          className="border-b border-rose-300 bg-rose-50 px-4 py-1 text-xs text-rose-800 dark:border-rose-800 dark:bg-rose-950/40 dark:text-rose-200"
        >
          {session.error}
        </div>
      ) : null}

      <div className="flex min-h-0 flex-1">
        <ConversationPane
          transcript={session.transcript}
          onSend={onSend}
          disabled={inputDisabled}
          resetToken={resetToken}
        />
        <PlanPreviewPane
          state={session.state}
          onEdit={onPlanEdit}
          disabled={
            session.phase === 'committing' ||
            session.phase === 'done' ||
            !session.sessionId
          }
        />
      </div>

      <button
        type="button"
        data-testid="newproject-ratify"
        onClick={openRatify}
        disabled={ratifyDisabled}
        className="fixed bottom-6 right-6 z-30 rounded-full bg-emerald-600 px-5 py-2 text-sm font-semibold text-white shadow-lg hover:bg-emerald-500 disabled:cursor-not-allowed disabled:opacity-50"
      >
        Ratify
      </button>

      <RatifyModal
        open={session.phase === 'ratifying' || session.phase === 'committing'}
        state={session.state}
        onConfirm={onConfirmRatify}
        onCancel={closeRatify}
        committing={session.phase === 'committing'}
        error={session.error}
      />
    </div>
  );
}
