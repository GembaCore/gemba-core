// Bootstrap wizard types (gm-uipx.7 — ui-spec §5.15).
//
// The wizard composes four steps with their own URL slugs so back/
// forward navigation works and tests can land on a specific step.

import type { BootstrapPlan, BootstrapProgress, BootstrapSource } from '@/api/bootstrap';

// StepID matches the URL slug under /bootstrap/<id>. The wizard host
// validates every transition against this set so a stray slug falls
// back to Step 1 instead of rendering a blank pane.
export type StepID = 'source' | 'analysis' | 'plan' | 'ratify';

// STEPS is the canonical step ordering. Components iterate this for
// the breadcrumb/progress indicator and back-navigation.
export const STEPS: { id: StepID; label: string }[] = [
  { id: 'source', label: 'Source' },
  { id: 'analysis', label: 'Analysis' },
  { id: 'plan', label: 'Plan' },
  { id: 'ratify', label: 'Ratify' },
];

// SourceTile is the Step-1 tile metadata. Each entry maps to one of
// the four BootstrapSource tokens; the wizard pins the choice into
// state and advances to Step 2.
export interface SourceTile {
  id: BootstrapSource;
  label: string;
  description: string;
}

export const SOURCE_TILES: SourceTile[] = [
  {
    id: 'jira',
    label: 'Jira',
    description: 'Import epics + sprints from a connected Jira board.',
  },
  {
    id: 'beads',
    label: 'Beads',
    description: 'Adopt an existing beads database; reuse priorities + history.',
  },
  {
    id: 'source-code',
    label: 'Source code',
    description: 'Scan a repository tree to derive epic structure and milestones.',
  },
  {
    id: 'fresh',
    label: 'Fresh',
    description: 'Start from scratch — no import; emit a clean project skeleton.',
  },
];

// WizardState is the in-memory state the host threads through each
// step. Nothing here is persisted to the backend until Step 4 — the
// "cancel from any step persists nothing" contract follows from
// keeping every byte in this object until commitBootstrap fires.
export interface WizardState {
  source: BootstrapSource | null;
  analysisId: string | null;
  progress: BootstrapProgress[];
  plan: BootstrapPlan | null;
}

export const INITIAL_WIZARD_STATE: WizardState = {
  source: null,
  analysisId: null,
  progress: [],
  plan: null,
};
