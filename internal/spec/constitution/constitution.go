// Package constitution parses the project constitution document. The
// constitution is a Markdown file with an embedded YAML "## Config" stanza
// that controls ASDD enforcement knobs.
package constitution

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Constitution captures the typed knobs that drive enforcement.
//
// Fields are pointers so we can distinguish "unset" from "false" for
// inheritance defaults (e.g. SpecStrictNoTasksMD inherits SpecStrict).
type Constitution struct {
	ASDDMode            *bool
	SpecStrict          *bool
	SpecStrictNoTasksMD *bool
}

// SpecStrictNoTasksMDEffective returns the effective value with inheritance:
// explicit value if set, otherwise inherits SpecStrict, otherwise false.
func (c Constitution) SpecStrictNoTasksMDEffective() bool {
	if c.SpecStrictNoTasksMD != nil {
		return *c.SpecStrictNoTasksMD
	}
	if c.SpecStrict != nil {
		return *c.SpecStrict
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
		bv, ok := parseBool(val)
		if !ok {
			continue
		}
		switch key {
		case "asdd_mode":
			b := bv
			c.ASDDMode = &b
		case "spec_strict":
			b := bv
			c.SpecStrict = &b
		case "spec_strict_no_tasks_md":
			b := bv
			c.SpecStrictNoTasksMD = &b
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
