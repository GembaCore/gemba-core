// NewSessionDialog (gm-native.15 + gm-hmqj). Two modes:
//
//   - Bead mode (default): pick a bead + agent type. Original flow.
//   - Manual mode (gm-hmqj): no bead. Pick persona + agent + repo +
//     write a prompt. Lets operators start sessions ad-hoc for
//     exploration / debugging without front-loading bead authoring.
//
// Auto-defaults: when a roster has exactly one option (one persona,
// one agent type, one repository) the form pre-selects it so the
// 80% case is a single click + a prompt.

import { useEffect, useMemo, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { listRepositories } from '@/api/repositories';
import { useAgents } from '@/hooks/useAgents';
import { useWorkItems } from '@/hooks/useWorkItems';
import { useStartSession } from '@/hooks/useSessions';
import {
  DEFAULT_PERSONAS,
} from '@/components/pm/PmPanelContext';
import type { Session } from '@/api/sessions';
import type { Persona } from '@/components/pm/types';
import type { WorkItem } from '@/types/core.gen';
import { cn } from '@/lib/utils';

export interface NewSessionDialogProps {
  open: boolean;
  onClose: () => void;
  // prefilledBeadId, when set, locks the dialog to bead mode and locks
  // the bead to that id. Used by the board drawer's Start session
  // button so the operator doesn't have to re-search for a bead they
  // already had open.
  prefilledBeadId?: string;
  // onStarted fires after a successful start, before onClose. The
  // board drawer uses it to navigate the operator to /sessions.
  onStarted?: (session: Session) => void;
}

type Mode = 'bead' | 'manual';

export function NewSessionDialog({ open, onClose, prefilledBeadId, onStarted }: NewSessionDialogProps) {
  const wasOpenRef = useRef(false);
  const [mode, setMode] = useState<Mode>('bead');

  // Bead-mode local state.
  const [beadId, setBeadId] = useState(prefilledBeadId ?? '');
  const [search, setSearch] = useState('');

  // Shared: agent type. Manual: also persona, repository, prompt.
  const [agentType, setAgentType] = useState('');
  const [personaId, setPersonaId] = useState('');
  const [repositoryId, setRepositoryId] = useState('');
  const [prompt, setPrompt] = useState('');

  const { data: agents = [] } = useAgents();
  const { data: beads = [] } = useWorkItems();
  const reposQuery = useQuery({
    queryKey: ['repositories'],
    queryFn: listRepositories,
    enabled: open && mode === 'manual',
  });
  // Memoized so the autodefault useEffect's dep array stays stable
  // across renders that don't actually change the roster.
  const repos = useMemo(
    () => reposQuery.data?.repositories ?? [],
    [reposQuery.data]
  );
  // v1: persona roster comes from the same DEFAULT_PERSONAS the PM
  // panel uses. A future bead lifts it onto /api/personas; the
  // launcher will follow without changing call sites.
  const personas: Persona[] = DEFAULT_PERSONAS;

  const start = useStartSession();
  const resetStart = start.reset;

  // Only mutate local/query state when the dialog actually transitions
  // open/closed. When mounted closed, repeating reset() on every render
  // can self-trigger a render loop through React Query mutation state.
  useEffect(() => {
    const wasOpen = wasOpenRef.current;
    if (!open) {
      if (wasOpen) {
        setBeadId('');
        setSearch('');
        setAgentType('');
        setPersonaId('');
        setRepositoryId('');
        setPrompt('');
        setMode('bead');
        resetStart();
      }
      wasOpenRef.current = false;
      return;
    }

    wasOpenRef.current = true;
    if (prefilledBeadId) {
      setBeadId(prefilledBeadId);
      setMode('bead');
    }
  }, [open, prefilledBeadId, resetStart]);

  const beadOptions = useMemo(() => {
    const q = search.trim().toLowerCase();
    const pool: WorkItem[] = beads.filter(
      (b) =>
        b.state_category !== 'started' &&
        b.state_category !== 'completed' &&
        b.state_category !== 'canceled'
    );
    if (!q) return pool.slice(0, 50);
    return pool
      .filter((b) => b.id.toLowerCase().includes(q) || b.title.toLowerCase().includes(q))
      .slice(0, 50);
  }, [beads, search]);

  const agentTypeOptions = useMemo(() => {
    const tokens = new Set<string>();
    for (const a of agents) {
      if (a.dialect) tokens.add(a.dialect);
    }
    if (tokens.size === 0) tokens.add('claude');
    return Array.from(tokens).sort();
  }, [agents]);

  // Auto-default the agent type when exactly one is on offer. The
  // effect runs whenever the roster changes (incl. async load) and
  // is a no-op once the operator has picked one.
  useEffect(() => {
    if (!open) return;
    if (agentType === '' && agentTypeOptions.length === 1) {
      setAgentType(agentTypeOptions[0]);
    }
  }, [open, agentType, agentTypeOptions]);

  // Auto-default the repository when exactly one is registered.
  useEffect(() => {
    if (!open || mode !== 'manual') return;
    if (repositoryId === '' && repos.length === 1) {
      setRepositoryId(repos[0].id);
    }
  }, [open, mode, repositoryId, repos]);

  // Persona auto-default: a workspace with a single enabled persona
  // pre-selects it; otherwise we keep "(none)" as the safe blank.
  useEffect(() => {
    if (!open || mode !== 'manual') return;
    if (personaId === '' && personas.length === 1) {
      setPersonaId(personas[0].id);
    }
  }, [open, mode, personaId, personas]);

  if (!open) return null;

  const beadCanSubmit =
    mode === 'bead' && beadId !== '' && agentType !== '' && !start.isPending;
  const manualCanSubmit =
    mode === 'manual' &&
    agentType !== '' &&
    repositoryId !== '' &&
    prompt.trim() !== '' &&
    !start.isPending;
  const canSubmit = beadCanSubmit || manualCanSubmit;

  const submit = () => {
    if (!canSubmit) return;
    if (mode === 'bead') {
      start.mutate(
        { kind: 'bead', bead_id: beadId, agent_type: agentType },
        {
          onSuccess: (session) => {
            onStarted?.(session);
            onClose();
          },
        }
      );
      return;
    }
    start.mutate(
      {
        kind: 'manual',
        agent_type: agentType,
        repository_id: repositoryId,
        prompt: prompt.trim(),
        ...(personaId ? { persona_id: personaId } : {}),
      },
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
        data-testid="new-session-dialog"
      >
        <h2 className="mb-4 text-lg font-semibold">New session</h2>

        {!prefilledBeadId && <ModeTabs mode={mode} onSelect={setMode} />}

        {mode === 'bead' && prefilledBeadId ? (
          <ReadOnlyBead beadId={prefilledBeadId} />
        ) : mode === 'bead' ? (
          <BeadPicker
            beadId={beadId}
            search={search}
            onChangeSearch={setSearch}
            onSelectBead={setBeadId}
            options={beadOptions}
          />
        ) : (
          <ManualFields
            personaId={personaId}
            personas={personas}
            onSelectPersona={setPersonaId}
            repositoryId={repositoryId}
            repos={repos.map((r) => ({ id: r.id, label: r.id }))}
            reposLoading={reposQuery.isLoading}
            reposNotice={reposQuery.data?.notice}
            onSelectRepository={setRepositoryId}
            prompt={prompt}
            onChangePrompt={setPrompt}
          />
        )}

        <label className="mb-1 block text-xs font-medium text-neutral-600 dark:text-neutral-400">
          Agent type
        </label>
        <select
          value={agentType}
          onChange={(e) => setAgentType(e.target.value)}
          data-testid="new-session-agent-type"
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
          <div
            data-testid="new-session-error"
            className="mb-3 rounded-md bg-red-50 px-3 py-2 text-sm text-red-700 dark:bg-red-950 dark:text-red-300"
          >
            {start.error.message}
          </div>
        )}

        <div className="flex justify-end gap-2">
          <button
            type="button"
            onClick={onClose}
            data-testid="new-session-cancel"
            className="rounded-md px-3 py-1.5 text-sm text-neutral-600 hover:bg-neutral-100 dark:text-neutral-300 dark:hover:bg-neutral-800"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={submit}
            disabled={!canSubmit}
            data-testid="new-session-submit"
            className="rounded-md bg-neutral-900 px-3 py-1.5 text-sm text-white hover:bg-neutral-800 disabled:opacity-50 dark:bg-neutral-100 dark:text-neutral-900 dark:hover:bg-neutral-200"
          >
            {start.isPending ? 'Starting…' : 'Start session'}
          </button>
        </div>
      </div>
    </div>
  );
}

