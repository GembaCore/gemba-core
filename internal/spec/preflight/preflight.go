// Package preflight implements `gemba spec preflight --slug <slug>`: the safety
// check the gemba-shipped /implement slash command runs before working a bead.
package preflight

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/GembaCore/gemba-core/internal/spec/constitution"
)

// Result captures preflight evaluation.
type Result struct {
	OK            bool
	Reason        string
	OffenderPaths []string
}

// Run evaluates preflight for slug under projectRoot.
func Run(projectRoot, slug string, c constitution.Constitution) Result {
	if !c.SpecStrictNoTasksMDEffective() {
		return Result{OK: true}
	}
	var offenders []string
	for _, candidate := range []string{
		filepath.Join(projectRoot, "tasks.md"),
		filepath.Join(projectRoot, "todo.md"),
		filepath.Join(projectRoot, "specs", slug, "tasks.md"),
		filepath.Join(projectRoot, "specs", slug, "todo.md"),
	} {
		if _, err := os.Stat(candidate); err == nil {
			offenders = append(offenders, candidate)
		} else if !errors.Is(err, os.ErrNotExist) {
			// Stat failed for non-ENOENT reasons; ignore — preflight is
			// best-effort. Lint covers the deeper scan.
			_ = err
		}
	}
	if len(offenders) == 0 {
		return Result{OK: true}
	}
	return Result{
		OK:            false,
		OffenderPaths: offenders,
		Reason:        "reconcile first: 'gemba spec reconcile " + slug + "' or pass --allow-tasks-md to override",
	}
}
