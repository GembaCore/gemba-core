package preamble

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/adapter/native/agents"
	"github.com/GembaCore/gemba-core/internal/adapter/native/claudemd"
	"github.com/GembaCore/gemba-core/internal/persona"
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

func TestBuildInjectsSelectedInteractionProfileSection(t *testing.T) {
	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, ".gemba", "interaction_profile.md"),
		"## dangerous\nNo asking.\n\n## balanced\nStop for questions.\n\n## cautious\nSurface all.\n")
	item := core.WorkItem{ID: "gm-ip", Kind: "task"}
	c := Build(Sources{
		RepoRoot:               repo,
		InteractionProfilePath: filepath.Join(repo, ".gemba", "interaction_profile.md"),
		InteractionMode:        "balanced",
	}, item)
	if !strings.Contains(c.Text, "Stop for questions") {
		t.Errorf("balanced section missing from composed preamble:\n%s", c.Text)
	}
	if strings.Contains(c.Text, "No asking.") || strings.Contains(c.Text, "Surface all.") {
		t.Errorf("non-selected sections leaked into composed preamble:\n%s", c.Text)
	}
}

func TestBuildOmitsProfileWhenUnconfigured(t *testing.T) {
	// No InteractionProfilePath + no InteractionMode → no section
	// injected, but also no error. Regression test against requiring
	// the profile for Build to succeed.
	c := Build(Sources{}, core.WorkItem{ID: "gm-x", Kind: "task"})
	if strings.Contains(c.Text, "dangerous") || strings.Contains(c.Text, "cautious") {
		t.Errorf("unconfigured Sources produced profile section:\n%s", c.Text)
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
	if !strings.Contains(string(first), claudemd.SentinelBegin) {
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

// gm-r5vz: surface section tests.

func TestBuildOmitsWorkingSurfaceWhenSurfaceNil(t *testing.T) {
	c := Build(Sources{}, core.WorkItem{ID: "gm-x", Kind: "task"})
	if strings.Contains(c.Text, "Working surface") {
		t.Errorf("nil Surface MUST NOT emit a section; got:\n%s", c.Text)
	}
}

func TestBuildRendersSurfaceSectionForRepoScope(t *testing.T) {
	s := &persona.Surface{
		Cwd:               "/work/repo-a",
		SiblingReads:      []string{"/work/repo-b"},
		WorkspaceMetadata: "/work/.gemba",
		ToolingPaths:      []string{"$HOME/.gitconfig", "$HOME/.cargo"},
	}
	c := Build(Sources{
		Surface:    s,
		SurfaceEnv: map[string]string{"HOME": "/home/mike"},
		Repository: "repo-a",
		Branch:     "feat/gm-r5vz",
	}, core.WorkItem{ID: "gm-x", Kind: "task"})

	for _, sub := range []string{
		"## Working surface",
		"You are working in: /work/repo-a",
		"Repository: repo-a",
		"Branch: feat/gm-r5vz",
		"You may write inside /work/repo-a.",
		"Additional write paths: none.",
		"sibling repos in this workspace: /work/repo-b",
		"workspace metadata at /work/.gemba",
		"/home/mike/.gitconfig",
		"/home/mike/.cargo",
		"additional read paths: none",
		"ask the operator via gemba-ask",
	} {
		if !strings.Contains(c.Text, sub) {
			t.Errorf("missing %q in surface section:\n%s", sub, c.Text)
		}
	}
	// Env-expanded paths must replace the literal $VAR — a leaked
	// "$HOME" string would mean ExpandPaths didn't run.
	if strings.Contains(c.Text, "$HOME") {
		t.Errorf("unexpanded $HOME leaked into rendered preamble:\n%s", c.Text)
	}
}

func TestBuildRendersProjectScopePlaceholderWhenNoRepo(t *testing.T) {
	s := &persona.Surface{Cwd: "/work"}
	c := Build(Sources{Surface: s}, core.WorkItem{ID: "gm-x", Kind: "task"})
	if !strings.Contains(c.Text, "Repository: (workspace root, no single repo)") {
		t.Errorf("project-scope spawn must surface placeholder; got:\n%s", c.Text)
	}
}

func TestBuildRendersNoBranchPlaceholderForConsult(t *testing.T) {
	s := &persona.Surface{Cwd: "/work/repo-a"}
	c := Build(Sources{Surface: s, Repository: "repo-a"}, core.WorkItem{ID: "gm-x", Kind: "task"})
	if !strings.Contains(c.Text, "Branch: no branch — read-only consult") {
		t.Errorf("missing branch must render the consult placeholder; got:\n%s", c.Text)
	}
}

func TestBuildRendersAdditionalReadAndWritePaths(t *testing.T) {
	s := &persona.Surface{
		Cwd:              "/work/repo-a",
		AdditionalWrites: []string{"/var/log/gemba/sessions"},
		AdditionalReads:  []string{"$HOME/.aws/credentials", "/etc/myapp.conf"},
	}
	c := Build(Sources{
		Surface:    s,
		SurfaceEnv: map[string]string{"HOME": "/home/mike"},
		Repository: "repo-a",
		Branch:     "main",
	}, core.WorkItem{ID: "gm-x", Kind: "task"})

	if !strings.Contains(c.Text, "Additional write paths: /var/log/gemba/sessions.") {
		t.Errorf("additional write paths missing:\n%s", c.Text)
	}
	if !strings.Contains(c.Text, "additional read paths: /home/mike/.aws/credentials, /etc/myapp.conf") {
		t.Errorf("additional read paths missing or unexpanded:\n%s", c.Text)
	}
}

func TestBuildExpandsToolingPaths(t *testing.T) {
	s := &persona.Surface{
		Cwd:          "/work/repo-a",
		ToolingPaths: []string{"$HOME/.gitconfig", "$GOPATH/pkg/mod"},
	}
	c := Build(Sources{
		Surface: s,
		// Intentionally omit GOPATH so persona.ExpandPaths drops the
		// $GOPATH entry — the model should never see a path with an
		// unset envvar (would be a relative path on the daemon).
		SurfaceEnv: map[string]string{"HOME": "/home/mike"},
		Repository: "repo-a",
		Branch:     "main",
	}, core.WorkItem{ID: "gm-x", Kind: "task"})
	if !strings.Contains(c.Text, "/home/mike/.gitconfig") {
		t.Errorf("HOME-rooted tooling path not expanded:\n%s", c.Text)
	}
	if strings.Contains(c.Text, "$GOPATH") {
		t.Errorf("unexpanded $GOPATH leaked despite missing env value:\n%s", c.Text)
	}
}

func TestApplyToClaudeMD_SurfaceRoundTrips(t *testing.T) {
	// Round-trip: Apply with surface → CLAUDE.md has section; Remove
	// → CLAUDE.md returns to its pre-Apply byte-identical state.
	// Existing tests already cover the operator-content case; this one
	// pins the new surface section.
	ws := t.TempDir()
	original := "# operator notes\n\nhand-written\n"
	writeFile(t, filepath.Join(ws, "CLAUDE.md"), original)

	composed := Build(Sources{
		Surface:    &persona.Surface{Cwd: "/work/repo-a"},
		Repository: "repo-a",
		Branch:     "main",
	}, core.WorkItem{ID: "gm-x", Kind: "task"})
	if err := ApplyToClaudeMD(ws, composed); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(ws, "CLAUDE.md"))
	if !strings.Contains(string(after), "## Working surface") {
		t.Error("surface section missing after Apply")
	}

	if err := RemoveFromClaudeMD(ws); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(ws, "CLAUDE.md"))
	if string(got) != original {
		t.Errorf("CLAUDE.md not byte-identical after Remove; got:\n%q\nwant:\n%q", got, original)
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
