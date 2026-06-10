// /recent — surfaces beads created since the operator's personal
// watermark (gm-g5xz.2 + .3). Default cutoff is 24h ago; a button row
// of presets (1h, 4h, 12h, 24h, 3d, 7d, 30d, all) and an "Advance to
// now" action let the operator move the cutoff forward (drain) or back
// (see more). The cutoff persists per-browser in localStorage.
//
// No per-bead "reviewed" state — the watermark is the entire control
// surface. Server side is just `?created_since=` on GET /api/work-items.

import { useCallback, useEffect, useMemo, useState } from 'react';
import { Clock, ChevronDown, ChevronRight, Sparkles } from 'lucide-react';
import { useFilteredWorkItems } from '@/hooks/useWorkItems';
import { useRhp } from '@/components/rhp/RhpContext';
import type { WorkItem } from '@/types/core.gen';
import { cn } from '@/lib/utils';

// localStorage key for the operator's personal watermark. Stored as an
// ISO8601 timestamp string. Reading restores their last cutoff so a
// reload doesn't reset to 24h.
const WATERMARK_KEY = 'gemba.recent.watermark';

// Discrete window presets. "all" maps to the unix epoch so the request
// returns everything the WorkPlane has — useful when the operator wants
// to scrub backward beyond a month.
type Preset = {
  id: string;
  label: string;
  hours: number; // 0 = all
};

const PRESETS: readonly Preset[] = [
  { id: '1h', label: '1h', hours: 1 },
  { id: '4h', label: '4h', hours: 4 },
  { id: '12h', label: '12h', hours: 12 },
  { id: '24h', label: '24h', hours: 24 },
  { id: '3d', label: '3d', hours: 72 },
  { id: '7d', label: '7d', hours: 168 },
  { id: '30d', label: '30d', hours: 720 },
  { id: 'all', label: 'All', hours: 0 },
];

const DEFAULT_PRESET = '24h';

// computeCutoff turns a preset id into an ISO timestamp the API accepts.
// Preset 'all' returns epoch-zero so the server returns everything.
function computeCutoff(presetId: string): string {
  const preset = PRESETS.find((p) => p.id === presetId) ?? PRESETS[3]!;
  if (preset.hours === 0) return new Date(0).toISOString();
  const ms = preset.hours * 60 * 60 * 1000;
  return new Date(Date.now() - ms).toISOString();
}

// readWatermark restores the operator's last preset selection from
// localStorage. Returns the default when missing or malformed.
function readWatermarkPreset(): string {
  try {
    const raw = localStorage.getItem(WATERMARK_KEY);
    if (!raw) return DEFAULT_PRESET;
    if (PRESETS.find((p) => p.id === raw)) return raw;
    return DEFAULT_PRESET;
  } catch {
    return DEFAULT_PRESET;
  }
}

function writeWatermarkPreset(presetId: string): void {
  try {
    localStorage.setItem(WATERMARK_KEY, presetId);
  } catch {
    /* private mode / quota — degrade silently to in-memory state */
  }
}

// formatRelative turns "minutes ago" into a friendly string. Used in
// the empty-state copy and the per-row "age" cell.
function formatRelative(iso: string, now = Date.now()): string {
  const ms = now - Date.parse(iso);
  if (Number.isNaN(ms) || ms < 0) return iso;
  const minutes = Math.floor(ms / 60_000);
  if (minutes < 1) return 'just now';
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  return `${days}d ago`;
}

