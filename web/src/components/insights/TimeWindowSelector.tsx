// TimeWindowSelector (gm-e12.17.1).
//
// Top-right window picker for the Insights panel. Default 14d, options
// 24h / 7d / 14d / 30d. "Custom" is reserved (renders disabled) until
// the date-picker UX lands — the bead spec lists it as a future option
// but doesn't require it for the MVP.
//
// Renders as a segmented button group rather than a <select> so the
// page reads as a dashboard control, not a form input. Each preset
// maps to a (durationMs, bucket) tuple chosen so that the chart has
// roughly 50–200 points at any window — see WINDOW_PRESETS.

import { cn } from '@/lib/utils';

export type TimeWindowKey = '24h' | '7d' | '14d' | '30d';

export interface TimeWindowPreset {
  key: TimeWindowKey;
  label: string;
  // Total length of the window in milliseconds.
  durationMs: number;
  // Backend bucket size (`1h`, `6h`, `1d`, etc). Matches the contract
  // in /api/v1/metrics/series.
  bucket: string;
}

export const WINDOW_PRESETS: readonly TimeWindowPreset[] = [
  { key: '24h', label: '24h', durationMs: 24 * 60 * 60_000, bucket: '15m' },
  { key: '7d', label: '7d', durationMs: 7 * 24 * 60 * 60_000, bucket: '1h' },
  { key: '14d', label: '14d', durationMs: 14 * 24 * 60 * 60_000, bucket: '3h' },
  { key: '30d', label: '30d', durationMs: 30 * 24 * 60 * 60_000, bucket: '6h' },
] as const;

export const DEFAULT_WINDOW: TimeWindowKey = '14d';

export function presetFor(key: TimeWindowKey): TimeWindowPreset {
  const p = WINDOW_PRESETS.find((w) => w.key === key);
  if (!p) {
    throw new Error(`unknown time-window key: ${key}`);
  }
  return p;
}

// computeRange resolves a preset key into (since, until) RFC3339
// strings. `now` is injected so tests can pin the window.
export function computeRange(
  key: TimeWindowKey,
  now: Date = new Date(),
): { since: string; until: string; bucket: string } {
  const preset = presetFor(key);
  const until = new Date(now);
  const since = new Date(until.getTime() - preset.durationMs);
  return {
    since: since.toISOString(),
    until: until.toISOString(),
    bucket: preset.bucket,
  };
}

export interface TimeWindowSelectorProps {
  value: TimeWindowKey;
  onChange: (next: TimeWindowKey) => void;
  className?: string;
}

export function TimeWindowSelector({
  value,
  onChange,
  className,
}: TimeWindowSelectorProps): JSX.Element {
  return (
    <div
      data-testid="insights-window-selector"
      className={cn(
        'inline-flex rounded-md border border-neutral-200 bg-white p-0.5 text-xs dark:border-neutral-800 dark:bg-neutral-900',
        className,
      )}
      role="radiogroup"
      aria-label="Time window"
    >
      {WINDOW_PRESETS.map((preset) => {
        const active = preset.key === value;
        return (
          <button
            key={preset.key}
            type="button"
            role="radio"
            aria-checked={active}
            data-testid={`insights-window-${preset.key}`}
            onClick={() => onChange(preset.key)}
            className={cn(
              'rounded px-2 py-1 font-medium tabular-nums transition-colors',
              active
                ? 'bg-neutral-900 text-white dark:bg-neutral-100 dark:text-neutral-900'
                : 'text-neutral-600 hover:bg-neutral-100 dark:text-neutral-400 dark:hover:bg-neutral-800',
            )}
          >
            {preset.label}
          </button>
        );
      })}
    </div>
  );
}
