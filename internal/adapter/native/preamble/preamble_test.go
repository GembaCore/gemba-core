package preamble

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MikeBengtson/gemba/internal/adapter/native/agents"
	"github.com/MikeBengtson/gemba/internal/core"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBuildReadsEveryLayer(t *testing.T) {
	repo := t.TempDir()
	ws := t.TempDir()
	writeFile(t, filepath.Join(repo, ".gemba", "goals.md"),
		"# Goals\n- ship the native adaptor\n- preserve optionality\n")
	writeFile(t, filepath.Join(repo, ".gemba", "values.md"),
		"- we cut scope early\n- tests are first class\n")
	writeFile(t, filepath.Join(repo, ".gemba", "guardrails.md"),
		"- no destructive ops without approval\n")
	writeFile(t, filepath.Join(ws, ".gemba", "workspace.md"),
		"- runtime cares about mock-mode specifically\n")

	item := core.WorkItem{
		ID:          "gm-foo",
		Kind:        "task",
		Title:       "test bead",
		Description: "the goal is to ship",
		Labels:      []string{"area:test"},
	}
	c := Build(Sources{RepoRoot: repo, WorkspaceDir: ws}, item)
	for _, sub := range []string{
		"ship the native adaptor",
		"tests are first class",
		"no destructive ops",
		"mock-mode specifically",
		"gm-foo",
		"the goal is to ship",
	} {
		if !strings.Contains(c.Text, sub) {
			t.Errorf("missing layer content %q in composed:\n%s", sub, c.Text)
		}
	}
}

func TestBuildSynthesizesDoDWhenBeadHasNone(t *testing.T) {
	item := core.WorkItem{ID: "gm-x", Kind: "bug"}
	c := Build(Sources{}, item)
	if !strings.Contains(c.Text, "synthesized") {
		t.Errorf("bug bead should get synthesized DoD banner: %s", c.Text)
	}
	if !strings.Contains(c.Text, "regression test") {
		t.Error("bug synthesized DoD should include regression-test criterion")
	}
}

func TestBuildPrefersOperatorDoD(t *testing.T) {
	item := core.WorkItem{
		ID: "gm-x", Kind: "task",
		DoD: &core.DefinitionOfDone{
			AcceptanceCriteria: []string{"ship it", "CI green"},
		},
	}
	c := Build(Sources{}, item)
	if strings.Contains(c.Text, "synthesized") {
		t.Error("operator DoD must NOT get synthesized banner")
	}
	if !strings.Contains(c.Text, "ship it") {
		t.Error("operator criterion missing")
	}
}

func TestApplyToClaudeMDIdempotent(t *testing.T) {
	ws := t.TempDir()
	writeFile(t, filepath.Join(ws, "CLAUDE.md"), "# operator notes\n\nhand-written content\n")
	composed := Build(Sources{}, core.WorkItem{ID: "gm-x", Kind: "task"})

	if err := ApplyToClaudeMD(ws, composed); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(filepath.Join(ws, "CLAUDE.md"))
	if err := ApplyToClaudeMD(ws, composed); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(filepath.Join(ws, "CLAUDE.md"))
	if string(first) != string(second) {
		t.Error("ApplyToClaudeMD must be idempotent with same composed text")
	}
	// Operator content preserved.
	if !strings.Contains(string(first), "operator notes") {
		t.Error("operator content wiped")
	}
	// Sentinel block present.
	if !strings.Contains(string(first), sentinelBegin) {
		t.Error("sentinel marker missing")
	}
}

func TestRemoveFromClaudeMDRestoresOriginal(t *testing.T) {
	ws := t.TempDir()
	original := "# operator notes\n\nhand-written\n"
	writeFile(t, filepath.Join(ws, "CLAUDE.md"), original)
	composed := Build(Sources{}, core.WorkItem{ID: "gm-x", Kind: "task"})
	_ = ApplyToClaudeMD(ws, composed)
	if err := RemoveFromClaudeMD(ws); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(ws, "CLAUDE.md"))
	if strings.Contains(string(got), "gemba:preamble") {
		t.Errorf("sentinel block still present after remove:\n%s", got)
	}
	if !strings.Contains(string(got), "operator notes") {
		t.Error("operator content wiped by remove")
	}
}

func TestRemoveFromClaudeMDRemovesFileIfEmptyAfterStrip(t *testing.T) {
	ws := t.TempDir()
	composed := Build(Sources{}, core.WorkItem{ID: "gm-x", Kind: "task"})
	if err := ApplyToClaudeMD(ws, composed); err != nil {
		t.Fatal(err)
	}
	if err := RemoveFromClaudeMD(ws); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(ws, "CLAUDE.md")); !os.IsNotExist(err) {
		t.Error("empty CLAUDE.md should be removed, not left as a stub")
	}
}

func TestApplyClaudeStrategyProducesFirstMessagePointer(t *testing.T) {
	ws := t.TempDir()
	composed := Build(Sources{}, core.WorkItem{ID: "gm-x", Kind: "task"})
	agent := agents.AgentType{Name: "claude", Preamble: agents.PreambleClaudeMD}
	strat, err := Apply(ws, agent, composed)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strat.FirstMessage, "CLAUDE.md") {
		t.Errorf("claude strategy should point at CLAUDE.md: %q", strat.FirstMessage)
	}
	if _, err := os.Stat(filepath.Join(ws, "CLAUDE.md")); err != nil {
		t.Errorf("CLAUDE.md should be written by Apply: %v", err)
	}
}

func TestApplyShellOnlyStrategyEmitsHeredoc(t *testing.T) {
	ws := t.TempDir()
	composed := Build(Sources{}, core.WorkItem{ID: "gm-x", Kind: "task"})
	agent := agents.AgentType{Name: "shell-only", Preamble: agents.PreambleStdoutBanner}
	strat, err := Apply(ws, agent, composed)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(strat.FirstMessage, "cat <<'") {
		t.Errorf("shell-only must emit heredoc: %q", strat.FirstMessage)
	}
	if !strings.Contains(strat.FirstMessage, "gm-x") {
		t.Error("heredoc must carry bead context")
	}
}
