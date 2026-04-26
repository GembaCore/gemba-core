// Gemba walk types (gm-uipx.2). ui-spec §5.4.

// AgendaSource maps to the icon legend in §5.4: where each agenda
// item came from. The walk's content model is "things that need a
// decision" — escalations, HITL questions, filed/closed beads,
// and operator-added asks.
export type AgendaSource =
  | 'escalation'
  | 'hitl'
  | 'filed_bead'
  | 'closed_bead'
  | 'user_added';

// Decision matches the R/M/X/D hotkey set in ui-spec §6.6.
//   Ratify (R) — accept the PM's framing as-is
//   Modify (M) — accept with adjustments (free-form note)
//   Reject (X) — decline; surfaces a follow-up question
//   Defer  (D) — push to a later walk
export type Decision = 'ratify' | 'modify' | 'reject' | 'defer';

// Lane is the agenda kanban-mini's column set. "active" is the item
// currently being discussed (one at a time); "queued" is up next;
// "decided" is done; "deferred" is parked for a later walk.
export type Lane = 'queued' | 'active' | 'decided' | 'deferred';

export interface AgendaItem {
  id: string;
  source: AgendaSource;
  title: string;
  // urgency surfaces in the card as a small badge ("urgent" /
  // "today" / "soon" / "later"). The walk doesn't enforce ordering
  // by urgency — the operator drags to reorder.
  urgency?: 'urgent' | 'today' | 'soon' | 'later';
  lane: Lane;
  // decision is set when lane === 'decided'. Carries the decision
  // verb plus the optional Modify note.
  decision?: { kind: Decision; note?: string };
  // refId points at whatever produced the item — bead id, escalation
  // id, etc. Walk surface treats it opaquely; downstream readers
  // resolve back to the originating record.
  refId?: string;
}

export interface WalkSnapshot {
  active: boolean;
  agenda: AgendaItem[];
  // activeIndex is an index into agenda for the currently-discussed
  // item. -1 when no item is active (e.g. before the first lane=='active'
  // promotion or after every item is decided).
  activeIndex: number;
}
