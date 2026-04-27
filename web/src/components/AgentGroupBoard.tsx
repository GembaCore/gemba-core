// AgentGroupBoard (gm-e12.8). Mode-dispatched visualization of an
// AgentGroup, the adaptor-agnostic view of a collection of agents
// (gm-root DD-7). The component is purely presentational — fetching is
// the caller's job — so the same shape can be embedded in the
// AgentGroups page, the agents drawer, or a future capability browser
// preview.
//
// Per gm-e12.8's DoD:
//   - static  → fixed-member tiles ("fixed-member tiles")
//   - pool    → desired-vs-actual gauge + check log
//   - graph   → opaque box with member list ("graph topology owned by
//               the adaptor; we render a black-box summary, not the
//               topology — adaptors that want to expose the graph
//               extend with a typed widget under web/src/extensions/<id>/")
//
// All three modes render correctly against the noop adaptor (which
// returns no groups, surfaced as the empty state) and Gas Town (which
// emits singleton-static, multi-static, and per-rig pool groups).

import { Boxes, GitGraph, Network, Users } from 'lucide-react';
import type { AgentGroup, AgentID, GroupMode } from '@/types/core.gen';
import { cn } from '@/lib/utils';

export interface AgentGroupBoardProps {
  group: AgentGroup;
  // resolved is the optional ResolveGroupMembers payload. The page
  // fetches this lazily for pool / graph modes; static groups already
  // have membership baked into Members.static.
  resolved?: AgentID[];
  // poolCheck — last-seen pool check sample. Populated from the
  // adaptor's check endpoint when the page asks for it. Optional;
  // when absent the pool view shows "—" for actual.
  poolCheck?: PoolCheckSample;
  // role badge from group.extension.role (Gas Town carries this on
  // every group). Rendered as a stable tag colored from the role
  // taxonomy rather than parsing display_name.
  className?: string;
}

// PoolCheckSample is the shape the SPA expects from a pool group's
// check endpoint. Adaptor-emitted; the SPA does not parse it. Shown as
// a single "now" sample plus an optional small log of recent checks.
export interface PoolCheckSample {
  desired?: number;
  actual?: number;
  checked_at?: string;
  log?: PoolCheckLogEntry[];
}

export interface PoolCheckLogEntry {
  at: string;
  desired?: number;
  actual?: number;
  note?: string;
}

export function AgentGroupBoard({
  group,
  resolved,
  poolCheck,
  className,
}: AgentGroupBoardProps) {
  return (
    <section
      data-testid="agent-group-board"
      data-group-mode={group.mode}
      data-group-id={group.id}
      className={cn(
        'flex flex-col rounded-lg border border-neutral-200 bg-white dark:border-neutral-800 dark:bg-neutral-900',
        className
      )}
    >
      <Header group={group} />
      <div className="flex-1 p-4">
        <ModeBody group={group} resolved={resolved} poolCheck={poolCheck} />
      </div>
    </section>
  );
}

// Header is mode-agnostic chrome: name, role tag (from the Gas-Town
// extension), repository chips, and a mode pill.
function Header({ group }: { group: AgentGroup }) {
  const role =
    typeof group.extension?.role === 'string'
      ? (group.extension.role as string)
      : null;
  return (
    <header className="flex items-center justify-between gap-3 border-b border-neutral-200 px-4 py-3 dark:border-neutral-800">
      <div className="flex min-w-0 items-center gap-2">
        <ModeIcon mode={group.mode} />
        <h2 className="truncate text-sm font-semibold tracking-tight">
          {group.display_name || group.id}
        </h2>
        {role && (
          <span
            data-testid="agent-group-role"
            className="rounded bg-neutral-100 px-1.5 py-0.5 font-mono text-[10px] uppercase tracking-wider text-neutral-600 dark:bg-neutral-800 dark:text-neutral-400"
          >
            {role}
          </span>
        )}
      </div>
      <div className="flex items-center gap-1.5">
        {(group.repository ?? []).slice(0, 4).map((repo) => (
          <span
            key={repo}
            className="rounded bg-neutral-50 px-1.5 py-0.5 font-mono text-[10px] text-neutral-500 ring-1 ring-neutral-200 dark:bg-neutral-950 dark:text-neutral-400 dark:ring-neutral-800"
          >
            {repo}
          </span>
        ))}
        <ModePill mode={group.mode} />
      </div>
    </header>
  );
}

