// Gemba walk context (gm-i65). Wraps the /api/v1/walks/* surface in a
// React context so AgendaPane / ChatPane / RightPane / BottomBar /
// ActiveWalkBanner share one source of truth for the active walk.
//
// Internally the context stores the *current walk id* in localStorage
// (so a refresh resumes the walk) and uses react-query to fetch the
// Walk by id. SSE walk.* events invalidate the query so the UI
// updates without polling. Mutations (start, decide, addTurn, etc.)
// optimistically update the cache.

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  addAgendaItem as apiAddAgendaItem,
  addTurn as apiAddTurn,
  decideAgendaItem as apiDecide,
  endWalk as apiEnd,
  getWalk as apiGet,
  patchAgendaItemStatus as apiPatchStatus,
  pauseWalk as apiPause,
  resumeWalk as apiResume,
  startWalk as apiStart,
  type AddAgendaItemRequest,
  type AgendaItem,
  type DecisionKind,
  type StartWalkRequest,
  type Walk,
} from '@/api/walks';
import type { Decision } from './types';

const STORAGE_CURRENT_ID = 'gemba.walk.current_id';

export interface WalkState {
  // Raw backend walk; null when nothing is active or fetched.
  walk: Walk | null;
  // Legacy boolean — true when the current walk's status is 'active'.
  active: boolean;
  // Legacy projection: the agenda exposed as the components consume
  // it. Same as walk?.agenda ?? []; the named getter exists so the
  // components don't need a null-safety dance.
  agenda: AgendaItem[];
  // Index in agenda of the currently-discussed item. -1 when no item
  // is active (which is also the case before the first promotion).
  activeIndex: number;
  // Lifecycle state from the most recent fetch / mutation.
  loading: boolean;
  error: Error | null;

  // ── lifecycle ──────────────────────────────────────────────────
  start: (params?: StartWalkRequest) => Promise<Walk>;
  end: () => Promise<void>;
  pause: () => Promise<void>;
  resume: () => Promise<void>;
  // toggle is the legacy lifecycle the active-walk-banner uses: end
  // when active, start when not.
  toggle: () => Promise<void>;
  // setCurrentWalkID drives the context to follow a specific walk
  // (e.g. the read-only /walks/:id detail page). Pass null to clear.
  setCurrentWalkID: (id: string | null) => void;

  // ── agenda manipulation ────────────────────────────────────────
  setActiveItem: (id: string) => Promise<void>;
  decide: (id: string, decision: Decision, note?: string) => Promise<void>;
  defer: (id: string) => Promise<void>;
  // promoteNext finds the first 'queued' item and makes it active.
  // Idempotent — no-op if anything is already active.
  promoteNext: () => Promise<void>;
  addItem: (item: AddAgendaItemRequest) => Promise<void>;

  // ── transcript ─────────────────────────────────────────────────
  sendTurn: (content: string, agendaItemID?: string) => Promise<void>;
}

const WalkContext = createContext<WalkState | null>(null);

export interface WalkProviderProps {
  children: ReactNode;
  // Test seam — when set, the provider seeds with this id rather than
  // reading from localStorage.
  initialWalkID?: string | null;
}

