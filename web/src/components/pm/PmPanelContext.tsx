// PM panel context (gm-uipx.12). Holds the drawer's open/closed
// state, the active persona, the conversation turns, and the
// gemba-walk-active flag — the four pieces of state the panel UI
// reads. Persistence (open-last-state across reloads, height,
// active persona) lives in localStorage; conversation state is
// memory-only for v1 because the LLM hookup is a separate bead.

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react';
import type { ExecutedAction, Persona, SuggestedAction, Turn } from './types';

// DEFAULT_PERSONAS is the v1 hardcoded roster. A future bead will
// fetch personas from /api/personas (gm-uipx parent epic) and let
// the dropdown populate dynamically; the shape here mirrors what
// the eventual server response will carry so the swap is local.
export const DEFAULT_PERSONAS: Persona[] = [
  { id: 'coach', name: 'Coach', kind: 'coach' },
  { id: 'manager', name: 'Manager', kind: 'manager' },
  { id: 'architect', name: 'Architect', kind: 'architect' },
];

const DEFAULT_HEIGHT_PX = 320;
const MIN_HEIGHT_FRAC = 0.25; // 25% of viewport
const MAX_HEIGHT_FRAC = 0.75; // 75%

// Storage keys. Namespaced under `gemba.pm.` so a future SPA-wide
// reset utility can drop everything in one prefix scan.
const STORAGE_OPEN = 'gemba.pm.open';
const STORAGE_HEIGHT = 'gemba.pm.height';
const STORAGE_PERSONA = 'gemba.pm.persona';

export interface PmPanelState {
  // Open state. Open-last-state persisted to localStorage per
  // ui-spec §2.4 ("default state: open-last-state").
  open: boolean;
  // Expanded height in px. Stored absolute; the renderer clamps to
  // [25%, 75%] of viewport on read so a window-resize doesn't leave
  // an unreachable handle.
  heightPx: number;
  // Active persona id — looked up against personas. Persona switch
  // does NOT clear the conversation per ui-spec §2.4.
  personaId: string;
  personas: Persona[];
  // Conversation turns. Memory-only for v1; cleared on reload.
  turns: Turn[];
  // Gemba walk active flag. When true, the panel renders the
  // walk's chat surface instead of the regular conversation
  // (ui-spec §2.4 / §5.4). The walk surface itself is gm-uipx.2.
  walkActive: boolean;

  // Actions
  toggle: () => void;
  setOpen: (next: boolean) => void;
  setHeightPx: (next: number) => void;
  setPersonaId: (id: string) => void;
  appendTurn: (turn: Turn) => void;
  applySuggested: (turnID: string, suggested: SuggestedAction) => void;
  dismissSuggested: (turnID: string, suggestedID: string) => void;
  setWalkActive: (next: boolean) => void;
}

const PmPanelContext = createContext<PmPanelState | null>(null);

export interface PmPanelProviderProps {
  children: ReactNode;
  // initialOpen / initialHeightPx / initialTurns are test seams —
  // production reads from localStorage. Tests pass these to mount
  // the panel in a known state without poking at storage.
  initialOpen?: boolean;
  initialHeightPx?: number;
  initialPersonaId?: string;
  initialTurns?: Turn[];
  initialWalkActive?: boolean;
  // personas is overridable so a future server-fed roster can
  // replace DEFAULT_PERSONAS without touching call sites.
  personas?: Persona[];
}