export function RecentPage() {
  const [presetId, setPresetId] = useState<string>(() => readWatermarkPreset());

  // Recompute the actual ISO cutoff each render — that way a slow user
  // staying on the page doesn't see a stale window. The query key is
  // memoised against the cutoff so we don't refetch every keystroke.
  const cutoff = useMemo(() => computeCutoff(presetId), [presetId]);

  // Persist preset selection on every change so reload keeps state.
  useEffect(() => {
    writeWatermarkPreset(presetId);
  }, [presetId]);

  const onAdvanceToNow = useCallback(() => {
    // Snap the watermark to the smallest non-zero window. "Now"
    // technically means 0h-ago but a 0-hour window returns nothing
    // until the next bead arrives — which feels broken. 1h gives
    // immediate emptiness when nothing is happening and a clean
    // single-row view when a bead lands moments later.
    setPresetId('1h');
  }, []);

  const { data = [], isLoading, error } = useFilteredWorkItems({
    created_since: cutoff,
  });

  // Sort newest first so the operator's eye lands on the freshest work.
  const rows = useMemo(() => {
    const out = data.slice();
    out.sort((a, b) => Date.parse(b.created_at) - Date.parse(a.created_at));
    return out;
  }, [data]);

  return (
    <div className="flex h-full flex-col">
      <header className="border-b border-neutral-200 px-6 py-4 dark:border-neutral-800">
        <div className="flex items-baseline gap-3">
          <h1 className="text-xl font-semibold tracking-tight">Recent</h1>
          <p className="text-sm text-neutral-500 dark:text-neutral-400">
            Beads created in the selected window. Move the cutoff to scrub
            history; advance to drain.
          </p>
        </div>
        <div
          className="mt-3 flex flex-wrap items-center gap-2"
          data-testid="recent-watermark-controls"
        >
          {PRESETS.map((p) => (
            <button
              key={p.id}
              type="button"
              onClick={() => setPresetId(p.id)}
              data-testid={`recent-preset-${p.id}`}
              aria-pressed={presetId === p.id}
              className={cn(
                'rounded-md border px-2.5 py-1 text-xs font-medium transition-colors',
                presetId === p.id
                  ? 'border-neutral-900 bg-neutral-900 text-white dark:border-white dark:bg-white dark:text-neutral-900'
                  : 'border-neutral-200 bg-white text-neutral-700 hover:bg-neutral-100 dark:border-neutral-800 dark:bg-neutral-950 dark:text-neutral-300 dark:hover:bg-neutral-900'
              )}
            >
              {p.label}
            </button>
          ))}
          <button
            type="button"
            onClick={onAdvanceToNow}
            data-testid="recent-advance-to-now"
            className="ml-auto inline-flex items-center gap-1 rounded-md border border-neutral-200 bg-white px-2.5 py-1 text-xs font-medium text-neutral-700 hover:bg-neutral-100 dark:border-neutral-800 dark:bg-neutral-950 dark:text-neutral-300 dark:hover:bg-neutral-900"
            title="Snap watermark to the most recent window"
          >
            <Sparkles className="h-3.5 w-3.5" aria-hidden />
            Advance to now
          </button>
        </div>
        <p
          className="mt-2 text-xs text-neutral-500 dark:text-neutral-400"
          data-testid="recent-cutoff-label"
        >
          <Clock className="mr-1 inline h-3 w-3" aria-hidden />
          Showing beads since{' '}
          <code className="font-mono">{cutoff}</code> ({formatRelative(cutoff)}).
          {!isLoading && ` ${rows.length} item${rows.length === 1 ? '' : 's'}.`}
        </p>
      </header>

      <main className="flex-1 overflow-auto px-6 py-4">
        {isLoading && (
          <p className="text-sm text-neutral-500 dark:text-neutral-400">
            Loading…
          </p>
        )}
        {error && (
          <p className="text-sm text-rose-600 dark:text-rose-400">
            Failed to load recent items: {error.message}
          </p>
        )}
        {!isLoading && !error && rows.length === 0 && (
          <p
            className="text-sm text-neutral-500 dark:text-neutral-400"
            data-testid="recent-empty"
          >
            No new beads since {formatRelative(cutoff)}. Try widening the
            window or wait for agents to produce work.
          </p>
        )}
        {!isLoading && !error && rows.length > 0 && (
          <RecentList rows={rows} />
        )}
      </main>
    </div>
  );
}

