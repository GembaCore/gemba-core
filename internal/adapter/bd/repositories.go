package bd

import (
	"strings"

	"github.com/MikeBengtson/gemba/internal/core"
)

// labelRepoPrefix is the bd label prefix that names a Repository the
// bead is associated with (gm-kdh3). A bead may carry many `repo:*`
// labels — one per repository it touches. The first one in the
// slice becomes the bead's [core.WorkItem.PrimaryRepositoryID]; the
// full list is preserved in order on RepositoryIDs.
//
// This convention keeps Beads as the source of truth — no schema
// change required on the bd side. Operators add a repo via:
//
//	bd update <id> --label repo:frontend
//
// and the multi-repo polecat spawn path picks it up on next read.
const labelRepoPrefix = "repo:"

// repositoriesFromLabels parses a bead's labels and returns
// (primary, all). Order from the input slice is preserved so the
// "first repo wins" convention is stable across reads.
//
// Returns (RepositoryUnspecified, nil) when no `repo:*` label is
// present; the spawn path sees this as the legacy / unbackfilled
// case and rejects polecat work with a clear error. (Empty IDs are
// silently skipped — `repo:` with no id is treated as if the label
// were absent.)
func repositoriesFromLabels(labels []string) (core.RepositoryID, []core.RepositoryID) {
	if len(labels) == 0 {
		return "", nil
	}
	var primary core.RepositoryID
	var ids []core.RepositoryID
	seen := make(map[core.RepositoryID]bool)
	for _, l := range labels {
		if !strings.HasPrefix(l, labelRepoPrefix) {
			continue
		}
		id := core.RepositoryID(strings.TrimPrefix(l, labelRepoPrefix))
		if id == "" || seen[id] {
			// Empty id, or duplicate label like
			// "repo:frontend" + "repo:frontend": skip the duplicate
			// silently. Validation lives on WorkItem.
			continue
		}
		seen[id] = true
		ids = append(ids, id)
		if primary == "" {
			primary = id
		}
	}
	return primary, ids
}

// repositoryLabels is the inverse: given a [core.WorkItem]'s
// repository fields, produce the bd label slice that encodes them.
// Used on the write path so a programmatic update preserves repo
// associations across a round-trip. Order: PrimaryRepositoryID
// first, then any other RepositoryIDs in their declared order.
func repositoryLabels(primary core.RepositoryID, ids []core.RepositoryID) []string {
	if primary == "" && len(ids) == 0 {
		return nil
	}
	out := make([]string, 0, len(ids)+1)
	emitted := make(map[core.RepositoryID]bool)
	if primary != "" {
		out = append(out, labelRepoPrefix+string(primary))
		emitted[primary] = true
	}
	for _, id := range ids {
		if id == "" || emitted[id] {
			continue
		}
		out = append(out, labelRepoPrefix+string(id))
		emitted[id] = true
	}
	return out
}

// labelBranchPrefix names the bd label that records the git branch
// a bead's work happens on, per repository (gm-ou02). Format:
// `branch:<repo>:<name>`. The `<name>` MAY contain colons (some
// branch naming schemes use them); only the first colon after the
// prefix splits repo from branch.
//
// Example: `branch:frontend:feature/gm-e3-plan-view`
const labelBranchPrefix = "branch:"

// branchesFromLabels parses a bead's labels and returns the per-
// repository branch mapping. Order from the input slice is preserved
// to keep round-trips stable. Duplicate entries for the same repo
// (e.g. `branch:frontend:a` + `branch:frontend:b`) keep the FIRST
// — [core.WorkItem.ValidateBranches] is the validator, this helper
// is permissive on read so a bd state mid-edit doesn't 500.
func branchesFromLabels(labels []string) []core.BeadBranch {
	if len(labels) == 0 {
		return nil
	}
	var out []core.BeadBranch
	seen := make(map[core.RepositoryID]bool)
	for _, l := range labels {
		if !strings.HasPrefix(l, labelBranchPrefix) {
			continue
		}
		body := strings.TrimPrefix(l, labelBranchPrefix)
		// Only the FIRST colon splits repo from branch — branch
		// names can contain colons.
		idx := strings.IndexByte(body, ':')
		if idx <= 0 || idx == len(body)-1 {
			// "branch:" / "branch:repo" / "branch:repo:" — invalid
			// shapes. Skip silently; the validator catches anything
			// that snuck onto the WorkItem from another path.
			continue
		}
		repo := core.RepositoryID(body[:idx])
		name := body[idx+1:]
		if repo == "" || name == "" || seen[repo] {
			continue
		}
		seen[repo] = true
		out = append(out, core.BeadBranch{RepositoryID: repo, Branch: name})
	}
	return out
}

