// Semantic-conflict detector. See doc.go for context.

package semantic

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/planner/conflicts"
	"github.com/GembaCore/gemba-core/internal/planner/targets"
	"github.com/GembaCore/gemba-core/internal/sourceanalysis"
)

// Detector implements conflicts.SemanticDetector by running
// SourceAnalysis.Dependents queries over each bead's target files
// and intersecting against the other bead's target set.
//
// Configure once per planner cycle; the detector is stateless
// between Detect calls so the conflicts pairwise loop can run it
// safely in goroutines (sourceanalysis.SourceAnalysis is documented
// concurrent-safe).
type Detector struct {
	// Source is the backing analysis surface. Required.
	Source sourceanalysis.SourceAnalysis

	// Repository scopes the Target.Repository field for every
	// Dependents query. Required: SourceAnalysis edges are
	// repo-local, and an empty Repository yields ambiguous lookups
	// when the index spans multiple repos.
	Repository string

	// FS optionally expands non-literal target patterns into
	// concrete file paths before querying Dependents. nil leaves
	// non-literal targets unhandled — they're skipped silently
	// (Dependents on a glob has no meaningful semantics, so
	// fabricating an answer would mislead the planner).
	FS targets.Filesystem
}

// Compile-time interface check.
var _ conflicts.SemanticDetector = (*Detector)(nil)

// Detect implements conflicts.SemanticDetector.
//
// Two-direction check: a→b ("a's changes break things b modifies")
// AND b→a ("b's changes break things a modifies"). Either direction
// hitting is enough to flag the pair. Returns on the first hit so
// large bead sets pay only for the cheapest reason.
func (d *Detector) Detect(ctx context.Context, a, b conflicts.Bead) (bool, string, error) {
	if d == nil || d.Source == nil {
		return false, "", nil
	}
	aFiles, err := d.resolveFiles(a.Targets)
	if err != nil {
		return false, "", err
	}
	bFiles, err := d.resolveFiles(b.Targets)
	if err != nil {
		return false, "", err
	}
	if len(aFiles) == 0 || len(bFiles) == 0 {
		return false, "", nil
	}

	bSet := toSet(bFiles)
	aSet := toSet(aFiles)

	// Direction 1: a's files have dependents in b's target set?
	if hit, evidence, err := d.dependsAny(ctx, aFiles, bSet, a.ID, b.ID); err != nil {
		return false, "", err
	} else if hit {
		return true, evidence, nil
	}

	// Direction 2: b's files have dependents in a's target set?
	if hit, evidence, err := d.dependsAny(ctx, bFiles, aSet, b.ID, a.ID); err != nil {
		return false, "", err
	} else if hit {
		return true, evidence, nil
	}

	return false, "", nil
}

// dependsAny queries Dependents() for each `subjects` file and
// returns the first match against `victims`. The (subjID, victimID)
// pair is folded into the evidence string so the caller doesn't
// have to re-resolve who-broke-whom.
//
// ErrUnavailable from any single query short-circuits the WHOLE
// pair as "no semantic conflict detected" — graceful degrade per
// work-planning.md §5.3. The planner caller logs the skip via
// SourceAnalysis.Describe; this layer stays silent so the
// SemanticDetector contract's "(false, nil)" answer is honoured.
func (d *Detector) dependsAny(
	ctx context.Context,
	subjects []string,
	victims map[string]struct{},
	subjID, victimID core.WorkItemID,
) (bool, string, error) {
	for _, subject := range subjects {
		deps, err := d.Source.Dependents(ctx, sourceanalysis.Target{
			Repository: d.Repository,
			Path:       subject,
		})
		if err != nil {
			if errors.Is(err, sourceanalysis.ErrUnavailable) {
				return false, "", nil
			}
			return false, "", fmt.Errorf("semantic.Detect: Dependents(%q): %w", subject, err)
		}
		for _, dep := range deps {
			// Skip self-references — they're not a cross-bead conflict.
			if dep.Path == subject {
				continue
			}
			if _, ok := victims[dep.Path]; ok {
				return true, fmt.Sprintf(
					"%s's %s is depended on by %s's %s",
					subjID, subject, victimID, dep.Path,
				), nil
			}
		}
	}
	return false, "", nil
}

// resolveFiles flattens a target pattern slice into concrete file
// paths. Literal patterns pass through; glob patterns require an
// FS — without one they're silently dropped (querying Dependents
// on a glob has no defined meaning, so the conservative move is
// to under-report and let target-overlap catch the file conflict
// directly).
//
// Order is preserved so the Detect output is deterministic across
// runs of the same input (FS implementations should already return
// stable orderings; we don't re-sort here to avoid burning cycles
// when the FS already does the right thing).
func (d *Detector) resolveFiles(patterns []targets.Pattern) ([]string, error) {
	out := make([]string, 0, len(patterns))
	for _, p := range patterns {
		s := normalise(string(p))
		if !hasGlobMeta(s) {
			out = append(out, s)
			continue
		}
		if d.FS == nil {
			continue
		}
		expanded, err := d.FS.Glob(targets.Pattern(s))
		if err != nil {
			return nil, fmt.Errorf("semantic.resolveFiles: glob %q: %w", s, err)
		}
		out = append(out, expanded...)
	}
	return out, nil
}

// hasGlobMeta mirrors targets.hasMeta — that helper is unexported,
// and adding an exported alias just for the semantic package would
// pollute the targets API. Keep this stub tiny and the duplication
// honest.
func hasGlobMeta(s string) bool {
	return strings.ContainsAny(s, "*?[")
}

// normalise strips a leading "./" or "/" so the file paths the
// detector hands to SourceAnalysis match the index's repo-relative
// convention. Mirrors targets.normalise (also unexported); same
// rationale as hasGlobMeta.
func normalise(s string) string {
	s = strings.TrimPrefix(s, "./")
	s = strings.TrimPrefix(s, "/")
	for strings.Contains(s, "//") {
		s = strings.ReplaceAll(s, "//", "/")
	}
	return s
}

func toSet(xs []string) map[string]struct{} {
	out := make(map[string]struct{}, len(xs))
	for _, x := range xs {
		out[x] = struct{}{}
	}
	return out
}
