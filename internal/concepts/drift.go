package concepts

import (
	"sort"
	"time"
)

// BeadConcepts is the slice projection a [BeadConceptStore] returns
// for each bead — just the id and the current concept set. Both the
// drift detector and the historical rewrite consume this shape.
type BeadConcepts struct {
	BeadID   string
	Concepts []string
	// CreatedAt + ClosedAt feed the singleton-decay heuristic. Both
	// optional; zero values disable the time-window filter.
	CreatedAt time.Time
	ClosedAt  *time.Time
}

// DriftOpts tunes the detector's thresholds. Defaults match the
// values documented in docs/design/work-planning.md §6.4.
type DriftOpts struct {
	// NearDuplicateJaccard is the minimum Jaccard similarity a pair
	// of terms must share before the detector flags them as
	// near-duplicates. Default 0.6.
	NearDuplicateJaccard float64

	// NearDuplicateUseRatio guards against flagging a pair where
	// one term is heavily used and the other is a singleton — the
	// usage profiles must be comparable. min(|a|,|b|)/max(|a|,|b|).
	// Default 0.5.
	NearDuplicateUseRatio float64

	// SingletonDormantDays is how long after a bead's ClosedAt a
	// singleton-on-that-bead must wait before the detector emits a
	// delete suggestion. Default 90 (per spec §6.2). Set to 0 to
	// disable the dormant gate (every singleton becomes a suggestion).
	SingletonDormantDays int

	// SingletonMaxUses is the inclusive upper bound on the bead-count
	// for a term to qualify as a singleton candidate. Default 2 — the
	// spec's "fewer than 3 beads". Set to 1 for the strict "exactly
	// one bead" interpretation.
	SingletonMaxUses int

	// Now is the reference time for dormant calculations. Tests
	// inject a fixed time so cases stay deterministic; production
	// leaves it zero (defaults to time.Now().UTC()).
	Now time.Time
}

// DefaultDriftOpts is the policy that ships. Threshold values target
// the intent of work-planning.md §6.2 (cosine ≥ 0.85 near-dups,
// singletons "< 3 beads after 90 days") translated to the Jaccard +
// dormant-only metrics this detector ships:
//
//   - Jaccard 0.7 lands at a similar precision to cosine 0.85 on the
//     small-sparse-set distribution beads produce in practice.
//   - Singleton dormant 90d matches the spec's "after 90 days" gate.
//     Use-count < 3 (rather than == 1) is enforced via [SingletonMaxUses].
func DefaultDriftOpts() DriftOpts {
	return DriftOpts{
		NearDuplicateJaccard:  0.7,
		NearDuplicateUseRatio: 0.5,
		SingletonDormantDays:  90,
		SingletonMaxUses:      2,
	}
}

// Drift is the detector's report.
type Drift struct {
	NearDuplicates []NearDuplicate `json:"near_duplicates,omitempty"`
	Singletons     []Singleton     `json:"singletons,omitempty"`
}

// NearDuplicate flags a pair of terms whose co-occurrence pattern
// suggests they're being used interchangeably.
type NearDuplicate struct {
	A       string  `json:"a"`
	B       string  `json:"b"`
	Jaccard float64 `json:"jaccard"`
	UsesA   int     `json:"uses_a"`
	UsesB   int     `json:"uses_b"`
}

// Singleton flags a term used on exactly one bead. Carries that
// bead's id + close timestamp so the operator can decide whether the
// concept ever generalized.
type Singleton struct {
	Term       string     `json:"term"`
	BeadID     string     `json:"bead_id"`
	ClosedAt   *time.Time `json:"closed_at,omitempty"`
	DormantFor int        `json:"dormant_days,omitempty"`
}

