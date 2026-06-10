package constitution

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
)

// CurrentVersion is the active constitution schema version. Future schema
// changes bump this and add a branch in MigrateConstitution.
const CurrentVersion = "1.0.0"

// ErrUnsupportedConstitutionVersion is returned by MigrateConstitution when
// the raw document declares a schema_version this build cannot read.
var ErrUnsupportedConstitutionVersion = fmt.Errorf("constitution: unsupported schema_version (this build understands %s)", CurrentVersion)

// schemaVersionRe matches a YAML key line of the form `schema_version: "1.2.3"`
// or `schema_version: 1.2.3` (quotes optional). It is intentionally limited to
// top-of-line YAML; we do not parse the whole document just to find the key.
var schemaVersionRe = regexp.MustCompile(`(?m)^\s*schema_version\s*:\s*["']?([0-9]+\.[0-9]+\.[0-9]+)["']?\s*$`)

// MigrateConstitution returns a version-normalized copy of raw. If the input
// declares no schema_version, MigrateConstitution injects schema_version =
// CurrentVersion into the top-most YAML region (frontmatter if present, else
// the '## Config' yaml fence). When schema_version is present, it must equal
// CurrentVersion; otherwise ErrUnsupportedConstitutionVersion is returned.
//
// Migration is idempotent: calling MigrateConstitution on its own output is a
// no-op modulo whitespace.
func MigrateConstitution(raw []byte) ([]byte, error) {
	if m := schemaVersionRe.FindSubmatch(raw); m != nil {
		got := string(m[1])
		if got != CurrentVersion {
			return nil, fmt.Errorf("%w: got %q", ErrUnsupportedConstitutionVersion, got)
		}
		return raw, nil
	}

	// No schema_version present — inject it. Prefer YAML frontmatter at the
	// very top (`---\n...\n---`). If none, fall back to the '## Config' yaml
	// fence. If neither exists, prepend a fresh frontmatter block.
	if out, ok := injectIntoFrontmatter(raw); ok {
		return out, nil
	}
	if out, ok := injectIntoConfigFence(raw); ok {
		return out, nil
	}
	// Last resort: prepend a frontmatter block.
	header := []byte("---\nschema_version: " + CurrentVersion + "\n---\n")
	return append(header, raw...), nil
}

// injectIntoFrontmatter writes `schema_version: X.Y.Z` into a leading
// `---` YAML frontmatter block. Returns (out, true) when one is found.
func injectIntoFrontmatter(raw []byte) ([]byte, bool) {
	text := string(raw)
	// Allow a leading HTML comment / blank lines before the frontmatter.
	trimmed := strings.TrimLeft(text, " \t\r\n")
	prefixLen := len(text) - len(trimmed)
	if !strings.HasPrefix(trimmed, "---\n") && !strings.HasPrefix(trimmed, "---\r\n") {
		return nil, false
	}
	// Find the closing `---` on its own line after the opening fence.
	body := trimmed[4:]
	closeIdx := strings.Index(body, "\n---")
	if closeIdx < 0 {
		return nil, false
	}
	fmBody := body[:closeIdx]
	rest := body[closeIdx:]
	insert := "schema_version: " + CurrentVersion + "\n"
	// Prepend the version line inside the frontmatter.
	newFM := insert + fmBody
	var buf bytes.Buffer
	buf.WriteString(text[:prefixLen])
	buf.WriteString("---\n")
	buf.WriteString(newFM)
	buf.WriteString(rest)
	return buf.Bytes(), true
}

// injectIntoConfigFence writes `schema_version: X.Y.Z` into the first
// ```yaml fenced block following a '## Config' heading.
func injectIntoConfigFence(raw []byte) ([]byte, bool) {
	text := string(raw)
	cfgIdx := strings.Index(text, "## Config")
	if cfgIdx < 0 {
		return nil, false
	}
	// Locate the next yaml fence after the heading.
	rest := text[cfgIdx:]
	fenceRe := regexp.MustCompile("(?s)```ya?ml\\s*\\n(.*?)```")
	loc := fenceRe.FindStringSubmatchIndex(rest)
	if loc == nil {
		return nil, false
	}
	bodyStart := loc[2] // start of capture group 1 (yaml body)
	insert := "schema_version: " + CurrentVersion + "\n"
	var buf bytes.Buffer
	buf.WriteString(text[:cfgIdx+bodyStart])
	buf.WriteString(insert)
	buf.WriteString(text[cfgIdx+bodyStart:])
	return buf.Bytes(), true
}