function RecentList({ rows }: { rows: WorkItem[] }) {
  const { popDetail } = useRhp();
  // Group by parent_id so a freshly-spawned epic and its children land
  // together. Parent-less rows render in a top-level "Standalone" group.
  const groups = useMemo(() => groupByParent(rows), [rows]);

  return (
    <ul className="flex flex-col gap-1" data-testid="recent-list">
      {groups.map((g) => (
        <RecentGroup key={g.parentId ?? '__none__'} group={g} onOpen={(id) =>
          popDetail({ kind: 'workitem', id })
        } />
      ))}
    </ul>
  );
}

interface ParentGroup {
  parentId: string | null;
  parentTitle: string | null;
  items: WorkItem[];
}

// parentOf finds the parent_child edge pointing to wi (convention:
// from=parent, to=child) and returns the parent id when set.
function parentOf(wi: WorkItem): string | null {
  for (const r of wi.relationships ?? []) {
    if (r.kind === 'parent_child' && r.to === wi.id) return r.from;
  }
  return null;
}

function groupByParent(rows: WorkItem[]): ParentGroup[] {
  const byParent = new Map<string, ParentGroup>();
  const order: string[] = [];
  for (const wi of rows) {
    const parentId = parentOf(wi);
    const key = parentId ?? '__none__';
    if (!byParent.has(key)) {
      // parentTitle is filled when the parent itself is in the
      // window — otherwise we just show the parent id as breadcrumb.
      const parentInList = parentId
        ? rows.find((r) => r.id === parentId)
        : null;
      byParent.set(key, {
        parentId: parentId,
        parentTitle: parentInList?.title ?? null,
        items: [],
      });
      order.push(key);
    }
    byParent.get(key)!.items.push(wi);
  }
  return order.map((k) => byParent.get(k)!);
}

function RecentGroup({
  group,
  onOpen,
}: {
  group: ParentGroup;
  onOpen: (id: string) => void;
}) {
  const [open, setOpen] = useState(true);
  const heading = group.parentId
    ? group.parentTitle
      ? `${group.parentTitle} (${group.parentId})`
      : group.parentId
    : 'Standalone';
  return (
    <li className="rounded-md border border-neutral-200 dark:border-neutral-800">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm font-medium hover:bg-neutral-50 dark:hover:bg-neutral-900"
      >
        {open ? (
          <ChevronDown className="h-4 w-4" aria-hidden />
        ) : (
          <ChevronRight className="h-4 w-4" aria-hidden />
        )}
        <span className="truncate">{heading}</span>
        <span className="ml-auto text-xs text-neutral-500 dark:text-neutral-400">
          {group.items.length}
        </span>
      </button>
      {open && (
        <ul className="border-t border-neutral-200 dark:border-neutral-800">
          {group.items.map((wi) => (
            <RecentRow key={wi.id} wi={wi} onOpen={onOpen} />
          ))}
        </ul>
      )}
    </li>
  );
}

function RecentRow({
  wi,
  onOpen,
}: {
  wi: WorkItem;
  onOpen: (id: string) => void;
}) {
  return (
    <li>
      <button
        type="button"
        onClick={() => onOpen(wi.id)}
        data-testid={`recent-row-${wi.id}`}
        className="flex w-full items-center gap-3 px-3 py-1.5 text-left text-sm hover:bg-neutral-50 dark:hover:bg-neutral-900"
      >
        <span className="inline-block w-12 shrink-0 truncate rounded bg-neutral-100 px-1.5 py-0.5 text-center text-[10px] font-medium uppercase tracking-wide text-neutral-700 dark:bg-neutral-800 dark:text-neutral-300">
          {wi.kind}
        </span>
        <span className="flex-1 truncate">{wi.title}</span>
        <span className="shrink-0 text-xs text-neutral-500 dark:text-neutral-400">
          {formatRelative(wi.created_at)}
        </span>
      </button>
    </li>
  );
}

// Internal exports for unit tests.
export const __internal = {
  computeCutoff,
  readWatermarkPreset,
  writeWatermarkPreset,
  formatRelative,
  groupByParent,
  WATERMARK_KEY,
  PRESETS,
  DEFAULT_PRESET,
};
