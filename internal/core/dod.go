// Package core: see doc.go for the overview.
package core

// DefinitionOfDone is the informational-only DoD surface: the list of
// acceptance criteria, human notes, and a schema-ish version string.
//
// "Informational-only" is a gm-root DoD rule and a locked architectural
// decision: Gemba never blocks a state transition because a DoD item is
// unchecked. The UI may render the checklist and surface drift, but the
// operator (or the adaptor's own rules) decides.
//
// Version is a free-form tag adaptors set when the shape of their DoD
// template changes (e.g. "v2", "2026-04"). The UI uses it to avoid
// rendering stale criteria against a newer work item.
type DefinitionOfDone struct {
	AcceptanceCriteria []string `json:"acceptance_criteria"`
	Notes              string   `json:"notes,omitempty"`
	Version            string   `json:"version,omitempty"`
}