function ModeIcon({ mode }: { mode: GroupMode }) {
  if (mode === 'static') return <Users className="h-4 w-4 text-neutral-500" aria-hidden />;
  if (mode === 'pool') return <Boxes className="h-4 w-4 text-neutral-500" aria-hidden />;
  return <GitGraph className="h-4 w-4 text-neutral-500" aria-hidden />;
}

function ModePill({ mode }: { mode: GroupMode }) {
  return (
    <span
      data-testid="agent-group-mode-pill"
      className={cn(
        'rounded-full px-2 py-0.5 text-[10px] font-medium uppercase tracking-wider',
        mode === 'static' &&
          'bg-emerald-100 text-emerald-700 dark:bg-emerald-950 dark:text-emerald-300',
        mode === 'pool' &&
          'bg-amber-100 text-amber-700 dark:bg-amber-950 dark:text-amber-300',
        mode === 'graph' &&
          'bg-violet-100 text-violet-700 dark:bg-violet-950 dark:text-violet-300'
      )}
    >
      {mode}
    </span>
  );
}

function ModeBody({
  group,
  resolved,
  poolCheck,
}: {
  group: AgentGroup;
  resolved?: AgentID[];
  poolCheck?: PoolCheckSample;
}) {
  switch (group.mode) {
    case 'static':
      return <StaticBody group={group} />;
    case 'pool':
      return <PoolBody group={group} resolved={resolved} poolCheck={poolCheck} />;
    case 'graph':
      return <GraphBody group={group} resolved={resolved} />;
    default: {
      // Defensive: future mode declared by an adaptor we don't know.
      // Render the opaque box pattern so the page still functions.
      const _exhaustive: never = group.mode;
      void _exhaustive;
      return <GraphBody group={group} resolved={resolved} />;
    }
  }
}

// --- static -----------------------------------------------------------

// Fixed-member tiles. Membership is carried inline in members.static —
// no network round-trip needed. Singleton groups (Gas Town's Mayor /
// Deacon) render as a single tile; multi-member groups (Witnesses,
// Refineries) tile in a responsive grid.
function StaticBody({ group }: { group: AgentGroup }) {
  const members = group.members?.static ?? [];
  if (members.length === 0) {
    return (
      <p
        data-testid="agent-group-static-empty"
        className="text-sm text-neutral-500 dark:text-neutral-400"
      >
        No members declared.
      </p>
    );
  }
  return (
    <ul
      data-testid="agent-group-static-tiles"
      className="grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-3"
    >
      {members.map((id) => (
        <li
          key={id}
          data-testid="agent-group-static-tile"
          className="rounded-md border border-neutral-200 bg-neutral-50 px-3 py-2 dark:border-neutral-800 dark:bg-neutral-950"
        >
          <div className="flex items-center gap-2">
            <Users className="h-3.5 w-3.5 text-neutral-400" aria-hidden />
            <span className="truncate font-mono text-xs text-neutral-700 dark:text-neutral-300">
              {id}
            </span>
          </div>
        </li>
      ))}
    </ul>
  );
}

// --- pool -------------------------------------------------------------

// Desired-vs-actual gauge plus a short check log. Membership is elastic
// — pool_check_url names the adaptor endpoint (Gas Town: gt://polecat/list?rig=...),
// pool_min / pool_max bracket the desired count.
function PoolBody({
  group,
  resolved,
  poolCheck,
}: {
  group: AgentGroup;
  resolved?: AgentID[];
  poolCheck?: PoolCheckSample;
}) {
  const desired =
    poolCheck?.desired ??
    group.members.pool_max ??
    group.members.pool_min ??
    null;
  const actual = poolCheck?.actual ?? resolved?.length ?? null;
  return (
    <div className="flex flex-col gap-3" data-testid="agent-group-pool">
      <Gauge desired={desired} actual={actual} />
      <div className="grid grid-cols-2 gap-3 text-xs">
        <Field label="Min" value={fmtNum(group.members.pool_min)} />
        <Field label="Max" value={fmtNum(group.members.pool_max)} />
        <Field
          label="Check endpoint"
          value={group.members.pool_check_url ?? '—'}
          mono
        />
        <Field
          label="Last checked"
          value={poolCheck?.checked_at ?? '—'}
        />
      </div>
      {poolCheck?.log && poolCheck.log.length > 0 && (
        <div data-testid="agent-group-pool-log" className="mt-1">
          <div className="mb-1 text-[10px] uppercase tracking-wider text-neutral-500">
            Check log
          </div>
          <ol className="space-y-0.5 font-mono text-[11px]">
            {poolCheck.log.map((entry, i) => (
              <li
                key={`${entry.at}-${i}`}
                className="flex items-center gap-2 text-neutral-600 dark:text-neutral-400"
              >
                <span className="tabular-nums">{entry.at}</span>
                <span className="text-neutral-400">·</span>
                <span>
                  {fmtNum(entry.actual)}/{fmtNum(entry.desired)}
                </span>
                {entry.note && (
                  <>
                    <span className="text-neutral-400">·</span>
                    <span className="truncate">{entry.note}</span>
                  </>
                )}
              </li>
            ))}
          </ol>
        </div>
      )}
    </div>
  );
}

