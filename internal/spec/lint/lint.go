// Package lint implements constitution-driven lint rules.
//
// Two entry points:
//
//   - Scan(projectRoot, c) — project-wide tree scan (forbidden tasks.md files).
//     Used by `gemba constitution lint`.
//   - Lint(specPath, constitutionPath) — per-spec validation (frontmatter,
//     Story sections, AC counts). Used by `gemba spec lint`.
package lint

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/GembaCore/gemba-core/internal/spec/constitution"
)

// Finding describes one lint violation. Path/Anchor identify the offending
// location: Path is a project-relative filesystem path (Scan), Anchor is a
// "file:anchor" handle inside a spec (Lint).
type Finding struct {
	Path     string `json:"path,omitempty"`
	Anchor   string `json:"anchor,omitempty"`
	Rule     string `json:"rule"`
	Severity string `json:"severity,omitempty"`
	Message  string `json:"message"`
}

// Scan walks projectRoot and returns findings per the typed constitution.
//
// When the effective spec_strict_no_tasks_md is true, every tasks.md / todo.md
// is an error. Skips .git and the project's own .specify/templates directory.
func Scan(projectRoot string, c constitution.Constitution) ([]Finding, error) {
	if !c.SpecStrictNoTasksMDEffective() {
		return nil, nil
	}
	var findings []Finding
	err := filepath.WalkDir(projectRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		// Templates are allowed (the upstream Spec Kit ships one).
		rel, _ := filepath.Rel(projectRoot, path)
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(rel, ".specify/templates/") {
			return nil
		}
		base := strings.ToLower(d.Name())
		if base == "tasks.md" || base == "todo.md" {
			findings = append(findings, Finding{
				Path:    rel,
				Rule:    "spec_strict_no_tasks_md",
				Message: base + " is deprecated in ASDD mode; reconcile into bd",
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return findings, nil
}

// ScanConstitution emits findings about the constitution document at
// constitutionPath itself. Currently it implements the
// `constitution-version-mismatch` rule (warn): the document must declare a
// schema_version equal to constitution.CurrentVersion. A missing version is
// auto-injected at parse time but still surfaces a warn-level finding so
// operators know to commit the migration.
//
// constitutionPath may be empty, in which case ScanConstitution returns nil
// (nothing to check).
func ScanConstitution(constitutionPath string) ([]Finding, error) {
	if constitutionPath == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(constitutionPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	c := constitution.Parse(raw)
	var findings []Finding
	if !hasSchemaVersionLine(raw) {
		findings = append(findings, Finding{
			Path:     constitutionPath,
			Rule:     "constitution-version-mismatch",
			Severity: "warn",
			Message:  fmt.Sprintf("constitution is missing schema_version; expected %q (run 'gemba constitution lint' to migrate)", constitution.CurrentVersion),
		})
	} else if c.SchemaVersion != constitution.CurrentVersion {
		findings = append(findings, Finding{
			Path:     constitutionPath,
			Rule:     "constitution-version-mismatch",
			Severity: "warn",
			Message:  fmt.Sprintf("constitution schema_version %q does not match supported version %q", c.SchemaVersion, constitution.CurrentVersion),
		})
	}
	for _, k := range c.UnknownKeys {
		findings = append(findings, Finding{
			Path:     constitutionPath,
			Rule:     "constitution-unknown-key",
			Severity: "error",
			Message:  fmt.Sprintf("unknown constitution key %q (additionalProperties: false)", k),
		})
	}
	return findings, nil
}

// hasSchemaVersionLine reports whether the raw document explicitly declares
// schema_version. We look for the literal key prefix to avoid being fooled by
// the migrated copy of the same input.
func hasSchemaVersionLine(raw []byte) bool {
	return strings.Contains(string(raw), "schema_version:")
}
