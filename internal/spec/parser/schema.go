// Package parser provides parsing for the canonical ASDD Phase 2 spec.md
// format: YAML frontmatter followed by sectioned markdown.
//
// The spec document is intentionally human-authored. Anchor IDs (M-NN,
// E-NN, S-NN) are stable handles that survive renames/reorders so the
// reconciler can map them to bd beads via the lockfile.
package parser

import "time"

// Frontmatter is the typed view of the YAML frontmatter block at the top
// of a spec.md document.
type Frontmatter struct {
	SpecID           string    `yaml:"spec_id"`
	Title            string    `yaml:"title"`
	Version          string    `yaml:"version"`
	Created          time.Time `yaml:"created"`
	Owner            string    `yaml:"owner"`
	DecisionParent   string    `yaml:"decision_parent"`
	ConstitutionPath string    `yaml:"constitution_path"`
	Nonce            string    `yaml:"nonce"`
}

// Story is a single deliverable under an Epic.
type Story struct {
	Anchor   string   // e.g. "S-01"
	Title    string
	Status   string
	Parent   string
	Priority string
	AC       []string
	Body     string // remaining body text after the inline fields
}

// Epic groups stories.
type Epic struct {
	Anchor  string // e.g. "E-01"
	Title   string
	Body    string
	Stories []Story
}

// Milestone groups epics. In Phase 2 we keep a flat parse model: epics
// and stories may appear under either Milestones or top-level sections,
// and parent linkage is captured via the Story.Parent field.
type Milestone struct {
	Anchor string // e.g. "M-01"
	Title  string
	Body   string
}

// Spec is the parsed representation of a spec.md document.
type Spec struct {
	Frontmatter Frontmatter
	Goals       string
	Decisions   string
	Milestones  []Milestone
	Epics       []Epic
	Stories     []Story
}
