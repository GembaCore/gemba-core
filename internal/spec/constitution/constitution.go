// Package constitution parses the project constitution document. The
// constitution is a Markdown file with an embedded YAML "## Config" stanza
// that controls ASDD enforcement knobs.
package constitution

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Constitution captures the typed knobs that drive enforcement.
//
// Bool fields are pointers so we can distinguish "unset" from "false" for
// inheritance defaults (e.g. SpecStrictNoTasksMD inherits SpecStrict).
type Constitution struct {
	ASDDMode            *bool
	SpecStrict          *bool
	SpecStrictNoTasksMD *bool

	// Spec-quality knobs (gm-v0sp.7 expansion). Bool knobs inherit SpecStrict
	// when unset; MinACCount is explicit-only.
	RequireDecisionParent *bool
	ForbidOrphanBeads     *bool
	RequirePriority       *bool
	MinACCount            *int
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

var (
	configBlockRe = regexp.MustCompile("(?s)```ya?ml\\s*\\n(.*?)```")
	kvRe          = regexp.MustCompile(`^\s*([A-Za-z_][A-Za-z0-9_]*)\s*:\s*(.+?)\s*(#.*)?$`)
)

// Parse reads constitution Markdown bytes and returns the typed config.
func Parse(data []byte) Constitution {
	c := Constitution{}
	text := string(data)
	// Locate the Config section first; we still parse any yaml fence in case
	// the config block lacks a header.
	if idx := strings.Index(text, "## Config"); idx >= 0 {
		text = text[idx:]
	}
	matches := configBlockRe.FindStringSubmatch(text)
	if matches == nil {
		return c
	}
	body := matches[1]
	for _, line := range strings.Split(body, "\n") {
		m := kvRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		key := strings.ToLower(m[1])
		val := strings.TrimSpace(strings.Trim(m[2], "\""))
		switch key {
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
	return c
}

// Load reads the constitution from a project root. It searches a small list of
// canonical paths; returns a zero-value Constitution if none exist.
func Load(projectRoot string) (Constitution, string, error) {
	candidates := []string{
		filepath.Join(projectRoot, ".gemba", "constitution.md"),
		filepath.Join(projectRoot, ".specify", "memory", "constitution.md"),
	}
	for _, p := range candidates {
		data, err := os.ReadFile(p)
		if err == nil {
			return Parse(data), p, nil
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
