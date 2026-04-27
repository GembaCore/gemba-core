package bd

import "github.com/MikeBengtson/gemba/core"

// gm-s47n.1.1: Layer 0 enrichment labels project bead labels onto the
// four typed core.WorkItem enrichment fields (targets, concepts,
// dispatch_status, estimated_size). bd has no structured-extras
// surface today — labels are the established extension mechanism
// (read:, write:, repo:, branch:, type:milestone all use the same
// pattern), so the planner-facing schema rides through them.
//
// Operators set these labels via `bd update --add-label target:<glob>`
// (and similar). The CLI surface that gm-s47n.1.3 ships writes them
// the same way; this file is the read side.

// label prefixes — single source of truth for the enrichment labels.
// Other label prefixes ("read:", "write:", …) live in repositories.go
// where their typed projection does. Keeping the four enrichment
// prefixes together here makes the schema slice's surface obvious.
const (
	labelTargetPrefix   = "target:"
	labelConceptPrefix  = "concept:"
	labelDispatchPrefix = "dispatch:"
	labelSizePrefix     = "size:"
)

// targetsFromLabels mirrors readPathsFromLabels for `target:<glob>`.
// Order is preserved on first occurrence; duplicates and empties are
// dropped so a repeated label doesn't inflate the planner's
// conflict-scoring inputs.
func targetsFromLabels(labels []string) []string {
	return pathsFromLabels(labels, labelTargetPrefix)
}

// conceptsFromLabels parses `concept:<tag>` labels. Tag normalisation
// happens at the vocabulary layer (internal/concepts) rather than
// here so the bd adaptor stays free of the concepts package import —
// the planner re-normalises on read regardless.
func conceptsFromLabels(labels []string) []string {
	return pathsFromLabels(labels, labelConceptPrefix)
}

// dispatchStatusFromLabels resolves the bead's soft-block status.
// Returns the empty DispatchStatus when no `dispatch:` label is
// present — consumers normalise via [core.DispatchStatus.Effective]
// to DispatchReady. When multiple `dispatch:` labels are present the
// first canonical value wins; unknown values are dropped (the
// resolver biases toward "let the bead through" rather than
// silently soft-blocking on a typo).
func dispatchStatusFromLabels(labels []string) core.DispatchStatus {
	for _, l := range labels {
		if len(l) <= len(labelDispatchPrefix) || l[:len(labelDispatchPrefix)] != labelDispatchPrefix {
			continue
		}
		candidate := core.DispatchStatus(l[len(labelDispatchPrefix):])
		if candidate.IsValid() && candidate != "" {
			return candidate
		}
	}
	return ""
}

// estimatedSizeFromLabels resolves the bead's size bucket. Same
// "first canonical wins, unknowns drop" rule as dispatchStatus —
// an operator typo doesn't hide the bead from the planner; it just
// falls back to [core.EstimatedSize.Effective]'s SizeMedium default.
func estimatedSizeFromLabels(labels []string) core.EstimatedSize {
	for _, l := range labels {
		if len(l) <= len(labelSizePrefix) || l[:len(labelSizePrefix)] != labelSizePrefix {
			continue
		}
		candidate := core.EstimatedSize(l[len(labelSizePrefix):])
		if candidate.IsValid() && candidate != "" {
			return candidate
		}
	}
	return ""
}