function Gauge({
  desired,
  actual,
}: {
  desired: number | null | undefined;
  actual: number | null | undefined;
}) {
  const d = typeof desired === 'number' ? desired : null;
  const a = typeof actual === 'number' ? actual : null;
  const pct =
    d !== null && d > 0 && a !== null ? Math.min(100, Math.round((a / d) * 100)) : 0;
  const ok = d !== null && a !== null && a >= d;
  return (
    <div data-testid="agent-group-pool-gauge" className="flex flex-col gap-1">
      <div className="flex items-baseline justify-between">
        <span className="text-xs uppercase tracking-wider text-neutral-500">
          Desired vs actual
        </span>
        <span className="font-mono text-sm tabular-nums">
          <span data-testid="pool-gauge-actual">{fmtNum(a)}</span>
          <span className="text-neutral-400"> / </span>
          <span data-testid="pool-gauge-desired">{fmtNum(d)}</span>
        </span>
      </div>
      <div className="h-2 w-full overflow-hidden rounded-full bg-neutral-100 dark:bg-neutral-800">
        <div
          data-testid="pool-gauge-fill"
          className={cn(
            'h-full transition-[width] duration-300',
            ok ? 'bg-emerald-500' : 'bg-amber-500'
          )}
          style={{ width: `${pct}%` }}
          aria-valuenow={a ?? 0}
          aria-valuemin={0}
          aria-valuemax={d ?? 0}
          role="progressbar"
        />
      </div>
    </div>
  );
}

// --- graph ------------------------------------------------------------

// Opaque-box rendering. The graph topology is the adaptor's private
// concern (LangGraph nodes, CrewAI flows, etc.) — the SPA does not
// model it. We render the topology id, the resolved member list, and
// the Network glyph as a placeholder. Adaptors that want richer graph
// rendering ship a widget under web/src/extensions/<adaptor>/ and
// register it via the existing extension surface.
function GraphBody({
  group,
  resolved,
}: {
  group: AgentGroup;
  resolved?: AgentID[];
}) {
  const members =
    resolved && resolved.length > 0
      ? resolved
      : group.members.graph_resolved ?? [];
  return (
    <div className="flex flex-col gap-3" data-testid="agent-group-graph">
      <div
        data-testid="agent-group-graph-box"
        className="flex items-center justify-center rounded-md border border-dashed border-neutral-300 bg-neutral-50 py-8 dark:border-neutral-700 dark:bg-neutral-950"
      >
        <div className="flex flex-col items-center gap-2 text-center">
          <Network className="h-6 w-6 text-neutral-400" aria-hidden />
          <div className="font-mono text-xs text-neutral-600 dark:text-neutral-400">
            {group.members.graph_topology_id || 'opaque topology'}
          </div>
          <div className="text-[10px] uppercase tracking-wider text-neutral-400">
            adaptor-owned graph
          </div>
        </div>
      </div>
      <div>
        <div className="mb-1 text-[10px] uppercase tracking-wider text-neutral-500">
          Resolved members ({members.length})
        </div>
        {members.length === 0 ? (
          <p
            data-testid="agent-group-graph-empty"
            className="text-sm text-neutral-500 dark:text-neutral-400"
          >
            No resolved members.
          </p>
        ) : (
          <ul className="space-y-1 font-mono text-xs">
            {members.map((id) => (
              <li
                key={id}
                data-testid="agent-group-graph-member"
                className="truncate text-neutral-700 dark:text-neutral-300"
              >
                {id}
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}

// --- shared -----------------------------------------------------------

function Field({
  label,
  value,
  mono = false,
}: {
  label: string;
  value: string;
  mono?: boolean;
}) {
  return (
    <div>
      <div className="text-[10px] uppercase tracking-wider text-neutral-500">{label}</div>
      <div
        className={cn(
          'truncate text-neutral-700 dark:text-neutral-300',
          mono && 'font-mono'
        )}
      >
        {value}
      </div>
    </div>
  );
}

function fmtNum(n: number | null | undefined): string {
  return typeof n === 'number' ? String(n) : '—';
}
