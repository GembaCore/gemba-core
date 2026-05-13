// Package lint implements constitution-driven lint rules.
//
// Two entry points:
//
//   - Scan(projectRoot, c) — project-wide tree scan (e.g. forbidden tasks.md
//     files). Used by `gemba constitution lint`.
//   - Lint(specPath, constitutionPath) — per-spec validation (frontmatter,
//     Story sections, AC counts). Used by `gemba spec lint`.
package lint

import (
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/GembaCore/gemba-core/internal/spec/constitution"
)

// Finding describes one lint violation. Path/Anchor identify the offending
// location: Path is a project-relative filesystem path (Scan), Anchor is a
// "file:line" or section anchor inside a spec (Lint).
type Finding struct {
	Path     string // project-relative path (project scans)
	Anchor   string // file:line or section anchor (spec lint)
	Rule     string
	Severity string // "error" | "warn"; empty for legacy Scan findings
	Message  string
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
