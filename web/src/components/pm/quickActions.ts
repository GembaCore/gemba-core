// View-aware PM-panel quick actions (gm-96l).
//
// The PM panel surfaces a small row of buttons at the top of the
// expanded panel that map to whatever skills feel native to the
// route the operator is on. The mapping is intentionally tiny:
//
//   /board[/*]      → plan-related verbs from the PM persona
//                     (.gemba/personas/project-manager.toml). The
//                     persona ships epic_order today; the labels
//                     here advertise that skill alongside the staged
//                     verbs the spec asks for ("Recommend order",
//                     "What remains", "Trim to budget").
//   /escalations    → 'Triage'
//   /walk           → no extra surface — the walk takeover already
//                     owns the panel body, so the quick-action row
//                     is empty and the walk surface stretches.
//   *               → a generic 'Ask PM' button only.
//
// Quick actions don't dispatch real consults yet (gm-twp2.* will
// wire that up when the SPA grows a /consults POST surface that
// works without the operator naming a skill). For now selecting
// one stages the prompt input with the action label so the
// operator can refine before submit. The handler can be swapped
// for a real CreateConsult call without touching this catalog.

import type { PersonaKind } from './types';

export interface QuickAction {
  // Stable id used as a data-testid suffix and as the key for
  // localStorage-backed last-action analytics (out of scope here).
  id: string;
  // label is the visible button text. ALREADY variety-aware — i.e.
  // the caller decides whether to render 'Ask PM' or 'Consult PM'
  // based on the active persona's variety. The label here is the
  // *verb*; the variety prefix is layered on at render time.
  label: string;
  // prompt is the seed text dropped into the panel's input box on
  // click. Empty string is fine — some actions are pure "open a
  // fresh prompt with the label as a placeholder" affordances.
  prompt: string;
}

// QuickActionContext drives the variety-aware label prefix. The PM
// panel reads this from the active persona before rendering each
// button so 'Ask' vs 'Consult' lines up with who-suggests-vs-acts.
export interface QuickActionContext {
  varieties: Record<string, PersonaKind>; // persona.id → kind
  activePersonaId: string;
  activePersonaName: string;
}

// quickActionsForPath returns the verb list for the operator's
// current route. The matcher is intentionally simple (longest-
// prefix wins via order) so adding a route is a one-line tweak.
export function quickActionsForPath(pathname: string): QuickAction[] {
  if (pathname.startsWith('/board')) {
    // Board routes share the same plan-staging verbs. The first
    // entry mirrors the PM persona's 'epic_order' skill so an
    // operator one-click away from a recommendation gets there.
    return [
      { id: 'recommend-order', label: 'Recommend order', prompt: 'Recommend an order for the candidate epics.' },
      { id: 'what-remains', label: 'What remains', prompt: 'What work remains in scope for the active sprint?' },
      { id: 'trim-to-budget', label: 'Trim to budget', prompt: 'Trim the candidate set to the current sprint budget.' },
    ];
  }
  if (pathname.startsWith('/escalations')) {
    return [
      { id: 'triage', label: 'Triage', prompt: 'Triage the open escalations and recommend an order to address them.' },
    ];
  }
  if (pathname.startsWith('/walk')) {
    // Walk takes over the panel body — no quick-action row needed.
    return [];
  }
  // Generic: a single "open the input" affordance. The label is
  // intentionally short so the same row can render flush across
  // every other route without prescribing a verb.
  return [{ id: 'ask-pm', label: 'Ask PM', prompt: '' }];
}

// labelPrefixForVariety folds the Coach/Manager split into the
// 'Ask'/'Consult' verb prefix. Coach varieties ('coach') are
// advisory — they SUGGEST. Manager varieties ('manager',
// 'architect') are accountable for the call — they CONSULT.
//
// The Variety enum on the backend (internal/core/persona/types.go)
// has more states than the PersonaKind type carries here; until
// that surfaces over /api/personas the two known production
// kinds drive the split, and unknown kinds fall back to 'Ask'
// (the lower-stakes prefix) so a new variety doesn't suddenly
// imply Manager-grade authority.
export function labelPrefixForVariety(kind: PersonaKind | undefined): 'Ask' | 'Consult' {
  if (kind === 'manager' || kind === 'architect') return 'Consult';
  return 'Ask';
}
