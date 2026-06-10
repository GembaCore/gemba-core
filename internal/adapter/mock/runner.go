// runner.go (gm-root.28.6 + .11) — per-bead work for the mock plane.
//
// RunBead is what the mock OrchestrationPlane.StartSession calls
// after minting a Session. It:
//
//   1. Fetches the bead via the bound WorkPlaneFetcher (in-process —
//      shelling out to `bd show` would race the workplane adaptor's
//      embedded-Dolt lock).
//   2. Parses leading frontmatter (template / testid / files).
//   3. Dispatches to the matching template handler (templates.go).
//      Falls back to title-keyword matching if no frontmatter.
//   4. Closes the bead via the bound WorkPlaneFetcher (UpdateWorkItem
//      with state_category=completed).
//   5. Best-effort: emits gemba-state bead-done so a separately-running
//      bridge transitions sessions to SessionReady. Mock-mode usually
//      doesn't need this (we transition directly), but the emit is
//      kept so external observers can correlate.

package mock

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/GembaCore/gemba-core/core"
)

// RunBead executes the per-bead work loop. Returns nil on success.
func RunBead(
	ctx context.Context,
	wp WorkPlaneFetcher,
	projectDir, beadID string,
	log func(string),
) error {
	if log == nil {
		log = func(string) {}
	}
	if wp == nil {
		return fmt.Errorf("mock: WorkPlaneFetcher not configured (cli wiring missing in serve.go)")
	}
	bead, err := wp.GetWorkItem(ctx, core.WorkItemID(beadID))
	if err != nil {
		return fmt.Errorf("mock: GetWorkItem %s: %w", beadID, err)
	}
	fm := ParseFrontmatter(bead.Description)
	tplName := fm.Template
	if tplName == "" {
		tplName = matchTemplateFromTitle(bead.Title)
	}
	if tplName == "" {
		return fmt.Errorf(
			"mock: no template matches bead %s (frontmatter template=%q title=%q)",
			beadID, fm.Template, bead.Title,
		)
	}
	handler := GetTemplate(tplName)
	if handler == nil {
		return fmt.Errorf("mock: unknown template %q for bead %s", tplName, beadID)
	}
	tctx := TemplateContext{
		ProjectDir: projectDir,
		BeadID:     beadID,
		Labels:     bead.Labels,
		Log:        log,
	}
	if err := handler(tctx, fm); err != nil {
		return fmt.Errorf("mock: template %q for bead %s: %w", tplName, beadID, err)
	}
	if err := closeBead(ctx, wp, beadID); err != nil {
		return fmt.Errorf("mock: close bead %s: %w", beadID, err)
	}
	if err := closeCompletedAncestors(ctx, wp, beadID, map[core.WorkItemID]bool{}); err != nil {
		return fmt.Errorf("mock: close ancestors for bead %s: %w", beadID, err)
	}
	emitBeadDone(projectDir, beadID, log)
	return nil
}

// closeBead transitions the bead to state_category=completed via the
// in-process workplane adaptor. The bound bd workplane (gm-e6.x)
// resolves the state-map and writes through to the embedded Dolt
// without any extra subprocess.
func closeBead(ctx context.Context, wp WorkPlaneFetcher, beadID string) error {
	stateCompleted := core.StateCompleted
	patch := core.WorkItemPatch{
		StateCategory: &stateCompleted,
	}
	_, err := wp.UpdateWorkItem(ctx, core.WorkItemID(beadID), patch)
	return err
}

func closeCompletedAncestors(
	ctx context.Context,
	wp WorkPlaneFetcher,
	childID string,
	seen map[core.WorkItemID]bool,
) error {
	child, err := wp.GetWorkItem(ctx, core.WorkItemID(childID))
	if err != nil {
		return err
	}
	for _, rel := range child.Relationships {
		if rel.Kind != core.RelParentChild || rel.To != child.ID || rel.From == "" {
			continue
		}
		parentID := rel.From
		if seen[parentID] {
			continue
		}
		seen[parentID] = true
		parent, err := wp.GetWorkItem(ctx, parentID)
		if err != nil {
			return err
		}
		if isClosed(parent.StateCategory) {
			continue
		}
		allClosed, err := parentChildrenClosed(ctx, wp, parent, child.ID)
		if err != nil {
			return err
		}
		if !allClosed {
			continue
		}
		completed := core.StateCompleted
		if _, err := wp.UpdateWorkItem(ctx, parent.ID, core.WorkItemPatch{StateCategory: &completed}); err != nil {
			return err
		}
		if err := closeCompletedAncestors(ctx, wp, string(parent.ID), seen); err != nil {
			return err
		}
	}
	return nil
}

func parentChildrenClosed(ctx context.Context, wp WorkPlaneFetcher, parent core.WorkItem, justClosed core.WorkItemID) (bool, error) {
	for _, rel := range parent.Relationships {
		if rel.Kind != core.RelParentChild || rel.From != parent.ID || rel.To == "" {
			continue
		}
		if rel.To == justClosed {
			continue
		}
		child, err := wp.GetWorkItem(ctx, rel.To)
		if err != nil {
			return false, err
		}
		if !isClosed(child.StateCategory) {
			return false, nil
		}
	}
	return true, nil
}

func isClosed(cat core.StateCategory) bool {
	return cat == core.StateCompleted || cat == core.StateCanceled
}

// emitBeadDone is best-effort. The mock plane transitions sessions
// directly in start.go's runSession goroutine; this emit is for
// external observers that want to correlate via the gemba-state
// bridge (e.g. a separately-running bd hook).
func emitBeadDone(projectDir, beadID string, log func(string)) {
	cmd := exec.Command("gemba-state", "bead-done", "--bead", beadID)
	cmd.Dir = projectDir
	if out, err := cmd.CombinedOutput(); err != nil {
		log(fmt.Sprintf(
			"mock: gemba-state bead-done failed (best-effort) for %s: %v\n%s",
			beadID, err, string(out),
		))
	}
}

// matchTemplateFromTitle is the fallback when frontmatter is absent.
// More-specific keywords win (init-repo before write-component, etc.).
func matchTemplateFromTitle(title string) string {
	lower := strings.ToLower(title)
	for _, m := range matchers {
		if m.re.MatchString(lower) {
			return m.template
		}
	}
	return ""
}

type titleMatcher struct {
	re       *regexp.Regexp
	template string
}

var matchers = []titleMatcher{
	{regexp.MustCompile(`init repo|git init|package\.json|vite\.config`), "init-repo"},
	{regexp.MustCompile(`npm install|node_modules`), "npm-install"},
	{regexp.MustCompile(`test\.tsx|vitest|spec`), "write-test"},
	{regexp.MustCompile(`\.tsx|\.ts|component|temperaturerows|temperature.*table`), "write-component"},
	{regexp.MustCompile(`\bbuild\b|vite build|rebuild`), "build"},
	{regexp.MustCompile(`\bserve\b|preview`), "serve"},
	{regexp.MustCompile(`tsconfig|index\.html|main\.tsx`), "noop"},
}
