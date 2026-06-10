// AgentProfile types + per-day decay (gm-v5z2.2).

package agentprofile

import (
	"math"
	"time"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/planner"
)

// DefaultDecayHalfLifeDays is the per-day half-life of agent
// concept / file weights. 14 days matches the spec rule of thumb
// — a hand-off after a couple weeks of focused work still gives
// the new session a warm starting point, but a long quiet period
// fades the priming back toward neutral.
const DefaultDecayHalfLifeDays = 14.0

// AgentProfile is the per-agent persistent state. Mirrors the
// shape of planner.SessionProfile so the affinity scorer can read
// both with the same map types.
type AgentProfile struct {
	AgentID core.AgentID `json:"agent_id"`

	// Concepts and Files are recency-decayed weight maps. Decay is
	// per-day (vs per-event for the session profile) so an idle
	// agent doesn't accumulate inflated priming just from absence
	// of new bead events.
	Concepts map[planner.ConceptTag]float64 `json:"concepts,omitempty"`
	Files    map[string]float64             `json:"files,omitempty"`

	// LifetimeBeadCount is the number of bead-completion events
	// this agent has seen across every session. Used by
	// RecordCompletion to scale a new bead's contribution by
	// 1 / count so a single bead doesn't dominate the profile.
	LifetimeBeadCount int64 `json:"lifetime_bead_count"`

	// LastActivityAt is the wall-clock of the most recent bead
	// completion. AgeByDays uses (now - last_activity_at) as the
	// decay input; a fresh profile (no completions yet) carries a
	// zero-value LastActivityAt and AgeByDays is a no-op.
	LastActivityAt time.Time `json:"last_activity_at"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// AgeByDays returns a copy of `in` with every weight multiplied
// by 0.5 ^ (days / halfLifeDays). Pure: input is not mutated.
//
// halfLifeDays <= 0 falls back to DefaultDecayHalfLifeDays.
// days <= 0 is a no-op (returns a copy with weights unchanged) so
// the same-day re-read path stays free of arithmetic noise.
//
// nil `in` returns nil so callers can chain without nil-checking.
func AgeByDays(in map[planner.ConceptTag]float64, days, halfLifeDays float64) map[planner.ConceptTag]float64 {
	if in == nil {
		return nil
	}
	if halfLifeDays <= 0 {
		halfLifeDays = DefaultDecayHalfLifeDays
	}
	out := make(map[planner.ConceptTag]float64, len(in))
	if days <= 0 {
		for k, v := range in {
			out[k] = v
		}
		return out
	}
	factor := math.Pow(0.5, days/halfLifeDays)
	for k, v := range in {
		out[k] = v * factor
	}
	return out
}

// AgeFilesByDays is the string-keyed twin of AgeByDays for the
// AgentProfile.Files map.
func AgeFilesByDays(in map[string]float64, days, halfLifeDays float64) map[string]float64 {
	if in == nil {
		return nil
	}
	if halfLifeDays <= 0 {
		halfLifeDays = DefaultDecayHalfLifeDays
	}
	out := make(map[string]float64, len(in))
	if days <= 0 {
		for k, v := range in {
			out[k] = v
		}
		return out
	}
	factor := math.Pow(0.5, days/halfLifeDays)
	for k, v := range in {
		out[k] = v * factor
	}
	return out
}

// daysBetween returns wall-clock days between two timestamps.
// Negative inputs (now < since) clamp to 0 so a clock skew can't
// PROMOTE weights. Used both by Store.RecordCompletion and any
// pure math test that wants a stable elapsed-days computation.
func daysBetween(now, since time.Time) float64 {
	if since.IsZero() {
		return 0
	}
	delta := now.Sub(since)
	if delta <= 0 {
		return 0
	}
	return delta.Hours() / 24.0
}
