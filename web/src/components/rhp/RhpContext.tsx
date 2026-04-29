// RhpContext — Right-Hand Panel provider + hook.
//
// Co-owned by gm-root.22.2 (provider scaffolding, layout state,
// pinned-tab registration) and gm-root.22.4 (detail-tab semantics,
// URL codec, per-route scoping, detail-content registry hookup).
//
// Per docs/design/rhp.md, the RHP is a persistent right-side surface
// hosting a vertical tab rail and unified detail surfaces.
//
// Detail-tab system (gm-root.22.4):
//
// - Detail tabs are URL-as-source-of-truth, encoded as
//   `?rhp=<kind>:<id>(,<kind>:<id>)*`. Order = stacking order; rightmost
//   = focused on first paint. Parser splits each segment on the FIRST
//   ':' so workspace-prefixed ids ("gemba/gemba/gm-1") survive.
// - `popDetail({kind, id})`: same-kind-replaces, different-kind-stacks.
// - `closeDetail(kind, id?)`: closes the matching detail tab; closing
//   the last refocuses the first pinned tab (Help in v1).
// - Per-current-route scoping: a `useLocation().pathname` effect clears
//   all detail tabs and the `?rhp` param on route change.
// - Malformed `?rhp` segments are dropped silently and the URL is
//   rewritten to canonical via setSearchParams(..., {replace: true}).
//
// Detail-content registry: kind owners (gm-root.22.5/.6/.7) call
// `registerDetailContent({kind, render, icon, label})` once on mount
// (see RhpDetail.tsx). The registry lives in this provider so
// `popDetail` can synthesize a tab with the right icon + label without
// the caller passing them through. Unregistered kinds render a
// placeholder body and use a fallback icon registered by RhpDetail on
// provider mount.

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react';
import { useLocation, useSearchParams } from 'react-router-dom';
import type { LucideIcon } from 'lucide-react';

export interface RhpTab {
  /** Unique within the panel: `'help'`, `'workitem:gm-1'`, etc. */
  id: string;
  /** `'help'` for pinned; otherwise the detail kind (`'workitem'`, `'epic'`, …). */
  kind: string;
  pinned: boolean;
  icon: LucideIcon;
  /** Shown as title-bar of the tab body and tooltip on the rail. */
  label: string;
}

export interface RhpDetailRequest {
  kind: string;
  id: string;
}

export interface RhpAPI {
  // State
  tabs: RhpTab[];
  activeTabId: string | null;

  // Tab management
  focusTab(id: string): void;
  /** No-op on pinned tabs. */
  closeTab(id: string): void;
  popDetail(req: RhpDetailRequest): void;
  closeDetail(kind: string, id?: string): void;

  // Pinned-tab registration. Returns an unregister function the caller
  // invokes on unmount.
  registerPinnedTab(tab: { id: string; icon: LucideIcon; label: string }): () => void;

  // Layout state
  collapsed: boolean;
  setCollapsed(next: boolean): void;
  width: number;
  setWidth(next: number): void;
}

const RhpContext = createContext<RhpAPI | null>(null);

// ── Detail-content registry (gm-root.22.4) ────────────────────────────
//
// Kind owners (drawer migrations) register a content component, an
// icon, and a label per kind. The registry is exposed via a sibling
// context so RhpDetail.tsx can register kinds and the shell can
// resolve them at render time. Stored as a ref-backed Map keyed by
// kind; a version counter triggers re-render of consumers when the
// registry changes.

export interface RhpDetailContentRegistration {
  kind: string;
  render: (id: string) => ReactNode;
  icon: LucideIcon;
  label: string;
}

export interface RhpDetailRegistryAPI {
  register(reg: RhpDetailContentRegistration): () => void;
  resolve(kind: string): RhpDetailContentRegistration | undefined;
}

const RhpDetailRegistryContext = createContext<RhpDetailRegistryAPI | null>(null);

