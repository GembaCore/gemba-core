// Gemba walk context (gm-uipx.2). Holds active flag + agenda +
// active-item index. Persists active flag and agenda to
// localStorage so a walk survives a page reload (per ui-spec §5.4
// "walks should feel continuous").

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react';
import type { AgendaItem, Decision } from './types';

const STORAGE_ACTIVE = 'gemba.walk.active';
const STORAGE_AGENDA = 'gemba.walk.agenda';

// Seed agenda used on first walk start. Real agenda assembly (from
// open escalations / HITL questions / recently filed beads) is
// downstream wiring; v1 ships a representative seed so the UI
// surface is testable without an LLM-side integration.
const SEED_AGENDA: AgendaItem[] = [
  {
    id: 'walk-seed-1',
    source: 'escalation',
    title: 'Auth middleware tests flaking on CI',
    urgency: 'urgent',
    lane: 'active',
  },
  {
    id: 'walk-seed-2',
    source: 'hitl',
    title: 'Rate-limit policy: per-IP or per-token?',
    urgency: 'today',
    lane: 'queued',
  },
  {
    id: 'walk-seed-3',
    source: 'filed_bead',
    title: 'Backfill targets[] on legacy beads',
    urgency: 'soon',
    lane: 'queued',
  },
];

export interface WalkState {
  active: boolean;
  agenda: AgendaItem[];
  // activeIndex is the index in agenda of the currently-discussed
  // item. -1 when no item is active.
  activeIndex: number;

  // Lifecycle
  start: () => void;
  end: () => void;
  toggle: () => void;

  // Agenda manipulation
  setActiveItem: (id: string) => void;
  decide: (id: string, decision: Decision, note?: string) => void;
  defer: (id: string) => void;
  // promoteNext finds the first lane='queued' item and makes it
  // active. Called after a decision lands so the walk auto-advances.
  promoteNext: () => void;
}

const WalkContext = createContext<WalkState | null>(null);

export interface WalkProviderProps {
  children: ReactNode;
  // Test seams. Production reads from localStorage.
  initialActive?: boolean;
  initialAgenda?: AgendaItem[];
}

export function WalkProvider({
  children,
  initialActive,
  initialAgenda,
}: WalkProviderProps): JSX.Element {
  const [active, setActive] = useState<boolean>(() => {
    if (initialActive !== undefined) return initialActive;
    return readBool(STORAGE_ACTIVE, false);
  });
  const [agenda, setAgenda] = useState<AgendaItem[]>(() => {
    if (initialAgenda !== undefined) return initialAgenda;
    const stored = readJSON<AgendaItem[]>(STORAGE_AGENDA);
    return stored ?? SEED_AGENDA;
  });

  // Persist active + agenda to localStorage on every change so a
  // reload mid-walk lands the operator back where they left off.
  useEffect(() => {
    writeStorage(STORAGE_ACTIVE, JSON.stringify(active));
  }, [active]);
  useEffect(() => {
    writeStorage(STORAGE_AGENDA, JSON.stringify(agenda));
  }, [agenda]);

  const activeIndex = useMemo(
    () => agenda.findIndex((i) => i.lane === 'active'),
    [agenda]
  );

  const start = useCallback(() => setActive(true), []);
  const end = useCallback(() => setActive(false), []);
  const toggle = useCallback(() => setActive((v) => !v), []);

  const setActiveItem = useCallback((id: string) => {
    setAgenda((prev) => {
      // Demote current active to queued (if any), promote target.
      // Idempotent: no-op when the target is already active.
      return prev.map((item) => {
        if (item.id === id) {
          return { ...item, lane: 'active' };
        }
        if (item.lane === 'active') {
          return { ...item, lane: 'queued' };
        }
        return item;
      });
    });
  }, []);

  const decide = useCallback((id: string, decision: Decision, note?: string) => {
    setAgenda((prev) => {
      const next = prev.map((item) => {
        if (item.id !== id) return item;
        if (decision === 'defer') {
          return { ...item, lane: 'deferred' as const, decision: { kind: decision, note } };
        }
        return { ...item, lane: 'decided' as const, decision: { kind: decision, note } };
      });
      // Auto-promote: if the decided item was the active one, find
      // the first queued item and lift it. Decisions feel continuous
      // when the next item snaps into place without an extra click.
      const justDecided = next.find((i) => i.id === id);
      if (justDecided && (justDecided.lane === 'decided' || justDecided.lane === 'deferred')) {
        const stillActive = next.some((i) => i.lane === 'active');
        if (!stillActive) {
          const queuedIdx = next.findIndex((i) => i.lane === 'queued');
          if (queuedIdx >= 0) {
            next[queuedIdx] = { ...next[queuedIdx], lane: 'active' };
          }
        }
      }
      return next;
    });
  }, []);

  const defer = useCallback((id: string) => {
    decide(id, 'defer');
  }, [decide]);

  const promoteNext = useCallback(() => {
    setAgenda((prev) => {
      if (prev.some((i) => i.lane === 'active')) return prev;
      const queuedIdx = prev.findIndex((i) => i.lane === 'queued');
      if (queuedIdx < 0) return prev;
      const out = prev.slice();
      out[queuedIdx] = { ...out[queuedIdx], lane: 'active' };
      return out;
    });
  }, []);

  const value = useMemo<WalkState>(
    () => ({
      active,
      agenda,
      activeIndex,
      start,
      end,
      toggle,
      setActiveItem,
      decide,
      defer,
      promoteNext,
    }),
    [active, agenda, activeIndex, start, end, toggle, setActiveItem, decide, defer, promoteNext]
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

// Aggregates over the agenda — exported as pure helpers so banner
// + agenda pane share a single source of truth for counts.

export function decidedCount(agenda: AgendaItem[]): number {
  let n = 0;
  for (const i of agenda) if (i.lane === 'decided') n++;
  return n;
}

export function totalDecidableCount(agenda: AgendaItem[]): number {
  // Deferred items don't count toward "X of Y decided" because the
  // operator pushed them to a later walk. The denominator is items
  // that still need a verdict THIS walk.
  let n = 0;
  for (const i of agenda) {
    if (i.lane !== 'deferred') n++;
  }
  return n;
}

// ── localStorage helpers (silent on errors) ──────────────────────

function readBool(key: string, fallback: boolean): boolean {
  try {
    const raw = window.localStorage.getItem(key);
    if (raw == null) return fallback;
    return raw === 'true';
  } catch {
    return fallback;
  }
}

function readJSON<T>(key: string): T | null {
  try {
    const raw = window.localStorage.getItem(key);
    if (raw == null) return null;
    return JSON.parse(raw) as T;
  } catch {
    return null;
  }
}

function writeStorage(key: string, value: string): void {
  try {
    window.localStorage.setItem(key, value);
  } catch {
    // Quota / private mode — drop silently.
  }
}
