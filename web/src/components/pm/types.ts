// PM panel types (gm-uipx.12). Shapes for the bottom-drawer
// conversation surface. Kept narrow because v1 ships the UI only —
// LLM hookup, persona roster fetch, real cost tracking are all
// follow-up beads (gm-uipx and gm-root.14).

export type PersonaKind = 'coach' | 'manager' | 'architect';

export interface Persona {
  id: string;
  name: string;
  kind: PersonaKind;
}

// SuggestedAction — apply-or-dismiss control attached to a turn.
// ui-spec §2.4: "SuggestedActions group at the bottom of the
// relevant turn." When the operator clicks Apply the action moves
// into the turn's executed list and renders with a green check.
export interface SuggestedAction {
  id: string;
  label: string;
  // detail is optional context shown on hover; keep short — one line.
  detail?: string;
}

// ExecutedAction — a SuggestedAction the operator already applied.
// Rendered with a distinct icon + colour badge per ui-spec §2.4.
export interface ExecutedAction {
  id: string;
  label: string;
  appliedAt: string; // ISO timestamp
}

export type TurnSpeaker = 'operator' | Persona;

// Turn — one round of conversation. Speaker is either the operator
// or a persona; the rendering layer styles them alternately
// (terminal-style chat, monospace, alternating-indent per the spec).
export interface Turn {
  id: string;
  speaker: TurnSpeaker;
  text: string;
  // costUSD is the per-turn cost surfaced on hover. Zero is fine
  // for v1 (no real cost tracking yet) — the renderer just shows
  // "$0.000" in the tooltip.
  costUSD?: number;
  // Suggested + Executed lists are independent: a suggestion that
  // gets applied moves to executed, but suggestions can also be
  // dismissed (removed from suggested without going into executed).
  suggested?: SuggestedAction[];
  executed?: ExecutedAction[];
}
