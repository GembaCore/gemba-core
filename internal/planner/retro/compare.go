// Diff-vs-declared comparator (gm-s47n.8.1).

package retro

import (
	"sort"

	"github.com/GembaCore/gemba-core/internal/planner"
	"github.com/GembaCore/gemba-core/internal/planner/targets"
)

// Declared is the bead-authored input — what the planner thought
// the bead would touch.
type Declared struct {
	Targets  []targets.Pattern    `json:"targets,omitempty"`
	Concepts []planner.ConceptTag `json:"concepts,omitempty"`
}

// Actual is the post-merge ground truth — what the bead's commits
// actually changed. Files is the union of repo-relative paths
// touched across the bead's merge commits; Concepts is whatever the
// caller inferred (typically via SourceAnalysis on the diff plus a
// controlled-vocabulary projection).
type Actual struct {
	Files    []string             `json:"files,omitempty"`
	Concepts []planner.ConceptTag `json:"concepts,omitempty"`
}

// TargetCoverage names a single declared pattern alongside the
// concrete actual files it matched. Useful for debugging an
// over-declared pattern: a `MatchedFiles == nil` entry tells the
// reviewer the pattern matched nothing.
type TargetCoverage struct {
	Pattern      targets.Pattern `json:"pattern"`
	MatchedFiles []string        `json:"matched_files,omitempty"`
}

// Diff is the comparator output — the per-bead retrospective record.
//
// Stable JSON shape: surfaces in the scorer_grades dolt row
// (gm-s47n.8.2) and the queryable retrospective view (gm-s47n.8.4),
// so adding fields here is a wire-compat change. Removing or
// renaming is a breaking change.
type Diff struct {
	// MatchedTargets pairs each declared pattern with the actual
	// files it covered. A pattern with an empty MatchedFiles list
	// is over-declared (the bead said it would touch this surface
	// but didn't).
	MatchedTargets []TargetCoverage `json:"matched_targets,omitempty"`
	// UnmatchedDeclared is the convenience subset: declared
	// patterns that no actual file covered. Sorted lexically for
	// stable output.
	UnmatchedDeclared []targets.Pattern `json:"unmatched_declared,omitempty"`
	// UndeclaredActual is the inverse: actual files that no
	// declared pattern covered. Under-declared scope. Sorted
	// lexically.
	UndeclaredActual []string `json:"undeclared_actual,omitempty"`

	// KeptConcepts intersects declared and actual.
	KeptConcepts []planner.ConceptTag `json:"kept_concepts,omitempty"`
	// MissingConcepts is declared - actual: tags the bead claimed
	// it would touch that the diff didn't bear out.
	MissingConcepts []planner.ConceptTag `json:"missing_concepts,omitempty"`
	// NewConcepts is actual - declared: tags inferred from the
	// diff that the bead never declared. A signal for vocabulary
	// growth or a missed extraction.
	NewConcepts []planner.ConceptTag `json:"new_concepts,omitempty"`

	// TargetDivergence is the Jaccard distance over the SETS
	// {declared targets that matched ≥1 actual} ∪ {actual files}
	// — i.e. |symmetric difference| / |union|. Range [0, 1]; 0
	// means the declaration was perfect, 1 means no overlap.
	//
	// A pattern that matches multiple files contributes once on
	// the declared side and once per matched file on the actual
	// side. This is intentional: a bead declaring `src/auth/**`
	// that touched 12 files in src/auth is a perfect match
	// (divergence 0), but a bead declaring 12 specific paths and
	// only touching 1 is heavily over-declared (divergence ~0.91).
	TargetDivergence float64 `json:"target_divergence"`

	// ConceptDivergence is Jaccard distance over the declared and
	// actual concept sets.
	ConceptDivergence float64 `json:"concept_divergence"`
}

// HasDrift reports whether the diff's divergence on either axis
// crossed the given threshold. A threshold of 0.5 (the spec's
// example "diverged > 50%") is the typical review trigger.
func (d Diff) HasDrift(threshold float64) bool {
	return d.TargetDivergence > threshold || d.ConceptDivergence > threshold
}

// Compare produces the retrospective diff. Pure function; safe to
// call from any goroutine and from inside an RPC handler. Returns
// a zero-value Diff when both sides are empty.
func Compare(decl Declared, act Actual) Diff {
	out := Diff{}
	out.MatchedTargets, out.UnmatchedDeclared, out.UndeclaredActual = compareTargets(decl.Targets, act.Files)
	out.KeptConcepts, out.MissingConcepts, out.NewConcepts = compareConcepts(decl.Concepts, act.Concepts)
	out.TargetDivergence = targetDivergence(out)
	out.ConceptDivergence = conceptDivergence(out)
	return out
}

