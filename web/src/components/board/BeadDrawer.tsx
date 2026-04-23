// BeadDrawer (gm-qai / M1.7c): right-side drill-in that renders every
// attribute of a WorkItem returned by useBead(id).
//
// Controlled by the parent via (openId, onClose). Internally maintains a
// small nav stack so clicking a Relationship swaps the drawer to the
// target bead while preserving a Back affordance. When openId changes
// externally (user clicks a new card while the drawer is already open)
// the stack resets to that id — parent-initiated navigation is an
// explicit "new drawer", not a history push.
//
// Design tenets:
//   - Show every field on the wire. Collapse sections that are empty
//     (rather than dropping them silently) so missing data is visible.
//   - Relationships are the navigable surface; clicking them pushes a
//     new id onto the stack. Extension edges from Custom["beads:*"]
//     render with an "ext" badge so the user can see that those come
//     from the adaptor, not core.
//   - No markdown library pulled in for this milestone — Description /
//     notes render as whitespace-preserved prose. Rich rendering is a
//     deliberate follow-up (see gm-qai DoD: "visible; none silently
//     dropped", not "formatted").

import { useCallback, useEffect, useMemo, useState } from 'react';
import * as Dialog from '@radix-ui/react-dialog';
import { ArrowLeft, Check, Copy, X } from 'lucide-react';
import { useBead } from '@/hooks/useBeads';
import { useCapabilities } from '@/capabilities';
import { cn } from '@/lib/utils';
import type { Evidence, WorkItem } from '@/types/core.gen';
import { rendererFor } from './descriptionRenderers';

export interface BeadDrawerProps {
  // Bead id to show. null keeps the drawer closed. Changing this prop
  // resets the internal nav stack (treat it as parent-initiated open).
  openId: string | null;
  onClose: () => void;
}