export function WalkProvider({
  children,
  initialWalkID,
}: WalkProviderProps): JSX.Element {
  const qc = useQueryClient();
  const [currentID, setCurrentIDState] = useState<string | null>(() => {
    if (initialWalkID !== undefined) return initialWalkID;
    return readString(STORAGE_CURRENT_ID);
  });

  // Persist current id to localStorage so a refresh resumes the
  // current walk.
  useEffect(() => {
    writeStorage(STORAGE_CURRENT_ID, currentID ?? '');
  }, [currentID]);

  const setCurrentWalkID = useCallback((id: string | null) => {
    setCurrentIDState(id);
  }, []);

  // Fetch the current walk by id. Disabled when no id is set so the
  // browser doesn't hammer 400s on first paint.
  const query = useQuery<Walk | null, Error>({
    queryKey: ['walks', currentID],
    queryFn: async () => (currentID ? apiGet(currentID) : null),
    enabled: currentID !== null,
    staleTime: 5_000,
    refetchOnWindowFocus: true,
  });

  const walk = query.data ?? null;
  const active = walk?.status === 'active';
  // Stabilise the agenda reference so downstream useMemo deps don't
  // change on every render; the underlying array identity from
  // react-query already changes only on data change.
  const agenda = useMemo(() => walk?.agenda ?? [], [walk]);
  const activeIndex = useMemo(
    () => agenda.findIndex((i) => i.status === 'active'),
    [agenda]
  );

  // ── helpers ────────────────────────────────────────────────────

  const refreshWalk = useCallback(
    (next: Walk) => {
      qc.setQueryData(['walks', next.id], next);
    },
    [qc]
  );

  // ── lifecycle mutations ────────────────────────────────────────

  const startMutation = useMutation<Walk, Error, StartWalkRequest>({
    mutationFn: async (params) => apiStart(params),
    onSuccess: (next) => {
      refreshWalk(next);
      setCurrentIDState(next.id);
    },
  });

  const start = useCallback(
    async (params: StartWalkRequest = {}): Promise<Walk> => {
      return startMutation.mutateAsync(params);
    },
    [startMutation]
  );

  const end = useCallback(async () => {
    if (!currentID) return;
    const next = await apiEnd(currentID);
    refreshWalk(next);
    setCurrentIDState(null);
  }, [currentID, refreshWalk]);

  const pause = useCallback(async () => {
    if (!currentID) return;
    const next = await apiPause(currentID);
    refreshWalk(next);
  }, [currentID, refreshWalk]);

  const resume = useCallback(async () => {
    if (!currentID) return;
    const next = await apiResume(currentID);
    refreshWalk(next);
  }, [currentID, refreshWalk]);

  const toggle = useCallback(async () => {
    if (active) {
      await end();
    } else if (!currentID) {
      await start({});
    } else {
      await resume();
    }
  }, [active, currentID, end, resume, start]);

  // ── agenda mutations ───────────────────────────────────────────

  const setActiveItem = useCallback(
    async (itemID: string) => {
      if (!currentID || !walk) return;
      // Demote any currently-active item to queued first so the
      // swimlanes have at most one item in 'active'.
      const wasActive = walk.agenda.find((i) => i.status === 'active');
      let next = walk;
      if (wasActive && wasActive.id !== itemID) {
        next = await apiPatchStatus(currentID, wasActive.id, 'queued');
      }
      next = await apiPatchStatus(currentID, itemID, 'active');
      refreshWalk(next);
    },
    [currentID, walk, refreshWalk]
  );

  const decide = useCallback(
    async (itemID: string, decision: Decision, note?: string) => {
      if (!currentID) return;
      const kind: DecisionKind = decision; // 1-1 alignment for the four legacy values.
      const next = await apiDecide(currentID, itemID, {
        kind,
        rationale: note,
      });
      // Auto-promote next queued item — feel-of-the-walk continuity.
      const nextActive = next.agenda.find((i) => i.status === 'active');
      if (!nextActive) {
        const queued = next.agenda.find((i) => i.status === 'queued');
        if (queued) {
          const promoted = await apiPatchStatus(currentID, queued.id, 'active');
          refreshWalk(promoted);
          return;
        }
      }
      refreshWalk(next);
    },
    [currentID, refreshWalk]
  );

  const defer = useCallback(
    async (itemID: string) => {
      await decide(itemID, 'defer');
    },
    [decide]
  );

  const promoteNext = useCallback(async () => {
    if (!currentID || !walk) return;
    if (walk.agenda.some((i) => i.status === 'active')) return;
    const queued = walk.agenda.find((i) => i.status === 'queued');
    if (!queued) return;
    const next = await apiPatchStatus(currentID, queued.id, 'active');
    refreshWalk(next);
  }, [currentID, walk, refreshWalk]);

  const addItem = useCallback(
    async (item: AddAgendaItemRequest) => {
      if (!currentID) return;
      const next = await apiAddAgendaItem(currentID, item);
      refreshWalk(next);
    },
    [currentID, refreshWalk]
  );

  // ── transcript ─────────────────────────────────────────────────

  const sendTurn = useCallback(
    async (content: string, agendaItemID?: string) => {
      if (!currentID) return;
      const next = await apiAddTurn(currentID, {
        content,
        agenda_item_id: agendaItemID,
      });
      refreshWalk(next);
    },
    [currentID, refreshWalk]
  );

  const value = useMemo<WalkState>(
    () => ({
      walk,
      active,
      agenda,
      activeIndex,
      loading: query.isLoading || startMutation.isPending,
      error: query.error ?? startMutation.error ?? null,
      start,
      end,
      pause,
      resume,
      toggle,
      setCurrentWalkID,
      setActiveItem,
      decide,
      defer,
      promoteNext,
      addItem,
      sendTurn,
    }),
    [
      walk,
      active,
      agenda,
      activeIndex,
      query.isLoading,
      query.error,
      startMutation.isPending,
      startMutation.error,
      start,
      end,
      pause,
      resume,
      toggle,
      setCurrentWalkID,
      setActiveItem,
      decide,
      defer,
      promoteNext,
      addItem,
      sendTurn,
    ]
  );

  return <WalkContext.Provider value={value}>{children}</WalkContext.Provider>;
}

export function useWalk(): WalkState {
  const ctx = useContext(WalkContext);
  if (!ctx) {
    throw new Error('useWalk: no WalkProvider in tree');
  }
  return ctx;
}

// ── pure helpers ───────────────────────────────────────────────────

// decidedCount is the count of agenda items in 'decided' status.
export function decidedCount(agenda: AgendaItem[]): number {
  let n = 0;
  for (const i of agenda) if (i.status === 'decided') n++;
  return n;
}

// totalDecidableCount is the denominator of "X of Y decided" — items
// still needing a verdict THIS walk. Deferred items are parked for a
// later walk and don't count.
export function totalDecidableCount(agenda: AgendaItem[]): number {
  let n = 0;
  for (const i of agenda) {
    if (i.status !== 'deferred' && i.status !== 'dismissed') n++;
  }
  return n;
}

// ── localStorage helpers (silent on errors) ──────────────────────

function readString(key: string): string | null {
  try {
    const raw = window.localStorage.getItem(key);
    return raw && raw.length > 0 ? raw : null;
  } catch {
    return null;
  }
}

function writeStorage(key: string, value: string): void {
  try {
    if (value) window.localStorage.setItem(key, value);
    else window.localStorage.removeItem(key);
  } catch {
    // Quota / private mode — drop silently.
  }
}