function ModeTabs({ mode, onSelect }: { mode: Mode; onSelect: (m: Mode) => void }): JSX.Element {
  return (
    <div
      role="tablist"
      aria-label="Session start mode"
      data-testid="new-session-mode-tabs"
      className="mb-4 flex gap-1 rounded-md border border-neutral-200 p-0.5 text-xs dark:border-neutral-800"
    >
      <TabBtn active={mode === 'bead'} onClick={() => onSelect('bead')} testid="new-session-mode-bead">
        From bead
      </TabBtn>
      <TabBtn
        active={mode === 'manual'}
        onClick={() => onSelect('manual')}
        testid="new-session-mode-manual"
      >
        Manual
      </TabBtn>
    </div>
  );
}

function TabBtn({
  active,
  onClick,
  testid,
  children,
}: {
  active: boolean;
  onClick: () => void;
  testid: string;
  children: React.ReactNode;
}): JSX.Element {
  return (
    <button
      type="button"
      role="tab"
      aria-selected={active}
      data-testid={testid}
      data-active={active ? 'true' : 'false'}
      onClick={onClick}
      className={cn(
        'flex-1 rounded px-2 py-1.5 transition-colors',
        active
          ? 'bg-neutral-900 text-white dark:bg-neutral-100 dark:text-neutral-900'
          : 'text-neutral-600 hover:bg-neutral-100 dark:text-neutral-300 dark:hover:bg-neutral-800'
      )}
    >
      {children}
    </button>
  );
}

