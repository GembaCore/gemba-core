// AgentContextStrip (gm-s47n.6.7). Horizontal scroll of
// AgentContextCard, one per live session. Drop into any surface
// that needs the at-a-glance "what is each session loaded with"
// view — coach mode, a future dispatch dashboard, an operator
// debug page.
//
// Pure component; conflict + affinity overlays are passed in as
// callbacks so the strip itself doesn't know anything about
// dispatch state.

import type { PlannerOperationalContext } from '@/api/planner';
import { AgentContextCard } from './AgentContextCard';

export interface AgentContextStripProps {
  sessions: PlannerOperationalContext[];
  // Optional callback: for each session id, return whether to
  // render the conflict highlight. Defaults to "no conflict."
  inConflict?: (sessionID: string) => boolean;
  // Optional callback: for each session id, return the affinity
  // score for the focused bead (or null when nothing focused).
  // The card surfaces the score inline.
  affinityFor?: (sessionID: string) => { beadId: string; combined: number } | null;
  // testid override — useful when multiple strips coexist on the
  // same page (e.g. paired strips for two different teams).
  testid?: string;
}

export function AgentContextStrip({
  sessions,
  inConflict,
  affinityFor,
  testid,
}: AgentContextStripProps) {
  if (sessions.length === 0) {
    return (
      <div
        data-testid={testid ?? 'agent-context-strip-empty'}
        className="border-b border-neutral-200 p-6 text-sm text-neutral-500 dark:border-neutral-800"
      >
        No live sessions.
      </div>
    );
  }
  return (
    <div className="overflow-x-auto border-b border-neutral-200 p-3 dark:border-neutral-800">
      <div data-testid={testid ?? 'agent-context-strip'} className="flex gap-3">
        {sessions.map((ctx) => {
          const sid = ctx.session?.id ?? '';
          const conflict = inConflict?.(sid) ?? false;
          const aff = affinityFor?.(sid) ?? null;
          return (
            <AgentContextCard
              key={sid || `idx-${Math.random()}`}
              ctx={ctx}
              inConflict={conflict}
              affinityForHovered={aff}
            />
          );
        })}
      </div>
    </div>
  );
}
