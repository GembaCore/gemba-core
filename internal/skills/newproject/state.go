package newproject

import "fmt"

// SweepDanglingRefs walks every bead's DependsOnRefs / BlocksRefs
// and removes entries that no longer resolve to a real bead in
// the tree. Returns the number of refs removed.
//
// Use case: when the operator deletes a milestone or epic via the
// SPA's plan-preview pane (an in-place edit, bypassing the
// skill), beads that pointed at the deleted node carry stale
// refs into the next turn. The host can call this after applying
// the in-place edit and before submitting the next turn so the
// model isn't re-prompted with a state ValidateState would
// reject.
//
// SweepDanglingRefs mutates s in place. It is safe to call on a
// state that has no dangling refs (returns 0). It does NOT touch
// LastChange -- that's the skill's job.
func SweepDanglingRefs(s *NewProjectState) int {
	if s == nil {
		return 0
	}
	paths := buildPathSet(s)
	removed := 0
	for mi := range s.Milestones {
		for ei := range s.Milestones[mi].Epics {
			for bi := range s.Milestones[mi].Epics[ei].Beads {
				b := &s.Milestones[mi].Epics[ei].Beads[bi]
				selfRef := fmt.Sprintf("milestone:%d/epic:%d/bead:%d", mi, ei, bi)
				b.DependsOnRefs, removed = filterRefs(b.DependsOnRefs, selfRef, paths, removed)
				b.BlocksRefs, removed = filterRefs(b.BlocksRefs, selfRef, paths, removed)
			}
		}
	}
	return removed
}

// buildPathSet builds the set of legal bead paths in the tree.
// Internal helper so SweepDanglingRefs and ValidateState use the
// exact same notion of "what counts as a real bead".
func buildPathSet(s *NewProjectState) map[string]struct{} {
	paths := make(map[string]struct{})
	for mi, m := range s.Milestones {
		for ei, e := range m.Epics {
			for bi := range e.Beads {
				paths[fmt.Sprintf("milestone:%d/epic:%d/bead:%d", mi, ei, bi)] = struct{}{}
			}
		}
	}
	return paths
}

// filterRefs returns a slice that drops self-refs and any ref
// not in paths. The removed counter is threaded so the caller
// can report the total swept across the whole tree.
func filterRefs(refs []string, selfRef string, paths map[string]struct{}, removed int) ([]string, int) {
	if len(refs) == 0 {
		return refs, removed
	}
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		if r == selfRef {
			removed++
			continue
		}
		if _, ok := paths[r]; !ok {
			removed++
			continue
		}
		out = append(out, r)
	}
	return out, removed
}