export function BeadDrawer({ openId, onClose }: BeadDrawerProps) {
  // Seed from openId so the first render already has a currentId —
  // otherwise Radix briefly mounts <Dialog.Content> without a
  // <Dialog.Title> (rendered by BeadDrawerBody) and logs an a11y
  // warning before the effect runs.
  const [stack, setStack] = useState<string[]>(() => (openId ? [openId] : []));

  useEffect(() => {
    if (openId && openId !== stack[stack.length - 1]) {
      setStack([openId]);
    } else if (!openId && stack.length > 0) {
      setStack([]);
    }
    // Only react to parent-initiated openId transitions. The in-drawer
    // push/pop handlers own everything else.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [openId]);

  const currentId = stack[stack.length - 1] ?? null;
  const canGoBack = stack.length > 1;

  const goBack = useCallback(() => {
    setStack((s) => (s.length > 1 ? s.slice(0, -1) : s));
  }, []);

  const navigate = useCallback((id: string) => {
    setStack((s) => [...s, id]);
  }, []);

  const handleOpenChange = useCallback(
    (open: boolean) => {
      if (!open) onClose();
    },
    [onClose]
  );

  return (
    <Dialog.Root open={!!openId} onOpenChange={handleOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay
          className="fixed inset-0 z-40 bg-black/40 backdrop-blur-sm data-[state=open]:animate-in data-[state=open]:fade-in-0 data-[state=closed]:animate-out data-[state=closed]:fade-out-0"
          data-testid="bead-drawer-overlay"
        />
        <Dialog.Content
          aria-describedby={undefined}
          className="fixed right-0 top-0 z-50 flex h-full w-full max-w-xl flex-col border-l border-neutral-200 bg-white shadow-xl outline-none dark:border-neutral-800 dark:bg-neutral-950 data-[state=open]:animate-in data-[state=open]:slide-in-from-right data-[state=closed]:animate-out data-[state=closed]:slide-out-to-right"
          data-testid="bead-drawer-content"
        >
          {currentId ? (
            <BeadDrawerBody
              id={currentId}
              canGoBack={canGoBack}
              onBack={goBack}
              onNavigate={navigate}
            />
          ) : null}
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

interface BeadDrawerBodyProps {
  id: string;
  canGoBack: boolean;
  onBack: () => void;
  onNavigate: (id: string) => void;
}

function BeadDrawerBody({ id, canGoBack, onBack, onNavigate }: BeadDrawerBodyProps) {
  const { data, isLoading, error } = useBead(id);

  return (
    <>
      <DrawerHeader id={id} title={data?.title ?? ''} canGoBack={canGoBack} onBack={onBack} />
      <div className="flex-1 overflow-y-auto px-6 pb-10" data-testid="bead-drawer-scroll">
        {isLoading ? (
          <div className="py-8 text-sm text-neutral-500" data-testid="bead-drawer-loading">
            Loading bead…
          </div>
        ) : error ? (
          <div
            className="py-8 text-sm text-red-600 dark:text-red-400"
            data-testid="bead-drawer-error"
          >
            {error.message}
          </div>
        ) : data ? (
          <BeadBody item={data} onNavigate={onNavigate} />
        ) : null}
      </div>
    </>
  );
}

function DrawerHeader({
  id,
  title,
  canGoBack,
  onBack,
}: {
  id: string;
  title: string;
  canGoBack: boolean;
  onBack: () => void;
}) {
  const [copied, setCopied] = useState(false);
  const copyId = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(id);
      setCopied(true);
      setTimeout(() => setCopied(false), 1200);
    } catch {
      // Clipboard unavailable (jsdom, insecure context). Surface the
      // state transiently so the user knows the click registered; a
      // future milestone can add a manual-copy fallback.
      setCopied(false);
    }
  }, [id]);

  return (
    <div className="flex items-start gap-2 border-b border-neutral-200 px-6 py-4 dark:border-neutral-800">
      {canGoBack ? (
        <button
          type="button"
          onClick={onBack}
          className="mt-1 rounded p-1 text-neutral-500 hover:bg-neutral-100 hover:text-neutral-900 dark:hover:bg-neutral-800 dark:hover:text-neutral-100"
          aria-label="Back"
          data-testid="bead-drawer-back"
        >
          <ArrowLeft className="h-4 w-4" />
        </button>
      ) : null}
      <div className="min-w-0 flex-1">
        <Dialog.Title className="truncate text-base font-semibold text-neutral-900 dark:text-neutral-100">
          {title || id}
        </Dialog.Title>
        <div className="mt-1 flex items-center gap-1 text-xs text-neutral-500">
          <span className="font-mono" data-testid="bead-drawer-id">
            {id}
          </span>
          <button
            type="button"
            onClick={copyId}
            className="rounded p-0.5 hover:bg-neutral-100 hover:text-neutral-900 dark:hover:bg-neutral-800 dark:hover:text-neutral-100"
            aria-label="Copy bead ID"
            data-testid="bead-drawer-copy"
          >
            {copied ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
          </button>
        </div>
      </div>
      <Dialog.Close
        className="mt-1 rounded p-1 text-neutral-500 hover:bg-neutral-100 hover:text-neutral-900 dark:hover:bg-neutral-800 dark:hover:text-neutral-100"
        aria-label="Close"
        data-testid="bead-drawer-close"
      >
        <X className="h-4 w-4" />
      </Dialog.Close>
    </div>
  );
}

function BeadBody({ item, onNavigate }: { item: WorkItem; onNavigate: (id: string) => void }) {
  const grouped = useMemo(() => groupRelationships(item), [item]);
  const customGroups = useMemo(() => groupCustom(item.custom), [item.custom]);
  const timestamps = useMemo(() => extractTimestamps(item), [item]);
  const sprintBudget = useMemo(() => extractSprintBudget(item.custom), [item.custom]);
  const closeReason = useMemo(() => extractCloseReason(item.custom), [item.custom]);
  // The adaptor declares how its Description field should be rendered
  // (plain / markdown / …) on the CapabilityManifest. We pick the right
  // component from the registry on every render rather than threading
  // the format through props; the manifest changes rarely (adaptor
  // restart) and useCapabilities memoizes.
  const { workPlane } = useCapabilities();
  const DescriptionRenderer = rendererFor(workPlane?.description_format);

  return (
    <div className="space-y-6 pt-4">
      <Section title="Overview" testid="section-overview">
        <div className="flex flex-wrap gap-2">
          <Chip label="status" value={item.status} />
          <Chip label="state" value={item.state_category} />
          <Chip label="type" value={item.kind} />
          {item.priority != null ? <Chip label="P" value={String(item.priority)} /> : null}
        </div>
        <DefRow label="Assignee">
          <AgentPill agent={item.assignee ?? null} />
        </DefRow>
        <DefRow label="Owner">
          <AgentPill agent={item.owner ?? null} />
        </DefRow>
        <DefRow label="Labels">
          {item.labels && item.labels.length > 0 ? (
            <div className="flex flex-wrap gap-1">
              {item.labels.map((l) => (
                <span
                  key={l}
                  className="rounded bg-neutral-100 px-1.5 py-0.5 font-mono text-xs text-neutral-700 dark:bg-neutral-800 dark:text-neutral-300"
                >
                  {l}
                </span>
              ))}
            </div>
          ) : (
            <Muted>none</Muted>
          )}
        </DefRow>
      </Section>

      <Section title="Description" testid="section-description">
        {item.description ? (
          <DescriptionRenderer source={item.description} />
        ) : (
          <Muted>No description.</Muted>
        )}
      </Section>

      {closeReason ? (
        <Section title="Close reason" testid="section-close-reason">
          <pre className="whitespace-pre-wrap break-words font-sans text-sm text-neutral-800 dark:text-neutral-200">
            {closeReason}
          </pre>
        </Section>
      ) : null}

      <Section title="Relationships" testid="section-relationships">
        <RelGroup label="blocks" rows={grouped.blocks} onNavigate={onNavigate} />
        <RelGroup label="blocked by" rows={grouped.blockedBy} onNavigate={onNavigate} />
        <RelGroup label="parent" rows={grouped.parent} onNavigate={onNavigate} />
        <RelGroup label="children" rows={grouped.children} onNavigate={onNavigate} />
        <RelGroup label="relates to" rows={grouped.relatesTo} onNavigate={onNavigate} />
        <RelGroup label="extension edges" rows={grouped.extension} onNavigate={onNavigate} ext />
        {!grouped.any ? <Muted>No relationships.</Muted> : null}
      </Section>

      <Section title="Evidence" testid="section-evidence">
        {item.evidence && item.evidence.length > 0 ? (
          <ul className="space-y-2">
            {item.evidence.map((e) => (
              <EvidenceRow key={e.id} evidence={e} />
            ))}
          </ul>
        ) : (
          <Muted>No evidence attached.</Muted>
        )}
      </Section>

      <Section title="Definition of Done" testid="section-dod">
        {item.dod ? (
          <div className="space-y-2 text-sm">
            <ul className="list-disc space-y-1 pl-5">
              {item.dod.acceptance_criteria.map((c, i) => (
                <li key={i}>{c}</li>
              ))}
            </ul>
            {item.dod.notes ? (
              <pre className="whitespace-pre-wrap break-words font-sans text-sm text-neutral-700 dark:text-neutral-300">
                {item.dod.notes}
              </pre>
            ) : null}
            {item.dod.version ? (
              <div className="text-xs text-neutral-500">version {item.dod.version}</div>
            ) : null}
          </div>
        ) : (
          <Muted>No DoD declared.</Muted>
        )}
      </Section>

      <Section title="Sprint & budget" testid="section-sprint">
        {item.sprint_id ? (
          <DefRow label="Sprint">
            <span className="font-mono text-sm">{item.sprint_id}</span>
          </DefRow>
        ) : null}
        {sprintBudget ? (
          <div className="mt-2 space-y-1 text-sm">
            <DefRow label="Budget used">
              <span className="font-mono">
                {sprintBudget.used} / {sprintBudget.limit}
              </span>
            </DefRow>
            <DefRow label="Thresholds">
              <span className="font-mono text-xs text-neutral-500">
                inform={sprintBudget.inform} warn={sprintBudget.warn} stop={sprintBudget.stop}
              </span>
            </DefRow>
          </div>
        ) : null}
        {!item.sprint_id && !sprintBudget ? <Muted>No sprint or budget set.</Muted> : null}
      </Section>

      <Section title="Derived signals" testid="section-derived">
        {item.derived ? (
          <div className="flex flex-wrap gap-2 text-xs">
            <DerivedPill label="agent-claimable" on={item.derived.agent_claimable} />
            <DerivedPill label="human-action-required" on={item.derived.human_action_required} />
            <DerivedPill label="review-pending" on={item.derived.review_pending} />
          </div>
        ) : (
          <Muted>Not populated by the adaptor.</Muted>
        )}
      </Section>

      <Section title="Timestamps" testid="section-timestamps">
        <dl className="grid grid-cols-[max-content_1fr] gap-x-4 gap-y-1 text-sm">
          <Timestamp label="Created" ts={timestamps.created} />
          <Timestamp label="Started" ts={timestamps.started} />
          <Timestamp label="Updated" ts={timestamps.updated} />
          <Timestamp label="Closed" ts={timestamps.closed} />
        </dl>
      </Section>

      {customGroups.length > 0 ? (
        <Section title="Extension fields" testid="section-custom">
          <div className="space-y-4">
            {customGroups.map((g) => (
              <div key={g.namespace}>
                <div className="mb-1 font-mono text-xs uppercase tracking-wide text-neutral-500">
                  {g.namespace}
                </div>
                <dl className="space-y-1">
                  {g.entries.map(([k, v]) => (
                    <CustomRow key={k} fullKey={k} value={v} />
                  ))}
                </dl>
              </div>
            ))}
          </div>
        </Section>
      ) : null}
    </div>
  );
}

function Section({
  title,
  testid,
  children,
}: {
  title: string;
  testid: string;
  children: React.ReactNode;
}) {
  return (
    <section data-testid={testid}>
      <h3 className="mb-2 text-xs font-semibold uppercase tracking-wide text-neutral-500">
        {title}
      </h3>
      <div className="space-y-2">{children}</div>
    </section>
  );
}

function DefRow({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex items-center gap-2 text-sm">
      <span className="w-20 shrink-0 text-xs uppercase tracking-wide text-neutral-500">
        {label}
      </span>
      <div className="min-w-0 flex-1">{children}</div>
    </div>
  );
}

function Muted({ children }: { children: React.ReactNode }) {
  return <span className="text-xs text-neutral-500">{children}</span>;
}

function Chip({ label, value }: { label: string; value: string }) {
  return (
    <span className="inline-flex items-center gap-1 rounded bg-neutral-100 px-2 py-0.5 text-xs dark:bg-neutral-800">
      <span className="text-neutral-500">{label}</span>
      <span className="font-mono text-neutral-900 dark:text-neutral-100">{value}</span>
    </span>
  );
}

function AgentPill({ agent }: { agent: WorkItem['assignee'] | null }) {
  if (!agent) return <Muted>unassigned</Muted>;
  return (
    <span className="inline-flex items-center gap-2 text-sm">
      <span
        className={cn(
          'rounded px-1.5 py-0.5 text-xs',
          agent.agent_kind === 'agent'
            ? 'bg-blue-100 text-blue-800 dark:bg-blue-900/40 dark:text-blue-200'
            : 'bg-emerald-100 text-emerald-800 dark:bg-emerald-900/40 dark:text-emerald-200'
        )}
      >
        {agent.agent_kind}
      </span>
      <span className="font-mono text-xs">{agent.name || agent.id}</span>
      {agent.role ? <span className="text-xs text-neutral-500">· {agent.role}</span> : null}
      {agent.dialect ? <span className="text-xs text-neutral-500">· {agent.dialect}</span> : null}
    </span>
  );
}

function DerivedPill({ label, on }: { label: string; on: boolean }) {
  return (
    <span
      className={cn(
        'rounded px-2 py-0.5',
        on
          ? 'bg-amber-100 text-amber-800 dark:bg-amber-900/40 dark:text-amber-200'
          : 'bg-neutral-100 text-neutral-500 dark:bg-neutral-800 dark:text-neutral-400'
      )}
    >
      {on ? '●' : '○'} {label}
    </span>
  );
}

function Timestamp({ label, ts }: { label: string; ts: string | null }) {
  return (
    <>
      <dt className="text-xs uppercase tracking-wide text-neutral-500">{label}</dt>
      <dd className="font-mono text-xs text-neutral-700 dark:text-neutral-300">
        {ts ?? <span className="text-neutral-500">—</span>}
      </dd>
    </>
  );
}

interface RelRow {
  id: string;
  // extension-edge rows carry a kind hint so the UI can show e.g.
  // "parent" vs. "discovered-from" — core rows leave this undefined.
  hint?: string;
}

function RelGroup({
  label,
  rows,
  onNavigate,
  ext = false,
}: {
  label: string;
  rows: RelRow[];
  onNavigate: (id: string) => void;
  ext?: boolean;
}) {
  if (rows.length === 0) return null;
  return (
    <div data-testid={`relgroup-${label.replace(/\s+/g, '-')}`}>
      <div className="mb-1 text-xs uppercase tracking-wide text-neutral-500">{label}</div>
      <ul className="flex flex-wrap gap-1">
        {rows.map((r, i) => (
          <li key={`${r.id}-${i}`}>
            <button
              type="button"
              onClick={() => onNavigate(r.id)}
              className="inline-flex items-center gap-1 rounded bg-neutral-100 px-1.5 py-0.5 font-mono text-xs text-neutral-800 hover:bg-neutral-200 dark:bg-neutral-800 dark:text-neutral-200 dark:hover:bg-neutral-700"
            >
              {r.id}
              {r.hint ? <span className="text-[10px] text-neutral-500">{r.hint}</span> : null}
              {ext ? (
                <span className="rounded bg-amber-200/60 px-1 text-[9px] uppercase text-amber-900 dark:bg-amber-900/40 dark:text-amber-100">
                  ext
                </span>
              ) : null}
            </button>
          </li>
        ))}
      </ul>
    </div>
  );
}

function EvidenceRow({ evidence }: { evidence: Evidence }) {
  return (
    <li className="rounded border border-neutral-200 p-2 text-sm dark:border-neutral-800">
      <div className="flex items-center gap-2 text-xs text-neutral-500">
        <span className="rounded bg-neutral-100 px-1.5 py-0.5 font-mono dark:bg-neutral-800">
          {evidence.kind}
        </span>
        <span className="font-mono">{evidence.source}</span>
        {evidence.ref ? <span className="truncate font-mono">{evidence.ref}</span> : null}
        <span className="ml-auto font-mono">{formatTs(evidence.captured_at)}</span>
      </div>
      {evidence.summary ? <div className="mt-1">{evidence.summary}</div> : null}
    </li>
  );
}

function CustomRow({ fullKey, value }: { fullKey: string; value: unknown }) {
  const short = fullKey.includes(':') ? fullKey.split(':').slice(1).join(':') : fullKey;
  return (
    <div className="flex gap-2 text-sm">
      <dt className="w-40 shrink-0 truncate font-mono text-xs text-neutral-500" title={fullKey}>
        {short}
      </dt>
      <dd className="min-w-0 flex-1">
        <pre className="whitespace-pre-wrap break-words font-mono text-xs text-neutral-800 dark:text-neutral-200">
          {renderCustomValue(value)}
        </pre>
      </dd>
    </div>
  );
}

// ---------- derivations ----------

interface GroupedRelationships {
  blocks: RelRow[];
  blockedBy: RelRow[];
  parent: RelRow[];
  children: RelRow[];
  relatesTo: RelRow[];
  extension: RelRow[];
  any: boolean;
}

function groupRelationships(item: WorkItem): GroupedRelationships {
  const out: GroupedRelationships = {
    blocks: [],
    blockedBy: [],
    parent: [],
    children: [],
    relatesTo: [],
    extension: [],
    any: false,
  };
  const selfId = item.id;
  for (const r of item.relationships ?? []) {
    if (r.kind === 'blocks') {
      if (r.from === selfId) out.blocks.push({ id: r.to });
      else if (r.to === selfId) out.blockedBy.push({ id: r.from });
    } else if (r.kind === 'parent_child') {
      if (r.from === selfId) out.children.push({ id: r.to });
      else if (r.to === selfId) out.parent.push({ id: r.from });
    } else if (r.kind === 'relates_to') {
      const other = r.from === selfId ? r.to : r.from;
      out.relatesTo.push({ id: other });
    }
  }
  const ext = extractExtensionEdges(item.custom);
  out.extension = ext;
  out.any =
    out.blocks.length +
      out.blockedBy.length +
      out.parent.length +
      out.children.length +
      out.relatesTo.length +
      out.extension.length >
    0;
  return out;
}

// extractExtensionEdges reads adaptor-declared non-core edges from
// Custom. For the bd adaptor those live under "beads:dependencies" /
// "beads:dependents" either as []string or []{issue_id, kind} records
// depending on the bd edge shape (internal/adapter/bd/types.go).
function extractExtensionEdges(custom: Record<string, unknown> | undefined): RelRow[] {
  if (!custom) return [];
  const rows: RelRow[] = [];
  for (const key of ['beads:dependencies', 'beads:dependents']) {
    const raw = custom[key];
    if (!Array.isArray(raw)) continue;
    for (const entry of raw) {
      if (typeof entry === 'string') {
        rows.push({ id: entry, hint: keyHint(key) });
      } else if (entry && typeof entry === 'object') {
        const obj = entry as Record<string, unknown>;
        const id = pickStringField(obj, ['issue_id', 'id', 'to']);
        const kind = typeof obj.kind === 'string' ? obj.kind : undefined;
        if (id) rows.push({ id, hint: kind ?? keyHint(key) });
      }
    }
  }
  return rows;
}

function keyHint(key: string): string {
  return key === 'beads:dependents' ? '←' : '→';
}

function pickStringField(obj: Record<string, unknown>, keys: string[]): string | undefined {
  for (const k of keys) {
    const v = obj[k];
    if (typeof v === 'string' && v.length > 0) return v;
  }
  return undefined;
}

interface CustomGroup {
  namespace: string;
  entries: [string, unknown][];
}

function groupCustom(custom: Record<string, unknown> | undefined): CustomGroup[] {
  if (!custom) return [];
  // Extension-edge blobs are already surfaced in the Relationships
  // section; showing them again under Extension fields is redundant.
  // Same for the handful of fields lifted into their own sections.
  const suppress = new Set([
    'beads:dependencies',
    'beads:dependents',
    'beads:close_reason',
    'beads:sprint',
    'beads:budget',
    'beads:started_at',
    'beads:closed_at',
  ]);
  const byNs = new Map<string, [string, unknown][]>();
  for (const [k, v] of Object.entries(custom)) {
    if (suppress.has(k)) continue;
    const ns = k.includes(':') ? k.split(':')[0] : '(no-namespace)';
    const list = byNs.get(ns) ?? [];
    list.push([k, v]);
    byNs.set(ns, list);
  }
  return Array.from(byNs.entries())
    .map(([namespace, entries]) => ({ namespace, entries: entries.sort() }))
    .sort((a, b) => a.namespace.localeCompare(b.namespace));
}

interface Timestamps {
  created: string | null;
  started: string | null;
  updated: string | null;
  closed: string | null;
}

function extractTimestamps(item: WorkItem): Timestamps {
  const c = item.custom ?? {};
  return {
    created: formatTs(item.created_at),
    updated: formatTs(item.updated_at),
    started: formatCustomTs(c['beads:started_at']),
    closed: formatCustomTs(c['beads:closed_at']),
  };
}

interface SprintBudget {
  limit: number;
  used: number;
  inform: number;
  warn: number;
  stop: number;
}

function extractSprintBudget(custom: Record<string, unknown> | undefined): SprintBudget | null {
  if (!custom) return null;
  const raw = custom['beads:budget'];
  if (!raw || typeof raw !== 'object') return null;
  const o = raw as Record<string, unknown>;
  const pick = (k: string) => (typeof o[k] === 'number' ? (o[k] as number) : undefined);
  const limit = pick('limit');
  const used = pick('used');
  const inform = pick('inform');
  const warn = pick('warn');
  const stop = pick('stop');
  if (
    limit === undefined ||
    used === undefined ||
    inform === undefined ||
    warn === undefined ||
    stop === undefined
  ) {
    return null;
  }
  return { limit, used, inform, warn, stop };
}

function extractCloseReason(custom: Record<string, unknown> | undefined): string | null {
  if (!custom) return null;
  const raw = custom['beads:close_reason'];
  return typeof raw === 'string' && raw.length > 0 ? raw : null;
}

function formatTs(iso: string | null | undefined): string | null {
  if (!iso) return null;
  return iso;
}

function formatCustomTs(v: unknown): string | null {
  return typeof v === 'string' && v.length > 0 ? v : null;
}

function renderCustomValue(v: unknown): string {
  if (v === null || v === undefined) return '—';
  if (typeof v === 'string') return v;
  if (typeof v === 'number' || typeof v === 'boolean') return String(v);
  try {
    return JSON.stringify(v, null, 2);
  } catch {
    return String(v);
  }
}
