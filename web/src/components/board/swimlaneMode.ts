// Swimlane partition modes for the board's Epic view (ui-spec §4.4).
// Lives in its own module so EpicView.tsx can stay component-only and
// the BoardPage can import the type + helpers without pulling the
// whole view in.
//
// Modes:
//   by-parent-epic — default; group by root epic in the parent_child graph
//   by-label       — one swimlane per distinct label (union semantics)
//   none           — single synthetic swimlane; layout reads as a flat board
//
// `by-parallel-group` is in the spec but deferred until parallel
// groups exist as a backend concept.

export type SwimlaneMode = 'by-parent-epic' | 'by-label' | 'none';

export const SWIMLANE_MODES: readonly SwimlaneMode[] = [
  'by-parent-epic',
  'by-label',
  'none',
] as const;

export const DEFAULT_SWIMLANE_MODE: SwimlaneMode = 'by-parent-epic';

// parseSwimlaneMode tolerates anything (including missing) on the wire
// and falls back to the default rather than throwing — keeps an old
// SPA build from breaking when the URL carries a value the registry
// doesn't know yet.
export function parseSwimlaneMode(raw: string | null | undefined): SwimlaneMode {
  if (raw && (SWIMLANE_MODES as readonly string[]).includes(raw)) {
    return raw as SwimlaneMode;
  }
  return DEFAULT_SWIMLANE_MODE;
}
