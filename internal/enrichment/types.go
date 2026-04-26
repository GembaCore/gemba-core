package enrichment

import (
	"sort"
	"strings"
	"time"
)

// Source labels how an [Enrichment] value was produced. Plain strings
// rather than an enum so future bootstrap paths (LLM extraction in
// gm-s47n.1.2, backfill in gm-s47n.1.4, vocabulary-driven inference)
// can label themselves without an enum churn.
type Source string

const (
	SourceOperator Source = "operator"
	SourceLLM      Source = "llm"
	SourceBackfill Source = "backfill"
)

// Enrichment is the per-bead targets[] + concepts[] data the planner
// reads. Stored as one JSON file per bead today; rewires to bd
// extras when gm-s47n.1.1 lands the schema.
type Enrichment struct {
	BeadID    string    `json:"bead_id"`
	Targets   []string  `json:"targets,omitempty"`
	Concepts  []string  `json:"concepts,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
	Source    Source    `json:"source,omitempty"`
}

// IsZero reports whether the enrichment carries any signal — useful
// for the CLI's "show" command to render "(no enrichment)" instead
// of an empty struct.
func (e Enrichment) IsZero() bool {
	return len(e.Targets) == 0 && len(e.Concepts) == 0
}

// AddTarget appends a target glob, returning a copy. Idempotent —
// re-adding an existing glob is a no-op. The result is sorted so
// CLI output stays diff-friendly between calls.
func (e Enrichment) AddTarget(pattern string) Enrichment {
	pattern = normalizeTarget(pattern)
	if pattern == "" {
		return e
	}
	cp := e.copy()
	cp.Targets = appendUniqueSorted(cp.Targets, pattern)
	return cp
}

// RemoveTarget drops the named target glob. No-op when not present.
func (e Enrichment) RemoveTarget(pattern string) Enrichment {
	pattern = normalizeTarget(pattern)
	cp := e.copy()
	cp.Targets = removeOne(cp.Targets, pattern)
	return cp
}

// SetTargets replaces the entire target list. Empty input clears.
// Each entry is normalized + de-duped + sorted.
func (e Enrichment) SetTargets(patterns []string) Enrichment {
	cp := e.copy()
	cp.Targets = nil
	for _, p := range patterns {
		p = normalizeTarget(p)
		if p == "" {
			continue
		}
		cp.Targets = appendUniqueSorted(cp.Targets, p)
	}
	return cp
}

// AddConcept appends a vocabulary tag. The caller is responsible
// for vocabulary validation (the CLI does it via a warning); this
// type stays free of cross-package imports so the planner can
// consume it without a vocabulary dependency.
func (e Enrichment) AddConcept(tag string) Enrichment {
	tag = normalizeConcept(tag)
	if tag == "" {
		return e
	}
	cp := e.copy()
	cp.Concepts = appendUniqueSorted(cp.Concepts, tag)
	return cp
}

// RemoveConcept drops the named tag. No-op when not present.
func (e Enrichment) RemoveConcept(tag string) Enrichment {
	tag = normalizeConcept(tag)
	cp := e.copy()
	cp.Concepts = removeOne(cp.Concepts, tag)
	return cp
}

// SetConcepts replaces the entire concept list.
func (e Enrichment) SetConcepts(tags []string) Enrichment {
	cp := e.copy()
	cp.Concepts = nil
	for _, t := range tags {
		t = normalizeConcept(t)
		if t == "" {
			continue
		}
		cp.Concepts = appendUniqueSorted(cp.Concepts, t)
	}
	return cp
}

// copy duplicates so mutator methods never alias the input slice.
// The slice headers point at fresh backing arrays.
func (e Enrichment) copy() Enrichment {
	cp := e
	if len(e.Targets) > 0 {
		cp.Targets = append([]string(nil), e.Targets...)
	}
	if len(e.Concepts) > 0 {
		cp.Concepts = append([]string(nil), e.Concepts...)
	}
	return cp
}

// normalizeTarget trims surrounding whitespace + redundant ./ + //
// runs so two writers entering "./internal/auth" and "internal/auth"
// land at the same canonical form. Glob patterns themselves pass
// through verbatim — the planner's targets package owns the
// pattern grammar.
func normalizeTarget(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = strings.TrimPrefix(s, "./")
	for strings.Contains(s, "//") {
		s = strings.ReplaceAll(s, "//", "/")
	}
	return s
}

// normalizeConcept canonicalizes a vocabulary tag to lower-kebab-case
// so the planner's concept axis can join across bead-vs-vocabulary
// without case sensitivity. Mirrors the rules in
// internal/concepts.Normalize but kept local to avoid the import
// cycle a cross-package call would introduce.
func normalizeConcept(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	prevSep := true
	for _, r := range s {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevSep = false
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
			prevSep = false
		case r == '-' || r == '_' || r == ' ' || r == '/' || r == '.' || r == ':':
			if !prevSep {
				b.WriteByte('-')
				prevSep = true
			}
		default:
			// Drop the rest (punctuation, brackets); same posture as
			// internal/concepts.Normalize.
		}
	}
	return strings.TrimRight(b.String(), "-")
}

func appendUniqueSorted(in []string, v string) []string {
	for _, x := range in {
		if x == v {
			return in
		}
	}
	out := append(in, v)
	sort.Strings(out)
	return out
}

func removeOne(in []string, v string) []string {
	out := make([]string, 0, len(in))
	for _, x := range in {
		if x == v {
			continue
		}
		out = append(out, x)
	}
	return out
}
