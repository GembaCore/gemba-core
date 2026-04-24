package install

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// fixtureSkillsFS returns a tiny in-memory FS that mirrors the
// coaching/* + manager/* layout the real bundle uses.
func fixtureSkillsFS() fs.FS {
	return fstest.MapFS{
		"coaching/walk.md":              {Data: []byte("# walk\n")},
		"manager/escalation-handler.md": {Data: []byte("# escalation\n")},
	}
}

// withSkillsFS sets the package-level SkillsFS for the duration of the
// test. Restores the previous value on cleanup so tests stay
// independent.
func withSkillsFS(t *testing.T, f fs.FS) {
	t.Helper()
	prev := SkillsFS
	SetSkillsFS(f)
	t.Cleanup(func() { SetSkillsFS(prev) })
}

func TestRegistryGetUnknown(t *testing.T) {
	if _, err := Get("space_opera"); err == nil {
		t.Fatal("want error for unknown installer")
	}
}

func TestRegistryGetClaude(t *testing.T) {
	inst, err := Get("claude")
	if err != nil {
		t.Fatal(err)
	}
	if inst.Name() != "claude" {
		t.Errorf("Name=%q, want claude", inst.Name())
	}
}

func TestRegistryGetShellOnly(t *testing.T) {
	inst, err := Get("shell_only")
	if err != nil {
		t.Fatal(err)
	}
	if inst.Name() != "shell_only" {
		t.Errorf("Name=%q, want shell_only", inst.Name())
	}
}