export function PmPanelProvider({
  children,
  initialOpen,
  initialHeightPx,
  initialPersonaId,
  initialTurns,
  initialWalkActive,
  personas = DEFAULT_PERSONAS,
}: PmPanelProviderProps): JSX.Element {
  const [open, setOpenState] = useState<boolean>(() => {
    if (initialOpen !== undefined) return initialOpen;
    return readStorageBool(STORAGE_OPEN, false);
  });
  const [heightPx, setHeightPxState] = useState<number>(() => {
    if (initialHeightPx !== undefined) return initialHeightPx;
    return readStorageNum(STORAGE_HEIGHT, DEFAULT_HEIGHT_PX);
  });
  const [personaId, setPersonaIdState] = useState<string>(() => {
    if (initialPersonaId !== undefined) return initialPersonaId;
    const stored = readStorageString(STORAGE_PERSONA, '');
    if (stored && personas.some((p) => p.id === stored)) return stored;
    return personas[0]?.id ?? '';
  });
  const [turns, setTurns] = useState<Turn[]>(() => initialTurns ?? []);
  const [walkActive, setWalkActiveState] = useState<boolean>(initialWalkActive ?? false);

  // Persist open / height / persona changes back to localStorage on
  // every transition. Each is its own effect so a flake in one
  // doesn't block the others.
  useEffect(() => {
    writeStorage(STORAGE_OPEN, JSON.stringify(open));
  }, [open]);
  useEffect(() => {
    writeStorage(STORAGE_HEIGHT, String(heightPx));
  }, [heightPx]);
  useEffect(() => {
    writeStorage(STORAGE_PERSONA, personaId);
  }, [personaId]);

  const setOpen = useCallback((next: boolean) => setOpenState(next), []);
  const toggle = useCallback(() => setOpenState((v) => !v), []);
  const setHeightPx = useCallback((next: number) => {
    // Clamp on write so persisted state can never out-of-bounds the
    // renderer. Math is "vh-relative" because operators size the
    // panel against their viewport, not against pixel intuition.
    if (typeof window === 'undefined') {
      setHeightPxState(next);
      return;
    }
    const vh = window.innerHeight;
    const minPx = Math.round(vh * MIN_HEIGHT_FRAC);
    const maxPx = Math.round(vh * MAX_HEIGHT_FRAC);
    if (next < minPx) next = minPx;
    if (next > maxPx) next = maxPx;
    setHeightPxState(next);
  }, []);
  const setPersonaId = useCallback((id: string) => setPersonaIdState(id), []);
  const setWalkActive = useCallback((next: boolean) => setWalkActiveState(next), []);
  const appendTurn = useCallback((turn: Turn) => {
    setTurns((prev) => [...prev, turn]);
  }, []);

  const applySuggested = useCallback(
    (turnID: string, suggested: SuggestedAction) => {
      const executed: ExecutedAction = {
        id: suggested.id,
        label: suggested.label,
        appliedAt: new Date().toISOString(),
      };
      setTurns((prev) =>
        prev.map((t) => {
          if (t.id !== turnID) return t;
          return {
            ...t,
            suggested: (t.suggested ?? []).filter((s) => s.id !== suggested.id),
            executed: [...(t.executed ?? []), executed],
          };
        })
      );
    },
    []
  );

  const dismissSuggested = useCallback((turnID: string, suggestedID: string) => {
    setTurns((prev) =>
      prev.map((t) => {
        if (t.id !== turnID) return t;
        return {
          ...t,
          suggested: (t.suggested ?? []).filter((s) => s.id !== suggestedID),
        };
      })
    );
  }, []);

  const value = useMemo<PmPanelState>(
    () => ({
      open,
      heightPx,
      personaId,
      personas,
      turns,
      walkActive,
      toggle,
      setOpen,
      setHeightPx,
      setPersonaId,
      appendTurn,
      applySuggested,
      dismissSuggested,
      setWalkActive,
    }),
    [
      open,
      heightPx,
      personaId,
      personas,
      turns,
      walkActive,
      toggle,
      setOpen,
      setHeightPx,
      setPersonaId,
      appendTurn,
      applySuggested,
      dismissSuggested,
      setWalkActive,
    ]
  );

  return <PmPanelContext.Provider value={value}>{children}</PmPanelContext.Provider>;
}

export function usePmPanel(): PmPanelState {
  const ctx = useContext(PmPanelContext);
  if (!ctx) {
    throw new Error('usePmPanel: no PmPanelProvider in tree');
  }
  return ctx;
}

// Cost is computed lazily so the header's running total updates
// without re-reading every Turn on each render. Pure function — no
// state inside.
export function totalCost(turns: Turn[]): number {
  let sum = 0;
  for (const t of turns) sum += t.costUSD ?? 0;
  return sum;
}

// ── localStorage helpers (silent on errors — quota / private mode) ──

function readStorageBool(key: string, fallback: boolean): boolean {
  try {
    const raw = window.localStorage.getItem(key);
    if (raw == null) return fallback;
    return raw === 'true';
  } catch {
    return fallback;
  }
}

function readStorageNum(key: string, fallback: number): number {
  try {
    const raw = window.localStorage.getItem(key);
    if (raw == null) return fallback;
    const n = Number(raw);
    return Number.isFinite(n) ? n : fallback;
  } catch {
    return fallback;
  }
}

function readStorageString(key: string, fallback: string): string {
  try {
    return window.localStorage.getItem(key) ?? fallback;
  } catch {
    return fallback;
  }
}

function writeStorage(key: string, value: string): void {
  try {
    window.localStorage.setItem(key, value);
  } catch {
    // Quota exceeded / private mode — skip. The next read will see
    // the previous value or the default; no operator-visible effect.
  }
}
