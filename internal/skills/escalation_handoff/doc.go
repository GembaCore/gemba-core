// Package escalation_handoff implements the dispatcher skill behind
// the EscalationsPage Hand-off action (gm-e11.8.7). It is the
// thinnest possible LLM-driven skill — its job is to route an
// operator's hand-off into a fresh consult against the chosen
// persona, scoped to the originating escalation.
//
// The skill does NOT resolve the escalation. The escalation card
// stays open in the inbox until the operator separately hits Resolve
// (or the agent satisfies the request through some other channel).
// The hand-off is purely "give me a second opinion from <persona> on
// this stuck escalation".
//
// Input shape:
//
//	{
//	  "escalation_id":   "esc-...",
//	  "title":           "...",
//	  "prompt":          "...",
//	  "assignment_id":   "tmux:gm-...:42",  // optional
//	  "target_persona":  "project-manager"  // informational; dispatcher
//	                                        // already resolves persona
//	                                        // via createConsultRequest.
//	}
//
// Output shape: a JSONL stream with exactly one summary line
// describing the hand-off. The persona's actual reply lands as the
// consult's narrative_lines via Receive; the summary line is the
// structured artifact that downstream code can apply or reference.
//
// Skill is registered with persona.SkillRegistry alongside epic_order
// in internal/cli/serve.go. Personas that should be hand-off targets
// MUST list `escalation_handoff` in their TOML `skills` block;
// PersonaCanInvoke gates every consult and rejects one whose persona
// hasn't opted in.
package escalation_handoff
