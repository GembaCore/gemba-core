// Session-health telemetry: gm-s47n.5.1.
//
// Three numbers per active session — context_pressure, concept_drift,
// time_on_task — surfaced to the upcoming `gemba session-health`
// CLI (gm-s47n.5.2), the SPA panel (gm-s47n.5.3), and the
// operational-context aggregator (gm-s47n.5.4). Read-only; advisory
// thresholds; no auto-kill (spec §4 Layer 4).

package planner

import (
	"context"
	"math"
	"time"

	"github.com/MikeBengtson/gemba/internal/core"
)

// BeadConceptLookup retrieves the concept profile for a previously
// completed bead. The concrete implementation lands with gm-s47n.1
// (WorkItem.concepts enrichment); callers that don't have it yet
// pass nil and accept ConceptDrift staying at zero.
//
// One method, no surface — keeps the interface narrow so any
// concept source (a dolt table, an external classifier, a static
// fixture in tests) can plug in.
type BeadConceptLookup interface {
	// BeadConcepts returns the concept-tag → weight map associated
	// with the given completed bead. Returning a nil/empty map +
	// nil error is "no concepts known for this bead" — the planner
	// treats it as a non-error skip.
	BeadConcepts(ctx context.Context, beadID core.WorkItemID) (map[ConceptTag]float64, error)
}

// ComputeHealth produces the SessionHealth snapshot per gm-s47n.5.1.
// All three numbers are derived together so callers (the CLI in
// .5.2, the SPA in .5.3, the operational-context aggregator in
// .5.4) get a coherent snapshot from one call instead of stitching
// it themselves.
//
// concepts is optional. When nil OR profile.LastBeads is empty,
// ConceptDrift stays at zero (no last-N vector available); the
// rest of the snapshot still populates so callers can render a
// degraded-but-useful card.
//
// now is injected for deterministic tests; production wires
// time.Now. nil falls through to time.Now.
//
// Returns (nil, nil) when sess is nil — preserves the
// "Health follows Session" invariant operational_context.go relies
// on. A non-nil sess always yields a non-nil SessionHealth: at
// minimum TimeOnTask is meaningful from a session alone, and the
// other fields have valid zero defaults.
func ComputeHealth(
	ctx context.Context,
	sess *core.Session,
	profile *SessionProfile,
	concepts BeadConceptLookup,
	now func() time.Time,
) (*SessionHealth, error) {
	if sess == nil {
		return nil, nil
	}
	if now == nil {
		now = time.Now
	}
	h := &SessionHealth{
		TimeOnTask: now().Sub(sess.StartedAt),
	}
	if profile == nil {
		return h, nil
	}
	h.ContextPressure = profile.ContextPct

	// ConceptDrift requires both vectors. If the lifetime profile
	// has nothing, drift is meaningless (fresh session); if the
	// LastNConceptVector ends up empty, drift is also meaningless
	// (no recent activity to compare against). Either way: zero.
	if concepts == nil || len(profile.Concepts) == 0 || len(profile.LastBeads) == 0 {
		return h, nil
	}
	lastN, err := LastNConceptVector(ctx, profile.LastBeads, concepts)
	if err != nil {
		return nil, err
	}
	h.ConceptDrift = ConceptDrift(profile.Concepts, lastN)
	return h, nil
}

// ConceptDrift is the cosine distance between two concept vectors.
// Range [0, 1] for non-negative weight maps:
//
//	0 — vectors point the same direction (same topic mix)
//	1 — vectors are orthogonal (completely different concepts)
//
// Empty / zero-magnitude vectors return 0 (no drift detectable
// from missing data — a session with no recent concept activity
// is not "drifting", it's idle).
//
// Pure function. The math is identical to the standard cosine
// similarity ↔ distance pair, restricted to non-negative weights.
// Floating-point clamp guards against tiny excursions outside
// [0, 1] when norms are nearly identical.
func ConceptDrift(lifetime, lastN map[ConceptTag]float64) float64 {
	if len(lifetime) == 0 || len(lastN) == 0 {
		return 0
	}
	dot := 0.0
	for k, vL := range lifetime {
		if vN, ok := lastN[k]; ok {
			dot += vL * vN
		}
	}
	normL := vectorNorm(lifetime)
	normN := vectorNorm(lastN)
	if normL == 0 || normN == 0 {
		return 0
	}
	cosSim := dot / (normL * normN)
	// Clamp tiny floating-point excursions so a "1.0000000001"
	// doesn't become a "negative drift".
	if cosSim > 1 {
		cosSim = 1
	}
	if cosSim < 0 {
		cosSim = 0
	}
	return 1 - cosSim
}

// LastNConceptVector aggregates the concept profiles of the last-N
// completed beads into a single vector. Each bead's concept map is
// summed key-wise; no decay is applied here — SessionProfile.Concepts
// already carries the recency-decayed lifetime view, and the drift
// calculation needs the un-decayed last-N snapshot to compare against.
//
// Per-bead lookup errors are skipped (one missing bead shouldn't
// poison the whole snapshot). Returning an empty map + nil err is
// the contract for "lookup ran but had nothing to add" — the caller
// treats that as zero drift.
func LastNConceptVector(
	ctx context.Context,
	lastBeads []core.WorkItemID,
	lookup BeadConceptLookup,
) (map[ConceptTag]float64, error) {
	out := map[ConceptTag]float64{}
	if lookup == nil || len(lastBeads) == 0 {
		return out, nil
	}
	for _, id := range lastBeads {
		concepts, err := lookup.BeadConcepts(ctx, id)
		if err != nil {
			// Tolerate per-bead lookup errors. The drift signal is
			// advisory; one missing bead shouldn't fail the whole
			// snapshot. If the lookup is systemically broken the
			// ComputeHealth caller can detect it via Describe / the
			// CLI banner — that's not this function's job.
			continue
		}
		for k, v := range concepts {
			out[k] += v
		}
	}
	return out, nil
}

func vectorNorm(m map[ConceptTag]float64) float64 {
	sum := 0.0
	for _, v := range m {
		sum += v * v
	}
	return math.Sqrt(sum)
}
