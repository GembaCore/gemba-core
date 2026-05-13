// Package lint implements `gemba constitution lint` rules. The current rule
// set scans for stray tasks.md / todo.md files when the constitution sets
// spec_strict_no_tasks_md.
package lint

import (
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/GembaCore/gemba-core/internal/spec/constitution"
)

// Finding describes one lint violation.
type Finding struct {
	Path    string
	Rule    string
	Message string
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