// compareTargets resolves declared globs against the actual file
// list. A declared pattern is "matched" iff at least one actual
// file matches it; an actual file is "covered" iff at least one
// declared pattern matches it.
func compareTargets(decl []targets.Pattern, files []string) ([]TargetCoverage, []targets.Pattern, []string) {
	if len(decl) == 0 && len(files) == 0 {
		return nil, nil, nil
	}
	covered := make(map[string]bool, len(files))
	matched := make([]TargetCoverage, 0, len(decl))
	var unmatched []targets.Pattern
	for _, p := range decl {
		var hits []string
		for _, f := range files {
			if targets.Match(p, f) {
				hits = append(hits, f)
				covered[f] = true
			}
		}
		if len(hits) == 0 {
			unmatched = append(unmatched, p)
			continue
		}
		sort.Strings(hits)
		matched = append(matched, TargetCoverage{Pattern: p, MatchedFiles: hits})
	}
	sort.Slice(unmatched, func(i, j int) bool { return unmatched[i] < unmatched[j] })
	var undeclared []string
	for _, f := range files {
		if !covered[f] {
			undeclared = append(undeclared, f)
		}
	}
	sort.Strings(undeclared)
	return matched, unmatched, undeclared
}

// compareConcepts is the simpler set-difference — concepts are an
// unordered tag set, no glob semantics.
func compareConcepts(decl, act []planner.ConceptTag) ([]planner.ConceptTag, []planner.ConceptTag, []planner.ConceptTag) {
	declSet := make(map[planner.ConceptTag]struct{}, len(decl))
	for _, c := range decl {
		declSet[c] = struct{}{}
	}
	actSet := make(map[planner.ConceptTag]struct{}, len(act))
	for _, c := range act {
		actSet[c] = struct{}{}
	}
	var kept, missing, fresh []planner.ConceptTag
	for c := range declSet {
		if _, ok := actSet[c]; ok {
			kept = append(kept, c)
		} else {
			missing = append(missing, c)
		}
	}
	for c := range actSet {
		if _, ok := declSet[c]; !ok {
			fresh = append(fresh, c)
		}
	}
	sortTags(kept)
	sortTags(missing)
	sortTags(fresh)
	return kept, missing, fresh
}

func sortTags(t []planner.ConceptTag) {
	sort.Slice(t, func(i, j int) bool { return t[i] < t[j] })
}

// targetDivergence reports the Jaccard distance over the declared-
// covered + actual file sets. The "declared" side is the set of
// concrete files the declared patterns matched (collapsing globs
// to their effective coverage); the "actual" side is the merge's
// file list. A bead with no declared targets and no touched files
// is divergence 0 (vacuous match); declared-with-no-files is 1.0,
// files-with-no-declared is 1.0.
func targetDivergence(d Diff) float64 {
	declared := make(map[string]struct{})
	for _, mt := range d.MatchedTargets {
		for _, f := range mt.MatchedFiles {
			declared[f] = struct{}{}
		}
	}
	// Unmatched declared patterns contribute to the declared side
	// keyed by their raw pattern text. They are guaranteed not to
	// collide with actual files (no actual file matched them).
	for _, p := range d.UnmatchedDeclared {
		declared[string(p)] = struct{}{}
	}
	actual := make(map[string]struct{})
	for _, mt := range d.MatchedTargets {
		for _, f := range mt.MatchedFiles {
			actual[f] = struct{}{}
		}
	}
	for _, f := range d.UndeclaredActual {
		actual[f] = struct{}{}
	}
	return jaccardDistance(declared, actual)
}

func conceptDivergence(d Diff) float64 {
	left := make(map[string]struct{})
	for _, c := range d.KeptConcepts {
		left[string(c)] = struct{}{}
	}
	for _, c := range d.MissingConcepts {
		left[string(c)] = struct{}{}
	}
	right := make(map[string]struct{})
	for _, c := range d.KeptConcepts {
		right[string(c)] = struct{}{}
	}
	for _, c := range d.NewConcepts {
		right[string(c)] = struct{}{}
	}
	return jaccardDistance(left, right)
}

// jaccardDistance is 1 - |a ∩ b| / |a ∪ b|. Returns 0 for two
// empty sets ("vacuous match", per the comparator's docstring).
func jaccardDistance(a, b map[string]struct{}) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 0
	}
	inter := 0
	for k := range a {
		if _, ok := b[k]; ok {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return 1 - float64(inter)/float64(union)
}
