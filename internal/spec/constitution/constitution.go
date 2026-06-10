// Package constitution parses the project constitution document. The
// constitution is a Markdown file with knobs supplied either via a top-level
// YAML frontmatter block (`--- ... ---`) and/or an embedded `## Config` yaml
// fenced block. Keys are merged with frontmatter taking precedence.
//
// The schema is defined in internal/spec/schema/constitution.schema.json and
// versioned via CurrentVersion (see version.go).
package constitution

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// ErrUnknownConstitutionKey is returned when a constitution declares a
// top-level key the schema does not recognize (additionalProperties: false).
var ErrUnknownConstitutionKey = errors.New("constitution: unknown top-level key")

// Constitution captures the typed knobs that drive enforcement.
//
// Bool fields are pointers so we can distinguish "unset" from "false" for
// inheritance defaults (e.g. SpecStrictNoTasksMD inherits SpecStrict).
type Constitution struct {
	// SchemaVersion is the declared schema version. Always set after Load /
	// Parse; defaults to CurrentVersion when migration injected it.
	SchemaVersion string

	ASDDMode            *bool
	SpecStrict          *bool
	SpecStrictNoTasksMD *bool

	// Spec-quality knobs (gm-v0sp.7 expansion). Bool knobs inherit SpecStrict
	// when unset; MinACCount is explicit-only.
	RequireDecisionParent *bool
	ForbidOrphanBeads     *bool
	RequirePriority       *bool
	MinACCount            *int

	// UnknownKeys lists top-level keys the parser saw but the schema does
	// not catalog. Surface via lint as constitution-version-mismatch or as
	// ErrUnknownConstitutionKey for callers that need strict rejection.
	UnknownKeys []string
}

// SpecStrictNoTasksMDEffective returns the effective value with inheritance:
// explicit value if set, otherwise inherits SpecStrict, otherwise false.
func (c Constitution) SpecStrictNoTasksMDEffective() bool {
	return effectiveBool(c.SpecStrictNoTasksMD, c.SpecStrict)
}

// RequireDecisionParentEffective: explicit, else inherits SpecStrict, else false.
func (c Constitution) RequireDecisionParentEffective() bool {
	return effectiveBool(c.RequireDecisionParent, c.SpecStrict)
}

// ForbidOrphanBeadsEffective: explicit, else inherits SpecStrict, else false.
func (c Constitution) ForbidOrphanBeadsEffective() bool {
	return effectiveBool(c.ForbidOrphanBeads, c.SpecStrict)
}

// RequirePriorityEffective: explicit, else inherits SpecStrict, else false.
func (c Constitution) RequirePriorityEffective() bool {
	return effectiveBool(c.RequirePriority, c.SpecStrict)
}

// MinACCountEffective returns the explicit value when set, otherwise 0.
func (c Constitution) MinACCountEffective() int {
	if c.MinACCount != nil {
		return *c.MinACCount
	}
	return 0
}

func effectiveBool(explicit, inherit *bool) bool {
	if explicit != nil {
		return *explicit
	}
	if inherit != nil {
		return *inherit
	}
	return false
}

// ASDDModeEffective returns the effective ASDD mode flag.
func (c Constitution) ASDDModeEffective() bool {
	if c.ASDDMode != nil {
		return *c.ASDDMode
	}
	return false
}

// knownKeys lists every top-level key catalogued by the schema. Keep in sync
// with internal/spec/schema/constitution.schema.json.
var knownKeys = map[string]struct{}{
	"schema_version":          {},
	"asdd_mode":               {},
	"spec_strict":             {},
	"spec_strict_no_tasks_md": {},
	"require_decision_parent": {},
	"forbid_orphan_beads":     {},
	"min_ac_count":            {},
	"require_priority":        {},
}

var (
	configBlockRe = regexp.MustCompile("(?s)```ya?ml\\s*\\n(.*?)```")
	kvRe          = regexp.MustCompile(`^\s*([A-Za-z_][A-Za-z0-9_]*)\s*:\s*(.+?)\s*(#.*)?$`)
	frontmatterRe = regexp.MustCompile(`(?s)\A\s*---\r?\n(.*?)\r?\n---\s*(?:\r?\n|$)`)
	versionLineRe = regexp.MustCompile(`(?m)^\s*schema_version\s*:\s*["']?([0-9]+\.[0-9]+\.[0-9]+)["']?\s*$`)
)

