// Event-half-life decay (gm-s47n.2.2). Pure math the write hooks
// (.2.3) call when a bead completion lands, and the planner uses
// to age out a profile in-place when no completion is pending.
//
// Spec §5.1:
//
//   Let e_1, ..., e_n be bead-completion events for a session,
//   oldest to newest, each with concept set C_i. With half-life
//   h (in events), the weight of event e_i at time e_n is:
//
//     w_i = 0.5 ^ ((n - i) / h)
//
//   Session concept weight for tag t:
//
//     S(t) = Σ_{i : t ∈ C_i} w_i
//
// Half-life is in events (not wall time) so a session that was
// idle overnight still remembers what it did yesterday — the
// retrospective grades the priming, not the clock.

package planner

import "math"

// EventContribution is one bead-completion's contribution to the
// session profile. Both maps are caller-supplied and not mutated.
//
// The Weight field is the *intrinsic* weight of the contribution —
// usually 1.0 (the bead happened) but DecayConcepts respects the
// caller's choice in case a future scorer wants to down-weight a
// canceled bead, an emergency rollback, or a bead the operator
// explicitly excluded from priming.
type EventContribution struct {
	Concepts []ConceptTag
	Files    []string
	Weight   float64
}

// DecayWeight returns 0.5 ^ ((n - i) / h) — the per-event factor
// for an event i positions back from the most-recent event in a
// stream of n events with half-life h.
//
//	eventsSinceMostRecent = n - i
//
// 0 → most recent event (weight 1.0)
// h → exactly half the most recent (weight 0.5)
//
// halfLife <= 0 falls back to DefaultDecayHalfLife so callers
// passing a sentinel zero don't get a division-by-zero. Negative
// eventsSinceMostRecent is clamped to 0 — "future" events are
// treated as right-now.
func DecayWeight(eventsSinceMostRecent int, halfLife int) float64 {
	if halfLife <= 0 {
		halfLife = DefaultDecayHalfLife
	}
	if eventsSinceMostRecent < 0 {
		eventsSinceMostRecent = 0
	}
	return math.Pow(0.5, float64(eventsSinceMostRecent)/float64(halfLife))
}

// DecayConcepts walks the event stream oldest-to-newest and
// returns the summed concept profile S(t). Stream order matters:
// events[0] is the oldest, events[len-1] is the most recent (the
// "now" reference for the n - i computation).
//
// halfLife <= 0 falls back to DefaultDecayHalfLife.
//
// Returns an empty (non-nil) map when there are no events so
// callers can range over the result without nil-checking.
func DecayConcepts(events []EventContribution, halfLife int) map[ConceptTag]float64 {
	out := make(map[ConceptTag]float64)
	if len(events) == 0 {
		return out
	}
	if halfLife <= 0 {
		halfLife = DefaultDecayHalfLife
	}
	n := len(events)
	for i, e := range events {
		dist := n - 1 - i // 0 for most recent; n-1 for oldest
		w := e.Weight
		if w == 0 {
			w = 1.0
		}
		factor := DecayWeight(dist, halfLife) * w
		for _, tag := range e.Concepts {
			out[tag] += factor
		}
	}
	return out
}

// DecayFiles is the file-axis twin of DecayConcepts.
func DecayFiles(events []EventContribution, halfLife int) map[string]float64 {
	out := make(map[string]float64)
	if len(events) == 0 {
		return out
	}
	if halfLife <= 0 {
		halfLife = DefaultDecayHalfLife
	}
	n := len(events)
	for i, e := range events {
		dist := n - 1 - i
		w := e.Weight
		if w == 0 {
			w = 1.0
		}
		factor := DecayWeight(dist, halfLife) * w
		for _, path := range e.Files {
			out[path] += factor
		}
	}
	return out
}

// AgeProfile rescales an existing decayed profile as if every
// stored weight referred to one additional event back. Used by
// the write hooks (.2.3) when a new bead lands without changing
// the prior events: each existing weight gets multiplied by
// 0.5^(1/h), then the new bead's contribution is added on top.
//
// Pure: returns a new map; the input is not mutated. nil input
// returns nil — callers can chain without nil-checking.
//
// halfLife <= 0 falls back to DefaultDecayHalfLife.
func AgeProfile(in map[ConceptTag]float64, halfLife int) map[ConceptTag]float64 {
	if in == nil {
		return nil
	}
	if halfLife <= 0 {
		halfLife = DefaultDecayHalfLife
	}
	factor := math.Pow(0.5, 1.0/float64(halfLife))
	out := make(map[ConceptTag]float64, len(in))
	for k, v := range in {
		out[k] = v * factor
	}
	return out
}

// AgeFileProfile is the string-keyed twin of AgeProfile, used for
// the SessionProfile.Files map.
func AgeFileProfile(in map[string]float64, halfLife int) map[string]float64 {
	if in == nil {
		return nil
	}
	if halfLife <= 0 {
		halfLife = DefaultDecayHalfLife
	}
	factor := math.Pow(0.5, 1.0/float64(halfLife))
	out := make(map[string]float64, len(in))
	for k, v := range in {
		out[k] = v * factor
	}
	return out
}

// MergeContribution adds a bead's concept tags onto a previously
// aged profile at full weight (the new event sits at i = n, so
// weight = 0.5^(0/h) = 1.0). Returns a new map; input not mutated.
//
// Use AgeProfile + MergeContribution together when a new event
// lands: age the existing profile, then merge the new event in
// at full weight. The result equals what DecayConcepts would have
// produced if we'd recomputed from the full event stream — but
// without keeping the stream around (the SessionProfile only
// stores the summed weights, not the per-event history).
func MergeContribution(profile map[ConceptTag]float64, tags []ConceptTag, weight float64) map[ConceptTag]float64 {
	if weight == 0 {
		weight = 1.0
	}
	out := make(map[ConceptTag]float64, len(profile)+len(tags))
	for k, v := range profile {
		out[k] = v
	}
	for _, tag := range tags {
		out[tag] += weight
	}
	return out
}

// MergeFileContribution is the string-keyed twin.
func MergeFileContribution(profile map[string]float64, paths []string, weight float64) map[string]float64 {
	if weight == 0 {
		weight = 1.0
	}
	out := make(map[string]float64, len(profile)+len(paths))
	for k, v := range profile {
		out[k] = v
	}
	for _, p := range paths {
		out[p] += weight
	}
	return out
}