export function useRhpDetailRegistry(): RhpDetailRegistryAPI {
  const ctx = useContext(RhpDetailRegistryContext);
  if (!ctx) throw new Error('useRhpDetailRegistry: no RhpProvider in tree');
  return ctx;
}

/**
 * Sentinel kind for the fallback icon. RhpDetail.tsx registers this on
 * mount so the rail always has a valid icon for kinds that haven't
 * been registered yet (placeholder body explains the situation).
 */
export const RHP_FALLBACK_KIND = '__rhp_fallback__';

// Layout constants — kept here (not in the shell file) because the
// provider clamps width on setWidth() so callers (e.g. the drag handle)
// don't have to re-implement the clamp logic.
export const RHP_WIDTH_DEFAULT = 384;
export const RHP_WIDTH_MIN = 320;
export const RHP_WIDTH_MAX = 800;

export const RHP_WIDTH_STORAGE_KEY = 'gemba.rhp.width';
export const RHP_COLLAPSED_STORAGE_KEY = 'gemba.rhp.collapsed';

/** URL param name for the detail-tab stack. */
export const RHP_URL_PARAM = 'rhp';

function clampWidth(n: number): number {
  if (!Number.isFinite(n)) return RHP_WIDTH_DEFAULT;
  if (n < RHP_WIDTH_MIN) return RHP_WIDTH_MIN;
  if (n > RHP_WIDTH_MAX) return RHP_WIDTH_MAX;
  return Math.round(n);
}

function readStoredWidth(): number {
  try {
    const raw = window.localStorage.getItem(RHP_WIDTH_STORAGE_KEY);
    if (raw == null) return RHP_WIDTH_DEFAULT;
    const parsed = Number(raw);
    return clampWidth(parsed);
  } catch {
    return RHP_WIDTH_DEFAULT;
  }
}

function readStoredCollapsed(): boolean | null {
  try {
    const raw = window.localStorage.getItem(RHP_COLLAPSED_STORAGE_KEY);
    if (raw == null) return null;
    return raw === 'true';
  } catch {
    return null;
  }
}

interface PinnedRegistration {
  id: string;
  icon: LucideIcon;
  label: string;
}

/** Parsed URL detail entry (kind + id, no UI metadata). */
export interface DetailUrlEntry {
  kind: string;
  id: string;
}

/**
 * Parse the `?rhp=<kind>:<id>(,<kind>:<id>)*` query param.
 *
 * Splits each segment on the FIRST `:` so workspace-prefixed ids
 * (`gemba/gemba/gm-1`) survive. Drops malformed segments silently.
 * Empty / missing param parses to an empty array.
 */
export function parseRhpParam(raw: string | null): DetailUrlEntry[] {
  if (!raw) return [];
  const out: DetailUrlEntry[] = [];
  for (const segment of raw.split(',')) {
    if (!segment) continue;
    const colon = segment.indexOf(':');
    if (colon <= 0 || colon === segment.length - 1) continue;
    const kind = segment.slice(0, colon).trim();
    const id = segment.slice(colon + 1).trim();
    if (!kind || !id) continue;
    out.push({ kind, id });
  }
  return out;
}

/** Inverse of parseRhpParam — produces the canonical encoded form. */
export function encodeRhpParam(entries: DetailUrlEntry[]): string {
  return entries.map((e) => `${e.kind}:${e.id}`).join(',');
}

