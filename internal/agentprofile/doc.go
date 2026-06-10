// Package agentprofile implements the persistent agent profile
// (gm-v5z2.2, work-planning.md §4 Layer 1.2).
//
// Sister to the session profile in internal/planner. Same shape
// (concepts {tag:weight} + files {path:weight}), keyed on
// AgentRef.ID, but with two key differences:
//
//   - Survives `gt handoff`. A new session inherits its agent's
//     profile as a warm starting point, then accumulates session-
//     specific weight on top.
//
//   - Different question. Session profile answers "what is this
//     session warm on right now?" Agent profile answers "what is
//     this agent good at over weeks?" An agent who has been deep
//     in the planner across multiple sessions stays primed on
//     planner concepts even on its first bead of a fresh session.
//
// Decay: per-day half-life (default 14d), distinct from the
// session profile's per-bead-event half-life (default 5).
//
// Writes: the retrospective hook (§7) writes both profiles on
// bead completion. Session row gets the bead's actual concepts/
// files at full weight; agent row gets the same contribution
// scaled by 1 / (lifetime bead count) so a single bead doesn't
// dominate a long-running agent's profile.
//
// Reads: §4 Layer 3.2 Affinity reads BOTH profiles, weighted
// (default 0.7 session + 0.3 agent — tunable). The mix lives in
// the scoring layer; this package only owns persistence + decay.
package agentprofile
