// InsightsPersonasPage (gm-twp2). The first concrete /insights/*
// surface — a one-row-per-consult list ranking persona activity so an
// operator can see which personas have been consulted, how recently,
// and what the spend looks like.
//
// Data flow:
//   /api/consults   → recent consults (in-memory, dispatcher.List)
//   /api/skills     → persona-aware skill catalog (just for the
//                     "personas without consults yet" empty-state
//                     hint about which Skills exist)
//
// Sort: in-flight first (newest started first), then completed
// newest-first by ended_at. Operator attention is on live consults.
//
// Keeps the bead's spec narrow: "minimal — persona name + last-
// consult timestamp + total tokens". Filtering, drill-down, and
// auto-refresh-via-SSE land in follow-up beads when we have feedback.

import { useMemo } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Sparkles, Bot, AlertTriangle } from 'lucide-react';
import { listConsults, listSkills, type ConsultSummary } from '@/api/consults';
import { ApiError } from '@/api/client';
import { cn } from '@/lib/utils';

// Aggregated row: one per persona id seen in the consult list. Empty
// catalog still renders an "(no consults yet)" banner with the
// registered skills below so the operator knows the dispatcher is up
// even before the first consult fires.
interface PersonaRow {
  personaId: string;
  consultCount: number;
  lastStartedAt: string;
  lastEndedAt: string | null;
  totalInputTokens: number;
  totalOutputTokens: number;
  inflightCount: number;
  failedCount: number;
}

export function InsightsPersonasPage() {
  const consultsQ = useQuery({
    queryKey: ['consults'],
    queryFn: listConsults,
    // Five-second polling matches /api/sessions until the SSE feed
    // for skill.output_emitted lands as a SPA push channel.
    refetchInterval: 5_000,
  });
  const skillsQ = useQuery({
    queryKey: ['skills'],
    queryFn: listSkills,
    // Skills change at boot only; one fetch is enough.
    staleTime: Infinity,
  });

  const rows = useMemo(() => aggregate(consultsQ.data ?? []), [consultsQ.data]);
  const skills = skillsQ.data ?? [];

  // Both queries 503 cleanly when the dispatcher isn't attached
  // (cmd/gemba serve installs it but tests / pre-config-load Routers
  // return 503). The shared error rendering surfaces "persona
  // dispatcher not configured" verbatim so the operator sees what's
  // missing without inspecting the server logs.
  const dispatcherUnavailable = isUnavailable(consultsQ.error) || isUnavailable(skillsQ.error);

  return (
    <div className="flex h-full flex-col" data-testid="insights-personas">
      <header className="flex items-center justify-between border-b border-neutral-200 px-8 py-4 dark:border-neutral-800">
        <div>
          <h1 className="flex items-center gap-2 text-xl font-semibold tracking-tight">
            <Sparkles className="h-5 w-5" aria-hidden />
            Personas
          </h1>
          <p className="text-xs text-neutral-500 dark:text-neutral-400">
            Recent persona consults — one row per persona, newest activity first.
          </p>
        </div>
      </header>

      {dispatcherUnavailable ? (
        <DispatcherUnavailable />
      ) : (
        <div className="min-h-0 flex-1 overflow-auto px-8 py-4">
          {consultsQ.isLoading ? (
            <div className="text-sm text-neutral-500" data-testid="insights-personas-loading">
              Loading…
            </div>
          ) : consultsQ.error ? (
            <ErrorState err={consultsQ.error} />
          ) : rows.length === 0 ? (
            <EmptyState skillCount={skills.length} />
          ) : (
            <PersonaTable rows={rows} />
          )}
        </div>
      )}
    </div>
  );
}

// aggregate folds a flat consult list into per-persona rows. Sort is
// inflight-first, then last-activity descending — see header comment.
function aggregate(consults: ConsultSummary[]): PersonaRow[] {
  const byPersona = new Map<string, PersonaRow>();
  for (const c of consults) {
    const r = byPersona.get(c.persona_id) ?? {
      personaId: c.persona_id,
      consultCount: 0,
      lastStartedAt: '',
      lastEndedAt: null,
      totalInputTokens: 0,
      totalOutputTokens: 0,
      inflightCount: 0,
      failedCount: 0,
    };
    r.consultCount++;
    if (c.started_at > r.lastStartedAt) r.lastStartedAt = c.started_at;
    if (c.ended_at && (r.lastEndedAt === null || c.ended_at > r.lastEndedAt)) {
      r.lastEndedAt = c.ended_at;
    }
    if (c.status === 'running') r.inflightCount++;
    if (c.status === 'failed') r.failedCount++;
    // Token totals are populated post-Finish; running consults
    // contribute zero, which is the right answer.
    byPersona.set(c.persona_id, r);
  }
  return [...byPersona.values()].sort((a, b) => {
    if (a.inflightCount !== b.inflightCount) return b.inflightCount - a.inflightCount;
    return b.lastStartedAt.localeCompare(a.lastStartedAt);
  });
}

interface PersonaTableProps {
  rows: PersonaRow[];
}

