// frontmatter.go (gm-root.28.4) — port of
// testing/acceptance/temperature-spa/shared/runner/frontmatter.ts.
//
// Each task bead in the target JSONL pack starts its description with
// a triple-dash YAML-flavored block:
//
//   ---
//   template: write-component
//   testid: temperature-table, row-{c}
//   files: src/TemperatureTable.tsx
//   ---
//
//   # Goal ...
//
// We don't pull in a YAML parser; the format is a strict subset (only
// `key: value` lines, no nesting). Mirror behavior of the TS version
// so the round-trip with target-jsonl/*.jsonl.tmpl stays correct.

package mock

import (
	"regexp"
	"strings"
)

// Frontmatter holds the parsed leading block.
type Frontmatter struct {
	Template string
	TestID   []string
	Files    []string
	// Extras captures any additional `key: value` lines that the
	// templates don't explicitly consume — preserved so future
	// templates can introspect without reparsing.
	Extras map[string]string
}

var blockRe = regexp.MustCompile(`(?s)^---\s*\n(.*?)\n---\s*\n`)

// ParseFrontmatter parses the leading frontmatter block (if present).
// If no block leads the description, returns an empty Frontmatter
// (zero values + non-nil Extras).
func ParseFrontmatter(description string) Frontmatter {
	out := Frontmatter{Extras: map[string]string{}}
	if description == "" {
		return out
	}
	m := blockRe.FindStringSubmatch(description)
	if m == nil {
		return out
	}
	body := m[1]
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		idx := strings.Index(trimmed, ":")
		if idx <= 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(trimmed[:idx]))
		value := strings.TrimSpace(trimmed[idx+1:])
		if key == "" || value == "" {
			continue
		}
		switch key {
		case "template":
			out.Template = value
		case "testid":
			out.TestID = splitCSV(value)
		case "files":
			out.Files = splitCSV(value)
		default:
			out.Extras[key] = value
		}
	}
	return out
}

// StripFrontmatter returns the body without the leading block. If no
// block is present, returns description unchanged.
func StripFrontmatter(description string) string {
	if description == "" {
		return ""
	}
	return blockRe.ReplaceAllString(description, "")
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}
