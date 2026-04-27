// JustificationList (gm-v5z2.9). Renders the per-(bead, session)
// Selection.Justification slice — one line per gate fire / score
// component — under each dispatch grid cell.
//
// Spec invariant (work-planning §4 Layer 3): never present the
// scalar without the breakdown. The breakdown lives here.
//
// Pure component. The caller decides density (compact for grid
// cells; full for the side drawer).

import { cn } from '@/lib/utils';

export interface JustificationListProps {
  lines: string[];
  // density controls how much padding / typography weight the
  // list carries. 'compact' → grid cells; 'full' → drawer panes.
  density?: 'compact' | 'full';
  // outcome tints rejected lines red; dispatchable stays neutral.
  outcome?: 'dispatchable' | 'rejected';
  // reason is rendered as a leading badge when present (the gate
  // that fired for a rejected result).
  reason?: string;
  testid?: string;
}

export function JustificationList({
  lines,
  density,
  outcome,
  reason,
  testid,
}: JustificationListProps) {
  const compact = density !== 'full';
  const tone = outcome === 'rejected' ? 'text-rose-700 dark:text-rose-300' : 'text-neutral-600 dark:text-neutral-400';
  const sizeClass = compact ? 'text-[10px] leading-snug' : 'text-xs leading-normal';
  return (
    <div
      data-testid={testid ?? 'justification-list'}
      data-outcome={outcome ?? 'dispatchable'}
      className={cn('flex flex-col gap-0.5 font-mono', sizeClass, tone)}
    >
      {reason ? (
        <span
          data-testid="justification-reason"
          className="inline-flex w-fit items-center rounded bg-rose-100 px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-rose-800 dark:bg-rose-900/40 dark:text-rose-200"
        >
          {reason}
        </span>
      ) : null}
      {lines.length === 0 ? (
        <span className="italic text-neutral-400">(no justification)</span>
      ) : (
        lines.map((line, i) => (
          <span key={`${i}-${line.length}`} data-testid={`justification-line-${i}`}>
            {line}
          </span>
        ))
      )}
    </div>
  );
}
