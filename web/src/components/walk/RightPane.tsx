// Right pane (gm-i65). 280px-wide column for "what's happening
// elsewhere": open escalations the operator hasn't pulled into the
// agenda yet, a budget gauge derived from walk.Cost, and a polecat-
// peek thumbnail placeholder (gm-bms wires the live perspective
// surface).

import { useQuery } from '@tanstack/react-query';
import { AlertTriangle, Coins, Eye, Plus } from 'lucide-react';
import { listEscalations, type EscalationRequest } from '@/api/escalations';
import { cn } from '@/lib/utils';
import { useWalk } from './WalkContext';

export function RightPane(): JSX.Element {
  return (
    <aside
      data-testid="walk-right-pane"
      className="flex h-full w-[280px] shrink-0 flex-col border-l border-neutral-200 bg-neutral-50 dark:border-neutral-800 dark:bg-neutral-950"
    >
      <OpenEscalationsSection />
      <BudgetGauge />
      <PolecatPeekStub />
    </aside>
  );
}

function OpenEscalationsSection(): JSX.Element {
  const walk = useWalk();
  const { data, isLoading } = useQuery({
    queryKey: ['escalations'],
    queryFn: () => listEscalations(),
    refetchInterval: 15_000,
    staleTime: 5_000,
  });

  const escalations = data ?? [];
  // Filter out escalations already pinned to the walk's agenda — we
  // dedupe by Source.Ref since the agenda aggregator stamps the
  // EscalationRequest id there.
  const pinned = new Set(
    walk.agenda
      .filter((i) => i.source.kind === 'escalation' && i.source.ref)
      .map((i) => i.source.ref as string)
  );
  const unpinned = escalations.filter((e) => !pinned.has(e.id));

  return (
    <section
      data-testid="walk-right-escalations"
      className="border-b border-neutral-200 dark:border-neutral-800"
    >
      <header className="flex items-center justify-between px-3 py-2">
        <span className="text-[11px] font-semibold uppercase tracking-wide text-neutral-500">
          Open escalations
        </span>
        <span className="text-[11px] text-neutral-500">{unpinned.length}</span>
      </header>
      <ul className="space-y-1 px-2 pb-2">
        {isLoading ? (
          <li className="px-1 text-[11px] italic text-neutral-400">Loading…</li>
        ) : unpinned.length === 0 ? (
          <li className="px-1 text-[11px] italic text-neutral-400">
            None outside the agenda.
          </li>
        ) : (
          unpinned.slice(0, 8).map((e) => <EscalationRow key={e.id} esc={e} walkID={walk.walk?.id} />)
        )}
      </ul>
    </section>
  );
}

function EscalationRow({
  esc,
  walkID,
}: {
  esc: EscalationRequest;
  walkID: string | undefined;
}): JSX.Element {
  const walk = useWalk();
  const blocking = esc.urgency === 'blocking';
  const onPin = async () => {
    if (!walkID) return;
    await walk.addItem({
      topic: esc.title || esc.prompt,
      source: { kind: 'escalation', ref: esc.id, worker: esc.agent_id },
      priority: blocking ? 450 : 400,
    });
  };
  return (
    <li>
      <div
        data-testid={`walk-right-escalation-${esc.id}`}
        className="flex items-start gap-2 rounded border border-neutral-200 bg-white px-2 py-1.5 text-xs dark:border-neutral-800 dark:bg-neutral-900"
      >
        <AlertTriangle
          className={cn(
            'h-3.5 w-3.5 shrink-0',
            blocking ? 'text-rose-600' : 'text-amber-600'
          )}
          aria-hidden
        />
        <span className="flex-1 truncate" title={esc.title}>
          {esc.title}
        </span>
        <button
          type="button"
          aria-label="Add to walk"
          data-testid={`walk-right-escalation-${esc.id}-add`}
          onClick={() => void onPin()}
          disabled={!walkID}
          className="rounded border border-neutral-300 bg-white px-1 py-0.5 text-neutral-500 hover:bg-neutral-100 disabled:opacity-50 dark:border-neutral-700 dark:bg-neutral-900"
        >
          <Plus className="h-3 w-3" aria-hidden />
        </button>
      </div>
    </li>
  );
}

function BudgetGauge(): JSX.Element {
  const walk = useWalk();
  const cost = walk.walk?.cost ?? { tokens_in: 0, tokens_out: 0, dollars: 0 };
  return (
    <section
      data-testid="walk-right-budget"
      className="border-b border-neutral-200 px-3 py-2 dark:border-neutral-800"
    >
      <header className="mb-1 flex items-center gap-1.5 text-[11px] font-semibold uppercase tracking-wide text-neutral-500">
        <Coins className="h-3.5 w-3.5" aria-hidden />
        <span>Budget</span>
      </header>
      <dl className="space-y-0.5 text-[11px] text-neutral-700 dark:text-neutral-300">
        <div className="flex justify-between">
          <dt className="text-neutral-500">Spent</dt>
          <dd data-testid="walk-right-budget-dollars">${cost.dollars.toFixed(2)}</dd>
        </div>
        <div className="flex justify-between">
          <dt className="text-neutral-500">Tokens (in / out)</dt>
          <dd data-testid="walk-right-budget-tokens">
            {fmtTokens(cost.tokens_in)} / {fmtTokens(cost.tokens_out)}
          </dd>
        </div>
      </dl>
    </section>
  );
}

function fmtTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`;
  return String(n);
}

function PolecatPeekStub(): JSX.Element {
  // Live polecat peek thumbnails land via gm-bms; this stub keeps
  // the right pane visually balanced + reserves the slot.
  return (
    <section
      data-testid="walk-right-peek"
      className="flex-1 px-3 py-2"
    >
      <header className="mb-1 flex items-center gap-1.5 text-[11px] font-semibold uppercase tracking-wide text-neutral-500">
        <Eye className="h-3.5 w-3.5" aria-hidden />
        <span>Polecats</span>
      </header>
      <p className="text-[11px] italic text-neutral-400">
        Live polecat peek thumbnails land with gm-bms.
      </p>
    </section>
  );
}
