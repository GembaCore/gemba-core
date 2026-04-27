// AgentGroupsPage (gm-e12.8). Top-level route that lists every
// AgentGroup the bound OrchestrationPlane exposes. Each group is
// rendered through AgentGroupBoard, which dispatches on
// AgentGroup.mode (static | pool | graph).
//
// Empty state: no orchestration plane bound (or bound and reporting
// no groups) renders a "no groups" copy block — same convention as
// the AssigneePicker / SessionsPage when the orchestration surface is
// silent. The AdaptorBanner already covers degraded-adaptor calls.

import { useMemo } from 'react';
import { Boxes } from 'lucide-react';
import { useAgentGroups } from '@/hooks/useAgentGroups';
import { AgentGroupBoard } from '@/components/AgentGroupBoard';
import type { AgentGroup, GroupMode } from '@/types/core.gen';

const MODE_ORDER: Record<GroupMode, number> = {
  static: 0,
  pool: 1,
  graph: 2,
};

export function AgentGroupsPage() {
  const { data: groups = [], isLoading, error } = useAgentGroups();

  // Stable sort: by mode (static → pool → graph), then by display name.
  // The Gas Town adaptor emits singletons (Mayor, Deacon) and crews
  // (Witnesses, Refineries) before the pool groups, so this sort keeps
  // its ordering even though we don't depend on it.
  const sorted = useMemo(() => {
    return [...groups].sort((a, b) => {
      const md = MODE_ORDER[a.mode] - MODE_ORDER[b.mode];
      if (md !== 0) return md;
      return (a.display_name || a.id).localeCompare(b.display_name || b.id);
    });
  }, [groups]);

  return (
    <div className="flex h-full flex-col" data-testid="agent-groups-page">
      <header className="flex items-center justify-between border-b border-neutral-200 px-8 py-4 dark:border-neutral-800">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">Agent groups</h1>
          <p className="text-xs text-neutral-500 dark:text-neutral-400">
            Static crews, elastic pools, and adaptor-owned graphs (gm-root DD-7).
          </p>
        </div>
        <ModeLegend groups={sorted} />
      </header>

      <div className="flex-1 overflow-auto p-8">
        {error && (
          <div
            data-testid="agent-groups-error"
            className="mb-6 rounded-md bg-red-50 px-4 py-3 text-sm text-red-700 dark:bg-red-950 dark:text-red-300"
          >
            {error.message || 'Failed to load agent groups.'}
          </div>
        )}

        {isLoading && !error && (
          <p className="text-sm text-neutral-500 dark:text-neutral-400">
            Loading agent groups…
          </p>
        )}

        {!isLoading && !error && sorted.length === 0 && (
          <EmptyState />
        )}

        {sorted.length > 0 && (
          <div
            data-testid="agent-groups-grid"
            className="grid grid-cols-1 gap-4 lg:grid-cols-2"
          >
            {sorted.map((g) => (
              <AgentGroupBoard key={g.id} group={g} />
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

function EmptyState() {
  return (
    <div
      data-testid="agent-groups-empty"
      className="mx-auto max-w-md rounded-lg border border-dashed border-neutral-300 bg-neutral-50 px-6 py-10 text-center dark:border-neutral-700 dark:bg-neutral-950"
    >
      <Boxes className="mx-auto mb-3 h-8 w-8 text-neutral-400" aria-hidden />
      <h2 className="text-sm font-semibold text-neutral-700 dark:text-neutral-200">
        No agent groups
      </h2>
      <p className="mt-1 text-xs text-neutral-500 dark:text-neutral-400">
        The bound orchestration plane reports no groups. The noop adaptor
        is silent here; bind a richer adaptor (Gas Town, LangGraph,
        CrewAI) to populate this view.
      </p>
    </div>
  );
}

function ModeLegend({ groups }: { groups: AgentGroup[] }) {
  const counts = useMemo(() => {
    const c: Record<GroupMode, number> = { static: 0, pool: 0, graph: 0 };
    for (const g of groups) c[g.mode] += 1;
    return c;
  }, [groups]);
  if (groups.length === 0) return null;
  return (
    <div
      data-testid="agent-groups-legend"
      className="flex items-center gap-2 text-[10px] uppercase tracking-wider"
    >
      <Chip label="static" count={counts.static} tone="emerald" />
      <Chip label="pool" count={counts.pool} tone="amber" />
      <Chip label="graph" count={counts.graph} tone="violet" />
    </div>
  );
}

function Chip({
  label,
  count,
  tone,
}: {
  label: string;
  count: number;
  tone: 'emerald' | 'amber' | 'violet';
}) {
  const cls =
    tone === 'emerald'
      ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-950 dark:text-emerald-300'
      : tone === 'amber'
        ? 'bg-amber-100 text-amber-700 dark:bg-amber-950 dark:text-amber-300'
        : 'bg-violet-100 text-violet-700 dark:bg-violet-950 dark:text-violet-300';
  return (
    <span className={`rounded-full px-2 py-0.5 ${cls}`}>
      {label} · {count}
    </span>
  );
}
