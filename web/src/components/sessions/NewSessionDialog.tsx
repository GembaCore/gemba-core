// NewSessionDialog (gm-native.15). Operator picks a bead + agent type;
// dialog dispatches StartSession and closes on success. Validation
// front-loaded — submit button is disabled until both fields are set —
// so server-side 400s are rare and the operator gets immediate
// feedback. Agent roster comes from /api/agents (gm-root.10); bead
// list from /api/work-items (gm-peg) filtered to not-yet-in-progress.

import { useEffect, useMemo, useState } from 'react';
import { useAgents } from '@/hooks/useAgents';
import { useWorkItems } from '@/hooks/useWorkItems';
import { useStartSession } from '@/hooks/useSessions';
import type { Session } from '@/api/sessions';
import type { WorkItem } from '@/types/core.gen';

export interface NewSessionDialogProps {
  open: boolean;
  onClose: () => void;
  // prefilledBeadId, when set, locks the bead to that id and hides
  // the searchable picker. Used by the board drawer's Start session
  // button (gm-native.21) so the operator doesn't have to re-search
  // for a bead they already had open.
  prefilledBeadId?: string;
  // onStarted fires after a successful start, before onClose. The
  // board drawer uses it to navigate the operator to /sessions so
  // they can see the new pane immediately (gm-native.21 scope).
  onStarted?: (session: Session) => void;
}

export function NewSessionDialog({ open, onClose, prefilledBeadId, onStarted }: NewSessionDialogProps) {
  const [beadId, setBeadId] = useState(prefilledBeadId ?? '');
  const [agentType, setAgentType] = useState('');
  const [search, setSearch] = useState('');
  const { data: agents = [] } = useAgents();
  const { data: beads = [] } = useWorkItems();
  const start = useStartSession();

  // Reset local state when the dialog opens / closes — operators
  // expect a blank slate on each invocation. When a prefilledBeadId
  // is given, hydrate that on open instead of clearing.
  useEffect(() => {
    if (!open) {
      setBeadId('');
      setAgentType('');
      setSearch('');
      start.reset();
    } else if (prefilledBeadId) {
      setBeadId(prefilledBeadId);
    }
  }, [open, prefilledBeadId, start]);

  const beadOptions = useMemo(() => {
    const q = search.trim().toLowerCase();
    const pool: WorkItem[] = beads.filter((b) => b.state_category !== 'started' && b.state_category !== 'completed' && b.state_category !== 'canceled');
    if (!q) return pool.slice(0, 50);
    return pool.filter((b) => b.id.toLowerCase().includes(q) || b.title.toLowerCase().includes(q)).slice(0, 50);
  }, [beads, search]);

  // Agent-type tokens come from the agent rosters' "dialect" field
  // when set; fall back to a hardcoded "claude" default so the dialog
  // still lets the operator try-something when the orchestrator hasn't
  // surfaced an agent yet.
  const agentTypeOptions = useMemo(() => {
    const tokens = new Set<string>();
    for (const a of agents) {
      if (a.dialect) tokens.add(a.dialect);
    }
    if (tokens.size === 0) tokens.add('claude');
    return Array.from(tokens).sort();
  }, [agents]);

  if (!open) return null;

  const canSubmit = beadId !== '' && agentType !== '' && !start.isPending;

  const submit = () => {
    if (!canSubmit) return;
    start.mutate(
      { bead_id: beadId, agent_type: agentType },
      {
        onSuccess: (session) => {
          onStarted?.(session);
          onClose();
        },
      }
    );
  };

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label="New session"
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40"
      onKeyDown={(e) => {
        if (e.key === 'Escape') onClose();
      }}
    >
      <div
        className="w-full max-w-lg rounded-lg bg-white p-6 shadow-xl dark:bg-neutral-900"
        onClick={(e) => e.stopPropagation()}
      >
        <h2 className="mb-4 text-lg font-semibold">New session</h2>

        {prefilledBeadId ? (
          // Pre-filled mode (board drawer dispatch): show the bead as a
          // read-only chip so the operator sees what they're about to
          // dispatch but can't accidentally swap it out.
          <div className="mb-4">
            <label className="mb-1 block text-xs font-medium text-neutral-600 dark:text-neutral-400">
              Bead
            </label>
            <div className="rounded-md border border-neutral-200 bg-neutral-50 px-3 py-2 font-mono text-xs text-neutral-700 dark:border-neutral-800 dark:bg-neutral-900 dark:text-neutral-300">
              {prefilledBeadId}
            </div>
          </div>
        ) : (
          <>
            <label className="mb-1 block text-xs font-medium text-neutral-600 dark:text-neutral-400">
              Bead
            </label>
            <input
              type="text"
              autoFocus
              placeholder="Search by id or title…"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              className="mb-2 w-full rounded-md border border-neutral-300 px-3 py-1.5 text-sm dark:border-neutral-700 dark:bg-neutral-800"
            />
            <select
              value={beadId}
              onChange={(e) => setBeadId(e.target.value)}
              size={6}
              className="mb-4 w-full rounded-md border border-neutral-300 px-2 py-1 text-sm dark:border-neutral-700 dark:bg-neutral-800"
            >
              {beadOptions.length === 0 && (
                <option value="" disabled>
                  No matching beads
                </option>
              )}
              {beadOptions.map((b) => (
                <option key={b.id} value={b.id}>
                  {b.id} — {b.title}
                </option>
              ))}
            </select>
          </>
        )}

        <label className="mb-1 block text-xs font-medium text-neutral-600 dark:text-neutral-400">
          Agent type
        </label>
        <select
          value={agentType}
          onChange={(e) => setAgentType(e.target.value)}
          className="mb-4 w-full rounded-md border border-neutral-300 px-3 py-1.5 text-sm dark:border-neutral-700 dark:bg-neutral-800"
        >
          <option value="">Select an agent type…</option>
          {agentTypeOptions.map((a) => (
            <option key={a} value={a}>
              {a}
            </option>
          ))}
        </select>

        {start.error && (
          <div className="mb-3 rounded-md bg-red-50 px-3 py-2 text-sm text-red-700 dark:bg-red-950 dark:text-red-300">
            {start.error.message}
          </div>
        )}

        <div className="flex justify-end gap-2">
          <button
            type="button"
            onClick={onClose}
            className="rounded-md px-3 py-1.5 text-sm text-neutral-600 hover:bg-neutral-100 dark:text-neutral-300 dark:hover:bg-neutral-800"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={submit}
            disabled={!canSubmit}
            className="rounded-md bg-neutral-900 px-3 py-1.5 text-sm text-white hover:bg-neutral-800 disabled:opacity-50 dark:bg-neutral-100 dark:text-neutral-900 dark:hover:bg-neutral-200"
          >
            {start.isPending ? 'Starting…' : 'Start session'}
          </button>
        </div>
      </div>
    </div>
  );
}
