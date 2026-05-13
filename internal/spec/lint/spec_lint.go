package lint

import (
	"fmt"
	"os"

	"github.com/GembaCore/gemba-core/internal/spec/constitution"
	"github.com/GembaCore/gemba-core/internal/spec/parser"
)

// Lint validates a single spec.md against the rules expressed in the
// constitution at constitutionPath. If constitutionPath is empty, the function
// returns an empty slice (no rules enabled).
//
// Each finding pins an Anchor of the form "<specPath>:<story-anchor>" or
// "<specPath>:frontmatter" so CI surfaces can render a clickable handle.
func Lint(specPath, constitutionPath string) ([]Finding, error) {
	specBytes, err := os.ReadFile(specPath)
	if err != nil {
		return nil, err
	}
	var c constitution.Constitution
	if constitutionPath != "" {
		cb, err := os.ReadFile(constitutionPath)
		if err != nil {
			return nil, err
		}
		c = constitution.Parse(cb)
	}

	// Parse via the canonical parser (gm-v0sp.3). A parse error is itself a
	// finding — we report it and bail; downstream rules need a valid AST.
	spec, perr := parser.Parse(specBytes)
	if perr != nil {
		return []Finding{{
			Anchor:   fmt.Sprintf("%s:frontmatter", specPath),
			Rule:     "spec_parse",
			Severity: "error",
			Message:  perr.Error(),
		}}, nil
	}

	var findings []Finding

	if c.RequireDecisionParentEffective() {
		if spec.Frontmatter.DecisionParent == "" {
			findings = append(findings, Finding{
				Anchor:   fmt.Sprintf("%s:frontmatter", specPath),
				Rule:     "require_decision_parent",
				Severity: "error",
				Message:  "spec frontmatter is missing required key 'decision_parent'",
			})
		}
	}

	if c.RequirePriorityEffective() {
		for _, st := range spec.Stories {
			if st.Priority == "" {
				findings = append(findings, Finding{
					Anchor:   fmt.Sprintf("%s:%s", specPath, st.Anchor),
					Rule:     "require_priority",
					Severity: "error",
					Message:  fmt.Sprintf("Story %s %q is missing 'Priority:'", st.Anchor, st.Title),
				})
			}
		}
	}

	if n := c.MinACCountEffective(); n > 0 {
		for _, st := range spec.Stories {
			if len(st.AC) < n {
				findings = append(findings, Finding{
					Anchor:   fmt.Sprintf("%s:%s", specPath, st.Anchor),
					Rule:     "min_ac_count",
					Severity: "error",
					Message:  fmt.Sprintf("Story %s has %d AC items; constitution requires >=%d", st.Anchor, len(st.AC), n),
				})
			}
		}
	}

	if c.ForbidOrphanBeadsEffective() {
		dp := spec.Frontmatter.DecisionParent
		if dp == "" {
			// require_decision_parent already reports the missing key when on;
			// add a separate finding so this rule fires independently.
			findings = append(findings, Finding{
				Anchor:   fmt.Sprintf("%s:frontmatter", specPath),
				Rule:     "forbid_orphan_beads",
				Severity: "error",
				Message:  "forbid_orphan_beads requires a decision_parent in frontmatter",
			})
		} else {
			// Real bead lookup requires a server connection. The linter is
			// offline-safe by design, so we emit a deferred-check warning
			// describing what the reconciler must verify. Severity 'warn' so
			// it does not fail strict CI; the reconciler raises 'error'.
			findings = append(findings, Finding{
				Anchor:   fmt.Sprintf("%s:frontmatter", specPath),
				Rule:     "forbid_orphan_beads",
				Severity: "warn",
				Message:  fmt.Sprintf("deferred: reconciler must verify every story bead under %s carries spec=%s", dp, spec.Frontmatter.SpecID),
			})
		}
	}

	return findings, nil
}