// Parse reads constitution Markdown bytes and returns the typed config.
//
// Parse runs MigrateConstitution on input first, so callers can rely on
// c.SchemaVersion being non-empty for any non-empty input. Unknown top-level
// keys are recorded in c.UnknownKeys (the linter / Load decide whether to
// promote them to an error).
func Parse(data []byte) Constitution {
	c := Constitution{}
	migrated, err := MigrateConstitution(data)
	if err != nil {
		// Migration only fails for an unsupported version; surface the raw
		// version we read so SchemaVersion reflects truth.
		if m := versionLineRe.FindSubmatch(data); m != nil {
			c.SchemaVersion = string(m[1])
		}
		return c
	}
	text := string(migrated)

	// 1) Frontmatter block at the top of the file (if any).
	if fm := frontmatterRe.FindStringSubmatch(text); fm != nil {
		applyKVBlock(&c, fm[1])
	}

	// 2) '## Config' yaml fence (legacy form, still supported).
	if idx := strings.Index(text, "## Config"); idx >= 0 {
		sub := text[idx:]
		if m := configBlockRe.FindStringSubmatch(sub); m != nil {
			applyKVBlock(&c, m[1])
		}
	}

	if c.SchemaVersion == "" {
		c.SchemaVersion = CurrentVersion
	}
	return c
}

// applyKVBlock parses a YAML-shaped key/value block (one key per line) and
// mutates c. Unknown top-level keys are recorded on c.UnknownKeys; recognised
// keys overwrite previously-set fields so callers can layer blocks in
// priority order (frontmatter then ## Config).
func applyKVBlock(c *Constitution, body string) {
	for _, line := range strings.Split(body, "\n") {
		m := kvRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		key := strings.ToLower(m[1])
		val := strings.TrimSpace(strings.Trim(m[2], "\""))
		val = strings.TrimSpace(strings.Trim(val, "'"))

		if _, ok := knownKeys[key]; !ok {
			c.UnknownKeys = append(c.UnknownKeys, key)
			continue
		}

		switch key {
		case "schema_version":
			c.SchemaVersion = val
		case "asdd_mode", "spec_strict", "spec_strict_no_tasks_md",
			"require_decision_parent", "forbid_orphan_beads", "require_priority":
			bv, ok := parseBool(val)
			if !ok {
				continue
			}
			b := bv
			switch key {
			case "asdd_mode":
				c.ASDDMode = &b
			case "spec_strict":
				c.SpecStrict = &b
			case "spec_strict_no_tasks_md":
				c.SpecStrictNoTasksMD = &b
			case "require_decision_parent":
				c.RequireDecisionParent = &b
			case "forbid_orphan_beads":
				c.ForbidOrphanBeads = &b
			case "require_priority":
				c.RequirePriority = &b
			}
		case "min_ac_count":
			n, err := strconv.Atoi(val)
			if err != nil || n < 0 {
				continue
			}
			c.MinACCount = &n
		}
	}
}

// Load reads the constitution from a project root. It searches a small list of
// canonical paths; returns a zero-value Constitution if none exist.
//
// Load enforces additionalProperties: false: when the constitution declares
// keys not recognised by the schema, Load returns ErrUnknownConstitutionKey
// (wrapped so callers can inspect via errors.Is).
func Load(projectRoot string) (Constitution, string, error) {
	candidates := []string{
		filepath.Join(projectRoot, ".gemba", "constitution.md"),
		filepath.Join(projectRoot, ".specify", "memory", "constitution.md"),
	}
	for _, p := range candidates {
		data, err := os.ReadFile(p)
		if err == nil {
			c := Parse(data)
			if len(c.UnknownKeys) > 0 {
				return c, p, fmt.Errorf("%w: %v (in %s)", ErrUnknownConstitutionKey, c.UnknownKeys, p)
			}
			return c, p, nil
		}
		if !os.IsNotExist(err) {
			return Constitution{}, "", err
		}
	}
	return Constitution{}, "", nil
}

func parseBool(s string) (bool, bool) {
	switch strings.ToLower(s) {
	case "true", "yes", "on", "1":
		return true, true
	case "false", "no", "off", "0":
		return false, true
	}
	return false, false
}
