// Package dod synthesizes a default DefinitionOfDone when a bead
// doesn't have one declared (gm-native.11). The result is injected
// into the session preamble only — never persisted to the bead, so
// the operator's explicit intent is never silently overwritten.
package dod

import (
	"fmt"
	"sort"
	"strings"

	"github.com/MikeBengtson/gemba/core"
)

// Synthesize returns a default DoD for the given WorkItem. Never
// returns nil — callers always get at least a single "code pushed;
// CI green" criterion. Label-based refinements stack on top of the
// kind default, de-duplicated, in label-sorted order for
// determinism.
func Synthesize(item core.WorkItem) core.DefinitionOfDone {
	crit := baseCriteriaForKind(item.Kind)

	labels := append([]string(nil), item.Labels...)
	sort.Strings(labels)
	for _, l := range labels {
		for _, extra := range criteriaForLabel(l) {
			if !contains(crit, extra) {
				crit = append(crit, extra)
			}
		}
	}
	return core.DefinitionOfDone{
		AcceptanceCriteria: crit,
		Notes: fmt.Sprintf(
			"Default DoD synthesized for kind=%q based on labels (gm-native.11). "+
				"Edit the bead to override.", item.Kind),
		Version: "synthesized-v1",
	}
}

// baseCriteriaForKind returns the per-kind starter criteria. Keep
// them short and actionable; agents read these verbatim.
func baseCriteriaForKind(kind string) []string {
	switch strings.ToLower(kind) {
	case "bug":
		return []string{
			"regression test added that reproduces the bug before the fix",
			"bug fixed; CI green",
			"code pushed and commit reported",
		}
	case "decision":
		return []string{
			"decision documented in the bead description",
			"related beads updated with the decision outcome",
		}
	case "epic":
		return []string{
			"every child bead's DoD is met",
			"epic-level integration verified",
		}
	case "feature":
		return []string{
			"feature implemented per bead description",
			"tests added covering the new surface",
			"code pushed; CI green",
		}
	case "task", "":
		return []string{
			"code pushed; CI green",
			"PR merged or ready-to-merge",
		}
	default:
		return []string{
			"work completed per bead description",
			"code pushed; CI green",
		}
	}
}

// criteriaForLabel returns any label-specific additions. Missing
// labels return nil — the default from the kind already covers the
// common case.
func criteriaForLabel(label string) []string {
	switch {
	case strings.HasPrefix(label, "area:test"), label == "surface:test":
		return []string{"unit tests pass in CI"}
	case strings.Contains(label, "integration"):
		return []string{"integration tests pass in CI"}
	case strings.HasPrefix(label, "surface:frontend"):
		return []string{"SPA tests green; lint clean"}
	case strings.HasPrefix(label, "surface:backend"):
		return []string{"go test + go vet clean"}
	case strings.HasPrefix(label, "risk:high"):
		return []string{"peer review before merge"}
	}
	return nil
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// NeedsDefault returns true when the bead has no DoD (nil or zero
// acceptance criteria). Callers use this to decide whether to call
// Synthesize or respect the operator's declaration verbatim.
func NeedsDefault(item core.WorkItem) bool {
	if item.DoD == nil {
		return true
	}
	if len(item.DoD.AcceptanceCriteria) == 0 && strings.TrimSpace(item.DoD.Notes) == "" {
		return true
	}
	return false
}
