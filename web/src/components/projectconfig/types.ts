// Project config types (gm-uipx.8).

// SectionID matches the URL anchor in /project/config#<id>. Order
// here matches ui-spec §5.16 — the StickyNav renders in this order
// and the IntersectionObserver tracks the same set.
export type SectionID =
  | 'values'
  | 'guardrails'
  | 'goals'
  | 'personas'
  | 'adaptors'
  | 'packs'
  | 'integrations'
  | 'mode'
  | 'subscriptions'
  | 'workspace-repos'
  | 'advanced';

export interface SectionDef {
  id: SectionID;
  label: string;
}

export const SECTIONS: SectionDef[] = [
  { id: 'values', label: 'Values' },
  { id: 'guardrails', label: 'Guardrails' },
  { id: 'goals', label: 'Goals' },
  { id: 'personas', label: 'Personas' },
  { id: 'adaptors', label: 'Adaptors' },
  { id: 'packs', label: 'Packs' },
  { id: 'integrations', label: 'Integrations' },
  { id: 'mode', label: 'Mode' },
  { id: 'subscriptions', label: 'Subscriptions' },
  { id: 'workspace-repos', label: 'Workspace repos' },
  { id: 'advanced', label: 'Advanced' },
];

// ProjectValue is a single ranked statement from the Values editor.
// Persisted to localStorage at v1; a future bead lifts the model to
// `.gemba/workspace.toml` so values survive across operators.
export interface ProjectValue {
  id: string;
  // statement is the freeform "we value X" / "we prefer Y" text.
  // Multi-line allowed; the modal renders a textarea.
  statement: string;
  // rank is the operator-set priority: 1 = highest. Two values may
  // not share a rank — the editor's reorder controls keep ranks
  // sequential and unique.
  rank: number;
}