function ReadOnlyBead({ beadId }: { beadId: string }): JSX.Element {
  return (
    <div className="mb-4">
      <label className="mb-1 block text-xs font-medium text-neutral-600 dark:text-neutral-400">
        Bead
      </label>
      <div className="rounded-md border border-neutral-200 bg-neutral-50 px-3 py-2 font-mono text-xs text-neutral-700 dark:border-neutral-800 dark:bg-neutral-900 dark:text-neutral-300">
        {beadId}
      </div>
    </div>
  );
}

function BeadPicker({
  beadId,
  search,
  onChangeSearch,
  onSelectBead,
  options,
}: {
  beadId: string;
  search: string;
  onChangeSearch: (s: string) => void;
  onSelectBead: (id: string) => void;
  options: WorkItem[];
}): JSX.Element {
  return (
    <>
      <label className="mb-1 block text-xs font-medium text-neutral-600 dark:text-neutral-400">
        Bead
      </label>
      <input
        type="text"
        autoFocus
        placeholder="Search by id or title…"
        value={search}
        onChange={(e) => onChangeSearch(e.target.value)}
        className="mb-2 w-full rounded-md border border-neutral-300 px-3 py-1.5 text-sm dark:border-neutral-700 dark:bg-neutral-800"
      />
      <select
        value={beadId}
        onChange={(e) => onSelectBead(e.target.value)}
        size={6}
        data-testid="new-session-bead"
        className="mb-4 w-full rounded-md border border-neutral-300 px-2 py-1 text-sm dark:border-neutral-700 dark:bg-neutral-800"
      >
        {options.length === 0 && (
          <option value="" disabled>
            No matching beads
          </option>
        )}
        {options.map((b) => (
          <option key={b.id} value={b.id}>
            {b.id} — {b.title}
          </option>
        ))}
      </select>
    </>
  );
}

function ManualFields({
  personaId,
  personas,
  onSelectPersona,
  repositoryId,
  repos,
  reposLoading,
  reposNotice,
  onSelectRepository,
  prompt,
  onChangePrompt,
}: {
  personaId: string;
  personas: Persona[];
  onSelectPersona: (id: string) => void;
  repositoryId: string;
  repos: { id: string; label: string }[];
  reposLoading: boolean;
  reposNotice?: string;
  onSelectRepository: (id: string) => void;
  prompt: string;
  onChangePrompt: (s: string) => void;
}): JSX.Element {
  return (
    <>
      <label className="mb-1 block text-xs font-medium text-neutral-600 dark:text-neutral-400">
        Persona <span className="text-neutral-400">(optional ride-along)</span>
      </label>
      <select
        value={personaId}
        onChange={(e) => onSelectPersona(e.target.value)}
        data-testid="new-session-persona"
        className="mb-4 w-full rounded-md border border-neutral-300 px-3 py-1.5 text-sm dark:border-neutral-700 dark:bg-neutral-800"
      >
        <option value="">(none)</option>
        {personas.map((p) => (
          <option key={p.id} value={p.id}>
            {p.name} · {p.kind}
          </option>
        ))}
      </select>

      <label className="mb-1 block text-xs font-medium text-neutral-600 dark:text-neutral-400">
        Working directory (repository)
      </label>
      {reposLoading ? (
        <div
          data-testid="new-session-repos-loading"
          className="mb-4 rounded-md border border-neutral-200 bg-neutral-50 px-3 py-2 text-xs italic text-neutral-500 dark:border-neutral-800 dark:bg-neutral-900"
        >
          Loading repositories…
        </div>
      ) : repos.length === 0 ? (
        <div
          data-testid="new-session-repos-empty"
          className="mb-4 rounded-md border border-amber-300 bg-amber-50 px-3 py-2 text-xs text-amber-800 dark:border-amber-800 dark:bg-amber-950/40 dark:text-amber-200"
        >
          {reposNotice ?? 'No repositories registered for this workspace.'}
        </div>
      ) : (
        <select
          value={repositoryId}
          onChange={(e) => onSelectRepository(e.target.value)}
          data-testid="new-session-repository"
          className="mb-4 w-full rounded-md border border-neutral-300 px-3 py-1.5 text-sm dark:border-neutral-700 dark:bg-neutral-800"
        >
          <option value="">Select a repository…</option>
          {repos.map((r) => (
            <option key={r.id} value={r.id}>
              {r.label}
            </option>
          ))}
        </select>
      )}

      <label className="mb-1 block text-xs font-medium text-neutral-600 dark:text-neutral-400">
        What do you want to do?
      </label>
      <textarea
        value={prompt}
        onChange={(e) => onChangePrompt(e.target.value)}
        rows={3}
        placeholder="e.g. Explore the rate-limit code path and find where the per-IP map decays."
        data-testid="new-session-prompt"
        className="mb-4 w-full resize-none rounded-md border border-neutral-300 px-3 py-2 text-sm dark:border-neutral-700 dark:bg-neutral-800"
      />
    </>
  );
}