func TestClaudeFreshInstall(t *testing.T) {
	withSkillsFS(t, fixtureSkillsFS())
	dir := t.TempDir()
	rep, err := NewClaude().Install(context.Background(), Options{Dir: dir})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if rep.Agent != "claude" {
		t.Errorf("Agent=%q", rep.Agent)
	}
	settings := filepath.Join(dir, ".claude", "settings.local.json")
	if _, err := os.Stat(settings); err != nil {
		t.Fatalf("settings missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "CLAUDE.md")); err != nil {
		t.Fatalf("CLAUDE.md missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "skills", "coaching", "walk.md")); err != nil {
		t.Fatalf("skill missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "skills", "manager", "escalation-handler.md")); err != nil {
		t.Fatalf("manager skill missing: %v", err)
	}
}

func TestClaudeMergePreservesOperatorJSON(t *testing.T) {
	withSkillsFS(t, fixtureSkillsFS())
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(dir, ".claude", "settings.local.json")
	if err := os.WriteFile(settingsPath, []byte(`{"env":{"FOO":"bar"},"otherKey":42}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewClaude().Install(context.Background(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(settingsPath)
	var got map[string]interface{}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	env, ok := got["env"].(map[string]interface{})
	if !ok || env["FOO"] != "bar" {
		t.Errorf("operator env wiped: %+v", got["env"])
	}
	if v, _ := got["otherKey"].(float64); v != 42 {
		t.Errorf("operator otherKey wiped: %v", got["otherKey"])
	}
	if _, ok := got["hooks"]; !ok {
		t.Error("hooks not added")
	}
	if _, ok := got[SentinelKey]; !ok {
		t.Errorf("sentinel %q not added", SentinelKey)
	}
}

func TestClaudeSecondRunSkipsSentinel(t *testing.T) {
	withSkillsFS(t, fixtureSkillsFS())
	dir := t.TempDir()
	first, err := NewClaude().Install(context.Background(), Options{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(dir, ".claude", "settings.local.json")
	before, _ := os.ReadFile(settingsPath)

	second, err := NewClaude().Install(context.Background(), Options{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(settingsPath)
	if string(before) != string(after) {
		t.Error("second run rewrote settings")
	}
	// Find the settings action in second run; should be skipped.
	var settingsAction *Action
	for i, a := range second.Actions {
		if strings.HasSuffix(a.Path, "settings.local.json") {
			settingsAction = &second.Actions[i]
			break
		}
	}
	if settingsAction == nil {
		t.Fatal("no settings action recorded")
	}
	if settingsAction.Kind != "skipped" {
		t.Errorf("second-run settings Kind=%q, want skipped", settingsAction.Kind)
	}
	_ = first
}

func TestClaudeMDPreservesOperatorContent(t *testing.T) {
	withSkillsFS(t, fixtureSkillsFS())
	dir := t.TempDir()
	mdPath := filepath.Join(dir, "CLAUDE.md")
	operatorContent := "# My project\n\nOperator-authored notes.\n"
	if err := os.WriteFile(mdPath, []byte(operatorContent), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewClaude().Install(context.Background(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(mdPath)
	got := string(b)
	if !strings.Contains(got, "Operator-authored notes.") {
		t.Errorf("operator content lost: %s", got)
	}
	if !strings.Contains(got, MarkerStart) || !strings.Contains(got, MarkerEnd) {
		t.Errorf("markers missing: %s", got)
	}
}

func TestClaudeMDReplacesBlockOnly(t *testing.T) {
	withSkillsFS(t, fixtureSkillsFS())
	dir := t.TempDir()
	mdPath := filepath.Join(dir, "CLAUDE.md")
	pre := "# Before\n\n" + MarkerStart + "\nOLD CONTENT\n" + MarkerEnd + "\n\n## After\n"
	if err := os.WriteFile(mdPath, []byte(pre), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewClaude().Install(context.Background(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(mdPath)
	got := string(b)
	if strings.Contains(got, "OLD CONTENT") {
		t.Errorf("old block content not replaced: %s", got)
	}
	if !strings.Contains(got, "# Before") || !strings.Contains(got, "## After") {
		t.Errorf("surrounding bytes lost: %s", got)
	}
}

func TestClaudeSkillsPreservedWhenPresent(t *testing.T) {
	withSkillsFS(t, fixtureSkillsFS())
	dir := t.TempDir()
	dest := filepath.Join(dir, ".claude", "skills", "coaching", "walk.md")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	operatorBody := "# operator's walk\n"
	if err := os.WriteFile(dest, []byte(operatorBody), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err := NewClaude().Install(context.Background(), Options{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(dest)
	if string(b) != operatorBody {
		t.Errorf("operator skill clobbered: %s", b)
	}
	// Manager skill (not pre-existing) should still be created.
	mgr := filepath.Join(dir, ".claude", "skills", "manager", "escalation-handler.md")
	if _, err := os.Stat(mgr); err != nil {
		t.Errorf("non-conflicting bundled skill not copied: %v", err)
	}
	// Find the preserved action.
	var preservedSeen bool
	for _, a := range rep.Actions {
		if a.Kind == "preserved" && strings.HasSuffix(a.Path, "coaching/walk.md") {
			preservedSeen = true
		}
	}
	if !preservedSeen {
		t.Error("expected preserved action for operator skill")
	}
}

func TestClaudeDryRunWritesNothing(t *testing.T) {
	withSkillsFS(t, fixtureSkillsFS())
	dir := t.TempDir()
	rep, err := NewClaude().Install(context.Background(), Options{Dir: dir, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "settings.local.json")); !os.IsNotExist(err) {
		t.Error("dry-run wrote settings file")
	}
	if _, err := os.Stat(filepath.Join(dir, "CLAUDE.md")); !os.IsNotExist(err) {
		t.Error("dry-run wrote CLAUDE.md")
	}
	if len(rep.Actions) == 0 {
		t.Error("dry-run still produces an action ledger")
	}
}

func TestShellOnlyFreshInstall(t *testing.T) {
	dir := t.TempDir()
	rep, err := NewShellOnly().Install(context.Background(), Options{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Agent != "shell_only" {
		t.Errorf("Agent=%q", rep.Agent)
	}
	body, err := os.ReadFile(filepath.Join(dir, ".gemba", "shellrc"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), ShellSentinel) {
		t.Error("sentinel missing from fresh shellrc")
	}
}

func TestShellOnlyPreservesOperatorOwnedFile(t *testing.T) {
	dir := t.TempDir()
	rcPath := filepath.Join(dir, ".gemba", "shellrc")
	if err := os.MkdirAll(filepath.Dir(rcPath), 0o755); err != nil {
		t.Fatal(err)
	}
	operator := "echo operator-owned\n"
	if err := os.WriteFile(rcPath, []byte(operator), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err := NewShellOnly().Install(context.Background(), Options{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(rcPath)
	if string(got) != operator {
		t.Errorf("operator-owned shellrc clobbered: %s", got)
	}
	if len(rep.Actions) != 1 || rep.Actions[0].Kind != "preserved" {
		t.Errorf("want single preserved action, got %+v", rep.Actions)
	}
}

func TestShellOnlySecondRunSkipsWhenCurrent(t *testing.T) {
	dir := t.TempDir()
	if _, err := NewShellOnly().Install(context.Background(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	rep, err := NewShellOnly().Install(context.Background(), Options{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Actions) != 1 || rep.Actions[0].Kind != "skipped" {
		t.Errorf("want single skipped action on idempotent run, got %+v", rep.Actions)
	}
}

func TestInstallerMissingDirErrors(t *testing.T) {
	if _, err := NewClaude().Install(context.Background(), Options{}); err == nil {
		t.Error("claude: want error for missing Dir")
	}
	if _, err := NewShellOnly().Install(context.Background(), Options{}); err == nil {
		t.Error("shell_only: want error for missing Dir")
	}
}