// DetectDrift reads bead concepts and returns the current drift
// state. Pure: same input → same output, no mutation.
//
// Drifters (semantic neighbor walking) live in gm-s47n.3 — the source
// analysis abstraction is the right place for embedding-based work,
// not this co-occurrence-only detector. This function ships the two
// signal types the bead description called out as concrete (.7.2).
func DetectDrift(beads []BeadConcepts, opts DriftOpts) Drift {
	if opts.NearDuplicateJaccard <= 0 {
		opts.NearDuplicateJaccard = DefaultDriftOpts().NearDuplicateJaccard
	}
	if opts.NearDuplicateUseRatio <= 0 {
		opts.NearDuplicateUseRatio = DefaultDriftOpts().NearDuplicateUseRatio
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now().UTC()
	}

	// Index: term → set of bead ids. Co-occurrence is set
	// intersection / union over these. Singletons are terms whose
	// set has size 1.
	idx := make(map[string]map[string]struct{})
	for _, b := range beads {
		seenInBead := make(map[string]bool)
		for _, c := range b.Concepts {
			canon := Normalize(c)
			if canon == "" || seenInBead[canon] {
				continue
			}
			seenInBead[canon] = true
			set, ok := idx[canon]
			if !ok {
				set = make(map[string]struct{})
				idx[canon] = set
			}
			set[b.BeadID] = struct{}{}
		}
	}

	terms := make([]string, 0, len(idx))
	for t := range idx {
		terms = append(terms, t)
	}
	sort.Strings(terms)

	out := Drift{}

	// Pairwise comparison. O(n^2) over distinct concepts — fine for
	// 30-60 bootstrapped terms; if the vocabulary ever grows past a
	// few hundred we'd switch to inverted-index pruning.
	for i := 0; i < len(terms); i++ {
		a := terms[i]
		setA := idx[a]
		for j := i + 1; j < len(terms); j++ {
			b := terms[j]
			setB := idx[b]
			intersect := 0
			for id := range setA {
				if _, ok := setB[id]; ok {
					intersect++
				}
			}
			if intersect == 0 {
				continue
			}
			union := len(setA) + len(setB) - intersect
			jac := float64(intersect) / float64(union)
			if jac < opts.NearDuplicateJaccard {
				continue
			}
			minN, maxN := len(setA), len(setB)
			if minN > maxN {
				minN, maxN = maxN, minN
			}
			if maxN == 0 {
				continue
			}
			if float64(minN)/float64(maxN) < opts.NearDuplicateUseRatio {
				continue
			}
			out.NearDuplicates = append(out.NearDuplicates, NearDuplicate{
				A: a, B: b,
				Jaccard: jac,
				UsesA:   len(setA), UsesB: len(setB),
			})
		}
	}

	// Singletons: term used on at most SingletonMaxUses beads. The
	// dormant filter gates on the most-recent ClosedAt across those
	// beads so an actively-developed concept doesn't get prematurely
	// flagged.
	maxUses := opts.SingletonMaxUses
	if maxUses <= 0 {
		maxUses = DefaultDriftOpts().SingletonMaxUses
	}
	beadByID := make(map[string]BeadConcepts, len(beads))
	for _, b := range beads {
		beadByID[b.BeadID] = b
	}
	for _, term := range terms {
		set := idx[term]
		if len(set) == 0 || len(set) > maxUses {
			continue
		}
		// Pick the most-recently-closed bead for the dormant
		// calculation; a singleton appearing on an open bead pins
		// the most-recent ClosedAt to nil (open trumps closed).
		var (
			anchorID    string
			anchorClose *time.Time
			anyOpen     bool
		)
		for id := range set {
			b := beadByID[id]
			if b.ClosedAt == nil {
				anyOpen = true
				anchorID = id
				anchorClose = nil
				continue
			}
			if anyOpen {
				continue
			}
			if anchorClose == nil || b.ClosedAt.After(*anchorClose) {
				anchorClose = b.ClosedAt
				anchorID = id
			}
		}
		var dormant int
		if anchorClose != nil {
			dormant = int(opts.Now.Sub(*anchorClose) / (24 * time.Hour))
		}
		if opts.SingletonDormantDays > 0 {
			// Open beads always skip — operator hasn't had a chance
			// to generalize the concept yet.
			if anyOpen {
				continue
			}
			if dormant < opts.SingletonDormantDays {
				continue
			}
		}
		out.Singletons = append(out.Singletons, Singleton{
			Term:       term,
			BeadID:     anchorID,
			ClosedAt:   anchorClose,
			DormantFor: dormant,
		})
	}

	return out
}
