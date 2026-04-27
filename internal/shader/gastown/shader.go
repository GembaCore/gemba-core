// Package gastown implements core.Shader for the Gas Town orchestrator
// (gm-root.4). Encodes WorkItem.Title with a kind-derived prefix on
// write so the underlying adaptor (bd today, jira tomorrow) records
// the bead in Gas Town's idiomatic format. Decodes the prefix off on
// read so the SPA always sees the bare title plus a kind chip.
//
// Conventions inferred from the existing gemba rig (per gm-root.4
// design Q1: "infer from existing beads, correct later"):
//   bug      → "BUG: <title>"
//   decision → "DESIGN: <title>"
//   epic     → "[epic] <title>"
//   task     → "<title>"  (no prefix)
//
// Operators override the table via the orchestrator config file.
//
// Wisps (any id containing "wisp") bypass title encoding/decoding —
// they're non-canonical Gas Town scaffolding beads that don't follow
// the title convention (gm-root.4 design Q2).

package gastown

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"text/template"

	"github.com/MikeBengtson/gemba/core"
)

// Config carries the operator-controlled bits of the Gas Town shader.
// Loaded from .gemba/orchestrator.json. All fields are optional —
// empties default to the bead-inferred conventions below.
type Config struct {
	// Rig is the workspace identity (e.g. "gemba"). Available in
	// templates as `.rig`.
	Rig string
	// RigAbbr is the short id prefix (e.g. "gm"). Available in
	// templates as `.rig_abbr`. Used only by id_format today; the
	// title format doesn't reference it by default.
	RigAbbr string
	// IDFormat is a Go text/template that documents the id shape the
	// adaptor MUST produce. Used by future id-validation work; not
	// enforced in this slice.
	IDFormat string
	// TitleFormat is a Go text/template applied on write. Variables:
	// rig, rig_abbr, kind, kind_prefix, title. Default:
	// "{{.kind_prefix}}{{.title}}".
	TitleFormat string
	// KindPrefixes maps WorkItem.Kind → the literal string the title
	// format substitutes for {{.kind_prefix}}. Empty map → use
	// DefaultKindPrefixes below.
	KindPrefixes map[string]string
}

// DefaultKindPrefixes is the inferred Gas Town convention. Refine
// after the gm-root.5 interop test exercises real bd writes.
var DefaultKindPrefixes = map[string]string{
	"bug":      "BUG: ",
	"decision": "DESIGN: ",
	"epic":     "[epic] ",
	"task":     "",
	"chore":    "",
	"feature":  "",
}

// DefaultTitleFormat keeps the title's prose untouched and only
// prepends the kind-derived prefix.
const DefaultTitleFormat = "{{.kind_prefix}}{{.title}}"

// Shader is the Gas Town encode/decode implementation. Construct via
// New so the title template parses up front.
type Shader struct {
	cfg       Config
	titleTmpl *template.Template
	prefixes  map[string]string
}

// New returns a Shader ready for use. Returns an error only when
// TitleFormat fails to parse — a misconfigured config file should
// fail startup loudly, not silently.
func New(cfg Config) (*Shader, error) {
	if cfg.TitleFormat == "" {
		cfg.TitleFormat = DefaultTitleFormat
	}
	tmpl, err := template.New("title").Parse(cfg.TitleFormat)
	if err != nil {
		return nil, fmt.Errorf("gastown shader: title_format %q: %w", cfg.TitleFormat, err)
	}
	prefixes := cfg.KindPrefixes
	if len(prefixes) == 0 {
		prefixes = DefaultKindPrefixes
	}
	return &Shader{cfg: cfg, titleTmpl: tmpl, prefixes: prefixes}, nil
}

func (s *Shader) Describe() core.ShaderManifest {
	return core.ShaderManifest{
		Name:          "gastown",
		EncodedFields: []string{"title"},
	}
}

// EncodeForWrite prepends the kind-derived prefix to item.Title.
// Wisps and unknown kinds pass through unchanged so the shader can't
// silently corrupt a bead it has no opinion on.
func (s *Shader) EncodeForWrite(_ context.Context, _ core.WriteOp, item core.WorkItem) (core.WorkItem, error) {
	if isWisp(item.ID) {
		return item, nil
	}
	prefix, hasPrefix := s.prefixes[item.Kind]
	if !hasPrefix {
		// Unknown kind: leave the title alone. This is a softer
		// failure than rejecting the write — a future config can add
		// the missing kind without breaking existing beads.
		return item, nil
	}
	if prefix == "" {
		return item, nil
	}
	// If the title already starts with the prefix, don't double-stack
	// it. Idempotent encode means a re-import of an already-encoded
	// title doesn't end up as "BUG: BUG: …".
	if strings.HasPrefix(item.Title, prefix) {
		return item, nil
	}
	var buf bytes.Buffer
	if err := s.titleTmpl.Execute(&buf, map[string]any{
		"rig":         s.cfg.Rig,
		"rig_abbr":    s.cfg.RigAbbr,
		"kind":        item.Kind,
		"kind_prefix": prefix,
		"title":       item.Title,
	}); err != nil {
		return core.WorkItem{}, fmt.Errorf("gastown shader: title encode: %w", err)
	}
	item.Title = buf.String()
	return item, nil
}

// DecodeFromRead strips the kind-derived prefix off item.Title.
// Wisps and unknown kinds pass through unchanged; titles that don't
// start with the expected prefix also pass through (operator-renamed
// items, beads imported before the shader was active).
func (s *Shader) DecodeFromRead(_ context.Context, item core.WorkItem) (core.WorkItem, error) {
	if isWisp(item.ID) {
		return item, nil
	}
	prefix, hasPrefix := s.prefixes[item.Kind]
	if !hasPrefix || prefix == "" {
		return item, nil
	}
	item.Title = strings.TrimPrefix(item.Title, prefix)
	return item, nil
}

// isWisp recognises the gastown "wisp" scaffolding bead by id
// substring. Wisps are non-canonical (the orchestrator doesn't treat
// them as real WorkItems) so the shader leaves them alone.
func isWisp(id core.WorkItemID) bool {
	return strings.Contains(string(id), "wisp")
}