// branchLabels is the inverse: given a [core.BeadBranch] slice,
// produce the bd label slice that encodes it. Used on the write
// path so a programmatic update preserves branch associations
// across a round-trip.
func branchLabels(brs []core.BeadBranch) []string {
	if len(brs) == 0 {
		return nil
	}
	out := make([]string, 0, len(brs))
	emitted := make(map[core.RepositoryID]bool)
	for _, br := range brs {
		if br.RepositoryID == "" || br.Branch == "" || emitted[br.RepositoryID] {
			continue
		}
		emitted[br.RepositoryID] = true
		out = append(out, labelBranchPrefix+string(br.RepositoryID)+":"+br.Branch)
	}
	return out
}

// labelReadPrefix is the bd label that grants a bead read access to
// a path outside the default surface (gm-v8vr). Format: `read:<glob>`.
// Multiple labels yield multiple patterns; order is preserved.
const labelReadPrefix = "read:"

// labelWritePrefix grants write access. Very rare — the operator
// MUST justify in bead notes. Format: `write:<glob>`.
const labelWritePrefix = "write:"

// readPathsFromLabels parses a bead's labels for `read:<glob>` entries
// and returns the patterns in order. Empty entries (`read:`) and
// duplicates are silently dropped; the resolver de-duplicates anyway
// but keeping the parser clean keeps the round-trip readable.
func readPathsFromLabels(labels []string) []string {
	return pathsFromLabels(labels, labelReadPrefix)
}

// writePathsFromLabels mirrors readPathsFromLabels for `write:<glob>`.
func writePathsFromLabels(labels []string) []string {
	return pathsFromLabels(labels, labelWritePrefix)
}

func pathsFromLabels(labels []string, prefix string) []string {
	if len(labels) == 0 {
		return nil
	}
	var out []string
	seen := make(map[string]bool)
	for _, l := range labels {
		if !strings.HasPrefix(l, prefix) {
			continue
		}
		body := strings.TrimPrefix(l, prefix)
		if body == "" || seen[body] {
			continue
		}
		seen[body] = true
		out = append(out, body)
	}
	return out
}

// readPathLabels is the inverse — turn a slice of glob patterns back
// into `read:<glob>` labels. Used by the write path so a programmatic
// update preserves AdditionalReadPaths across a round-trip.
func readPathLabels(paths []string) []string {
	return pathLabels(paths, labelReadPrefix)
}

// writePathLabels mirrors readPathLabels for write paths.
func writePathLabels(paths []string) []string {
	return pathLabels(paths, labelWritePrefix)
}

func pathLabels(paths []string, prefix string) []string {
	if len(paths) == 0 {
		return nil
	}
	out := make([]string, 0, len(paths))
	emitted := make(map[string]bool)
	for _, p := range paths {
		if p == "" || emitted[p] {
			continue
		}
		emitted[p] = true
		out = append(out, prefix+p)
	}
	return out
}

// prefixOf returns the substring of beadID up to (but not including)
// the first hyphen — the prefix used to route a bead to its
// [core.Repository] when no explicit `repo:*` label is set (gm-d2ts).
// Returns "" when beadID has no hyphen, which the caller treats as
// "no prefix match" and falls through to legacy behavior.
//
// Examples:
//
//	"gm-e3"     → "gm"
//	"fe-bug-44" → "fe"
//	"e3"        → ""
func prefixOf(beadID string) string {
	idx := strings.IndexByte(beadID, '-')
	if idx <= 0 {
		return ""
	}
	return beadID[:idx]
}

// stripBranchLabels returns in with every `branch:*` entry removed.
// Used on the write path so a patch carrying a new branch mapping
// authoritatively replaces stale labels rather than stacking on top.
func stripBranchLabels(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, l := range in {
		if strings.HasPrefix(l, labelBranchPrefix) {
			continue
		}
		out = append(out, l)
	}
	return out
}

// stripRepositoryLabels returns in with every `repo:*` entry removed.
// Mirrors stripAgentLabels — used on the write path so a patch
// carrying new repository fields authoritatively replaces stale
// labels rather than stacking on top of them.
func stripRepositoryLabels(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, l := range in {
		if strings.HasPrefix(l, labelRepoPrefix) {
			continue
		}
		out = append(out, l)
	}
	return out
}
