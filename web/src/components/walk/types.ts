// Gemba walk types (gm-i65). Re-exports the backend-aligned shapes
// from @/api/walks so the walk components and the API client share a
// single source of truth for the wire format.
//
// Earlier (gm-uipx.2) this file held a localStorage-backed mock
// shape. The backend gm-rfy / gm-wgv now own the canonical types;
// re-exporting here keeps the existing component imports stable.

export type {
  AgendaItem,
  AgendaItemStatus,
  AgendaSource,
  AgendaSourceKind,
  Citation,
  Cost,
  DecisionKind,
  Speaker,
  Walk,
  WalkDecision,
  WalkStatus,
  WalkTurn,
} from '@/api/walks';

// Decision is the legacy frontend enum (matches DecisionKind without
// 'handoff' — kept as an alias because the R/M/X/D hotkey set in
// ui-spec §6.6 uses these four).
export type Decision = 'ratify' | 'modify' | 'reject' | 'defer';

// Lane is the agenda kanban-mini's column set. Maps 1-1 onto the
// backend AgendaItemStatus (minus 'dismissed' which the UI rolls
// into 'decided' for swimlane purposes).
export type Lane = 'queued' | 'active' | 'decided' | 'deferred';