export function RhpProvider({ children }: { children: ReactNode }) {
  const [searchParams, setSearchParams] = useSearchParams();
  const location = useLocation();

  // Pinned-tab registrations. The Help bead (`.3`) calls
  // `registerPinnedTab` on mount; we keep them in an array so
  // registration order = render order (per design doc).
  const [pinned, setPinned] = useState<PinnedRegistration[]>([]);

  // Detail-content registry. Map kept in a ref + version counter so
  // tab rendering re-evaluates when registrations change without
  // re-rendering on every parent state change.
  const detailRegRef = useRef<Map<string, RhpDetailContentRegistration>>(new Map());
  const [detailRegVersion, setDetailRegVersion] = useState(0);
  const bumpDetailReg = useCallback(() => {
    setDetailRegVersion((n) => n + 1);
  }, []);

  const detailRegistry = useMemo<RhpDetailRegistryAPI>(
    () => ({
      register(reg) {
        detailRegRef.current.set(reg.kind, reg);
        bumpDetailReg();
        return () => {
          // Only delete if the registration we own is still the active
          // one — guards fast unmount/remount races where a newer
          // registration has taken over.
          const cur = detailRegRef.current.get(reg.kind);
          if (cur === reg) {
            detailRegRef.current.delete(reg.kind);
            bumpDetailReg();
          }
        };
      },
      resolve(kind) {
        return detailRegRef.current.get(kind);
      },
    }),
    [bumpDetailReg]
  );

  // ── URL-derived detail stack ──────────────────────────────────────
  const rhpRaw = searchParams.get(RHP_URL_PARAM);
  const urlDetails = useMemo(() => parseRhpParam(rhpRaw), [rhpRaw]);

  // First-paint URL canonicalization: if the parser dropped malformed
  // segments or trimmed whitespace, rewrite to canonical (replace
  // mode keeps history clean).
  useEffect(() => {
    if (rhpRaw === null) return;
    const canonical = encodeRhpParam(urlDetails);
    if (canonical === rhpRaw) return;
    const next = new URLSearchParams(searchParams);
    if (canonical) next.set(RHP_URL_PARAM, canonical);
    else next.delete(RHP_URL_PARAM);
    setSearchParams(next, { replace: true });
    // We deliberately depend only on rhpRaw — when the param changes
    // we revalidate. Including searchParams/setSearchParams would
    // cause re-runs on unrelated param flips.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rhpRaw]);

  // Focused-detail tracker. Distinct from the URL stack so the operator
  // can click a tab to focus it without re-ordering the stack. On
  // first paint with a URL stack, the rightmost is implicitly focused
  // (see activeTabId derivation below).
  const [focusedKind, setFocusedKind] = useState<string | null>(null);
  // Active pinned tab id (Help in v1).
  const [activePinnedId, setActivePinnedId] = useState<string | null>(null);
  // gm-root.22.9: distinguishes "user explicitly clicked the pinned
  // tab" from "no manual focus yet (cold-start hydrate-from-URL)". The
  // former should win over the rightmost-detail fallback in the
  // activeTabId derivation; the latter is the documented hydrate path.
  const [userPickedPinned, setUserPickedPinned] = useState<boolean>(false);

  // ── Layout state ─────────────────────────────────────────────────
  const [width, setWidthState] = useState<number>(() => readStoredWidth());
  const [collapsed, setCollapsedState] = useState<boolean>(() => {
    const stored = readStoredCollapsed();
    if (stored != null) return stored;
    // Cold-start branch: no persisted preference. Default to collapsed
    // so the operator sees max canvas; the post-mount effect below
    // flips us open if a tab arrives.
    return true;
  });
  const userCollapsedPref = useRef<boolean>(readStoredCollapsed() != null);

  // Cold-start flip: if no preference is stored and a tab exists
  // (pinned or detail), expand. Once a tab exists we no longer want to
  // keep the operator staring at a 40px sliver.
  useEffect(() => {
    if (userCollapsedPref.current) return;
    if (pinned.length === 0 && urlDetails.length === 0) return;
    setCollapsedState(false);
  }, [pinned.length, urlDetails.length]);

  const setWidth = useCallback((next: number) => {
    const clamped = clampWidth(next);
    setWidthState(clamped);
    try {
      window.localStorage.setItem(RHP_WIDTH_STORAGE_KEY, String(clamped));
    } catch {
      // localStorage may throw in privacy-mode / quota cases — ignore.
    }
  }, []);

  const setCollapsed = useCallback((next: boolean) => {
    userCollapsedPref.current = true;
    setCollapsedState(next);
    try {
      window.localStorage.setItem(RHP_COLLAPSED_STORAGE_KEY, String(next));
    } catch {
      // ignore
    }
  }, []);

  const registerPinnedTab = useCallback(
    (tab: { id: string; icon: LucideIcon; label: string }) => {
      setPinned((prev) => {
        // De-dupe by id — re-register replaces the existing entry in
        // place (preserves registration order).
        const idx = prev.findIndex((p) => p.id === tab.id);
        if (idx >= 0) {
          const next = prev.slice();
          next[idx] = tab;
          return next;
        }
        return [...prev, tab];
      });
      // First pinned tab also becomes the default-active pinned tab.
      // Per design doc: Help is the default-active tab on first mount.
      setActivePinnedId((cur) => cur ?? tab.id);
      return () => {
        setPinned((prev) => prev.filter((p) => p.id !== tab.id));
        setActivePinnedId((cur) => (cur === tab.id ? null : cur));
      };
    },
    []
  );

  // ── URL writer ────────────────────────────────────────────────────

  const writeDetails = useCallback(
    (next: DetailUrlEntry[]) => {
      const params = new URLSearchParams(searchParams);
      if (next.length === 0) params.delete(RHP_URL_PARAM);
      else params.set(RHP_URL_PARAM, encodeRhpParam(next));
      setSearchParams(params, { replace: true });
    },
    [searchParams, setSearchParams]
  );

  // ── Per-current-route scoping ─────────────────────────────────────
  // On pathname change, clear all detail tabs and the ?rhp param.
  // Help auto-refocuses (because activeTabId falls back to the active
  // pinned tab when there are no detail tabs).
  const lastPathRef = useRef<string | null>(null);
  useEffect(() => {
    if (lastPathRef.current === null) {
      // First mount: record the path but don't clear (deep-link must
      // hydrate from the URL).
      lastPathRef.current = location.pathname;
      return;
    }
    if (lastPathRef.current === location.pathname) return;
    lastPathRef.current = location.pathname;
    setFocusedKind(null);
    setUserPickedPinned(false);
    if (searchParams.get(RHP_URL_PARAM) !== null) {
      const next = new URLSearchParams(searchParams);
      next.delete(RHP_URL_PARAM);
      setSearchParams(next, { replace: true });
    }
  }, [location.pathname, searchParams, setSearchParams]);

  // ── Detail-tab actions ────────────────────────────────────────────

  const popDetail = useCallback(
    ({ kind, id }: RhpDetailRequest) => {
      const existing = urlDetails.findIndex((d) => d.kind === kind);
      let next: DetailUrlEntry[];
      if (existing >= 0) {
        // Same-kind replace: swap the id in place, keep position.
        next = urlDetails.slice();
        next[existing] = { kind, id };
      } else {
        // Different-kind stack: append to the end.
        next = [...urlDetails, { kind, id }];
      }
      writeDetails(next);
      setFocusedKind(kind);
      setUserPickedPinned(false);
    },
    [urlDetails, writeDetails]
  );

  const closeDetail = useCallback(
    (kind: string, id?: string) => {
      const next = urlDetails.filter((d) => {
        if (d.kind !== kind) return true;
        if (id !== undefined && d.id !== id) return true;
        return false;
      });
      if (next.length === urlDetails.length) return;
      writeDetails(next);
      // If the closed tab was focused, shift focus to the new
      // rightmost detail, or back to the active pinned tab (Help) if
      // none are left.
      if (focusedKind === kind) {
        if (next.length > 0) {
          setFocusedKind(next[next.length - 1].kind);
        } else {
          setFocusedKind(null);
        }
      }
    },
    [urlDetails, focusedKind, writeDetails]
  );

  const focusTab = useCallback(
    (id: string) => {
      // Pinned tab: id is the registration id (e.g. 'help').
      const pinnedHit = pinned.some((p) => p.id === id);
      if (pinnedHit) {
        setActivePinnedId(id);
        setFocusedKind(null);
        setUserPickedPinned(true);
        return;
      }
      // Detail tab: id is 'kind:id'. Split on first ':' to match the
      // URL codec rule.
      const colon = id.indexOf(':');
      if (colon > 0) {
        const kind = id.slice(0, colon);
        if (urlDetails.some((d) => d.kind === kind)) {
          setFocusedKind(kind);
          setUserPickedPinned(false);
        }
      }
    },
    [pinned, urlDetails]
  );

  const closeTab = useCallback(
    (id: string) => {
      // No-op on pinned tabs.
      if (pinned.some((p) => p.id === id)) return;
      const colon = id.indexOf(':');
      if (colon <= 0) return;
      const kind = id.slice(0, colon);
      const detailId = id.slice(colon + 1);
      closeDetail(kind, detailId);
    },
    [pinned, closeDetail]
  );

  // ── Tabs derivation ──────────────────────────────────────────────

  const tabs = useMemo<RhpTab[]>(() => {
    const out: RhpTab[] = [];
    for (const p of pinned) {
      out.push({
        id: p.id,
        kind: p.id, // pinned id == kind (e.g. 'help')
        pinned: true,
        icon: p.icon,
        label: p.label,
      });
    }
    for (const d of urlDetails) {
      const reg = detailRegRef.current.get(d.kind);
      // Fallback registration is registered by RhpDetail.tsx on mount
      // so we always have an icon for kinds that haven't been
      // registered yet (placeholder body explains the situation).
      const fallback = detailRegRef.current.get(RHP_FALLBACK_KIND);
      const icon = reg?.icon ?? fallback?.icon;
      const label = reg?.label ?? d.kind;
      if (!icon) {
        // No fallback yet (RhpDetail not mounted). Skip; the next
        // render after RhpDetail mounts includes the tab.
        continue;
      }
      out.push({
        id: `${d.kind}:${d.id}`,
        kind: d.kind,
        pinned: false,
        icon,
        label,
      });
    }
    return out;
    // detailRegVersion drives recompute when the registry changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pinned, urlDetails, detailRegVersion]);

  const activeTabId = useMemo<string | null>(() => {
    if (focusedKind) {
      const hit = urlDetails.find((d) => d.kind === focusedKind);
      if (hit) return `${hit.kind}:${hit.id}`;
    }
    if (focusedKind === null && urlDetails.length > 0 && !userPickedPinned) {
      // Hydrate-from-URL: rightmost is focused on first paint when
      // the operator hasn't manually picked a tab yet. Suppressed once
      // the operator clicks a pinned tab (gm-root.22.9) so that
      // explicit pinned focus wins over the rightmost-detail fallback.
      const last = urlDetails[urlDetails.length - 1];
      return `${last.kind}:${last.id}`;
    }
    return activePinnedId;
  }, [focusedKind, urlDetails, activePinnedId, userPickedPinned]);

  const value = useMemo<RhpAPI>(
    () => ({
      tabs,
      activeTabId,
      focusTab,
      closeTab,
      popDetail,
      closeDetail,
      registerPinnedTab,
      collapsed,
      setCollapsed,
      width,
      setWidth,
    }),
    [
      tabs,
      activeTabId,
      focusTab,
      closeTab,
      popDetail,
      closeDetail,
      registerPinnedTab,
      collapsed,
      setCollapsed,
      width,
      setWidth,
    ]
  );

  return (
    <RhpContext.Provider value={value}>
      <RhpDetailRegistryContext.Provider value={detailRegistry}>
        {children}
      </RhpDetailRegistryContext.Provider>
    </RhpContext.Provider>
  );
}

export function useRhp(): RhpAPI {
  const ctx = useContext(RhpContext);
  if (!ctx) {
    // Same posture as usePalette / useCapabilities — a missing
    // provider is a programmer error and should fail loudly under
    // test rather than silently degrading at the user's first click.
    throw new Error('useRhp: no RhpProvider in tree');
  }
  return ctx;
}