function PersonaTable({ rows }: PersonaTableProps) {
  return (
    <table
      className="w-full border-collapse text-sm"
      data-testid="insights-personas-table"
    >
      <thead>
        <tr className="border-b border-neutral-200 text-left text-xs uppercase tracking-wide text-neutral-500 dark:border-neutral-800">
          <th className="px-3 py-2 font-medium">Persona</th>
          <th className="px-3 py-2 font-medium">Consults</th>
          <th className="px-3 py-2 font-medium">Last activity</th>
          <th className="px-3 py-2 font-medium">Status</th>
          <th className="px-3 py-2 text-right font-medium">Tokens (in / out)</th>
        </tr>
      </thead>
      <tbody>
        {rows.map((r) => (
          <tr
            key={r.personaId}
            data-testid={`insights-persona-row-${r.personaId}`}
            className="border-b border-neutral-100 dark:border-neutral-900"
          >
            <td className="px-3 py-2 font-mono text-xs">
              <div className="flex items-center gap-1.5">
                <Bot className="h-3.5 w-3.5 text-neutral-500" aria-hidden />
                {r.personaId}
              </div>
            </td>
            <td className="px-3 py-2 tabular-nums">{r.consultCount}</td>
            <td className="px-3 py-2 text-neutral-600 dark:text-neutral-400">
              {formatRelativeOrAbsolute(r.lastStartedAt)}
            </td>
            <td className="px-3 py-2">
              <StatusBadges row={r} />
            </td>
            <td className="px-3 py-2 text-right font-mono tabular-nums">
              {r.totalInputTokens.toLocaleString()} / {r.totalOutputTokens.toLocaleString()}
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function StatusBadges({ row }: { row: PersonaRow }) {
  return (
    <div className="flex items-center gap-1.5 text-xs">
      {row.inflightCount > 0 && (
        <span
          className="rounded bg-sky-100 px-1.5 py-0.5 text-sky-700 dark:bg-sky-900/40 dark:text-sky-300"
          data-testid="insights-personas-inflight"
        >
          {row.inflightCount} running
        </span>
      )}
      {row.failedCount > 0 && (
        <span
          className="flex items-center gap-1 rounded bg-rose-100 px-1.5 py-0.5 text-rose-700 dark:bg-rose-900/40 dark:text-rose-300"
          data-testid="insights-personas-failed"
        >
          <AlertTriangle className="h-3 w-3" aria-hidden />
          {row.failedCount} failed
        </span>
      )}
      {row.inflightCount === 0 && row.failedCount === 0 && (
        <span className="text-neutral-400">—</span>
      )}
    </div>
  );
}

function EmptyState({ skillCount }: { skillCount: number }) {
  return (
    <div
      className="rounded-md border border-dashed border-neutral-300 p-8 text-center dark:border-neutral-700"
      data-testid="insights-personas-empty"
    >
      <p className="text-sm text-neutral-700 dark:text-neutral-200">
        No consults have been started yet.
      </p>
      <p className="mt-2 text-xs text-neutral-500 dark:text-neutral-400">
        {skillCount > 0
          ? `${skillCount} skill${skillCount === 1 ? '' : 's'} registered. Start a consult via POST /api/consults or the Plan view.`
          : 'No skills registered either. Check the gemba serve startup logs for skill registration warnings.'}
      </p>
    </div>
  );
}

function ErrorState({ err }: { err: unknown }) {
  return (
    <div
      className={cn(
        'rounded-md border border-rose-200 bg-rose-50 p-4 text-sm text-rose-700',
        'dark:border-rose-900/60 dark:bg-rose-950/30 dark:text-rose-300',
      )}
      data-testid="insights-personas-error"
    >
      Could not load consults: {(err as Error).message}
    </div>
  );
}

function DispatcherUnavailable() {
  return (
    <div
      className="m-8 rounded-md border border-amber-300 bg-amber-50 p-6 text-sm text-amber-800 dark:border-amber-900/60 dark:bg-amber-950/30 dark:text-amber-200"
      data-testid="insights-personas-503"
    >
      <p className="font-medium">Persona dispatcher not configured.</p>
      <p className="mt-1 text-xs">
        The server returned 503 from <code>/api/consults</code>. cmd/gemba serve
        installs the dispatcher when an OrchestrationPlane is bound; check the
        startup logs.
      </p>
    </div>
  );
}

// isUnavailable narrows an ApiError to the 503 case the lazy-attach
// pattern produces. Gracefully tolerates non-ApiError values
// (network failures still render via ErrorState in the row body).
function isUnavailable(err: unknown): boolean {
  if (!err) return false;
  if (err instanceof ApiError) {
    return err.status === 503;
  }
  return false;
}

// formatRelativeOrAbsolute returns "just now" / "5m ago" for recent
// timestamps; falls back to ISO-date for anything older than 24h so
// the operator sees a stable label they can reason about.
function formatRelativeOrAbsolute(iso: string): string {
  if (!iso) return '—';
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return iso;
  const diffMs = Date.now() - t;
  if (diffMs < 60_000) return 'just now';
  if (diffMs < 60 * 60_000) {
    const m = Math.round(diffMs / 60_000);
    return `${m}m ago`;
  }
  if (diffMs < 24 * 60 * 60_000) {
    const h = Math.round(diffMs / (60 * 60_000));
    return `${h}h ago`;
  }
  return iso.slice(0, 10); // YYYY-MM-DD
}
