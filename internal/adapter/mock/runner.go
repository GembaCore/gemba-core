// runner.go (gm-root.28.6) — per-bead work for the mock plane.
//
// RunBead is what the mock OrchestrationPlane.StartSession calls
// after minting a Session. It:
//
//   1. Fetches the bead's title + description via `bd show --json`.
//   2. Parses leading frontmatter (template / testid / files).
//   3. Dispatches to the matching template handler (templates.go).
//      Falls back to title-keyword matching if no frontmatter.
//   4. Closes the bead via `bd close <id>`.
//   5. Emits `gemba-state bead-done --bead <id>` so the bridge
//      transitions the session to SessionReady (gm-s47n.11). Best-
//      effort; missing gemba-state binary logs and continues.
//
// Failures at any step propagate as a non-zero error so the caller
// (StartSession in start.go) can mark the session Failed.

package mock

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// RunBead is the per-bead work loop. Returns nil on success.
func RunBead(_ context.Context, projectDir, beadID string, log func(string)) error {
	if log == nil {
		log = func(string) {}
	}
	bead, err := fetchBead(projectDir, beadID)
	if err != nil {
		return fmt.Errorf("mock: fetch bead %s: %w", beadID, err)
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
	ctx := TemplateContext{
		ProjectDir: projectDir,
		BeadID:     beadID,
		Labels:     bead.Labels,
		Log:        log,
	}
	if err := handler(ctx, fm); err != nil {
		return fmt.Errorf("mock: template %q for bead %s: %w", tplName, beadID, err)
	}
	if err := closeBead(projectDir, beadID); err != nil {
		return fmt.Errorf("mock: close bead %s: %w", beadID, err)
	}
	emitBeadDone(projectDir, beadID, log)
	return nil
}

// ─── Internals ────────────────────────────────────────────────────

type beadJSON struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Labels      []string `json:"labels"`
}

func fetchBead(projectDir, beadID string) (beadJSON, error) {
	cmd := exec.Command("bd", "show", beadID, "--json")
	cmd.Dir = projectDir
	out, err := cmd.Output()
	if err != nil {
		return beadJSON{}, fmt.Errorf("bd show %s: %w", beadID, err)
	}
	var b beadJSON
	if err := json.Unmarshal(out, &b); err != nil {
		return beadJSON{}, fmt.Errorf("parse bd show %s: %w", beadID, err)
	}
	return b, nil
}

func closeBead(projectDir, beadID string) error {
	cmd := exec.Command("bd", "close", beadID,
		"-m", "Closed by mock orchestration plane (gm-root.28.6)")
	cmd.Dir = projectDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("bd close %s: %w\n%s", beadID, err, string(out))
	}
	return nil
}

func emitBeadDone(projectDir, beadID string, log func(string)) {
	cmd := exec.Command("gemba-state", "bead-done", "--bead", beadID)
	cmd.Dir = projectDir
	out, err := cmd.CombinedOutput()
	if err != nil {
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
