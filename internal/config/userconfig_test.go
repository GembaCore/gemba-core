package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadUserConfig covers the resolution and parsing of
// ~/.gemba/config.toml (gm-root.17.4, gm-root.19).

// TestLoadUserConfig_MissingFile returns a zero-value config when
// ~/.gemba/config.toml does not exist — missing file is not an error.
func TestLoadUserConfig_MissingFile(t *testing.T) {
	cfg, err := LoadUserConfig("/definitely/not/a/real/path/config.toml")
	if err != nil {
		t.Fatalf("expected nil error for missing file; got %v", err)
	}
	if cfg.Projects.DefaultDir != "" {
		t.Errorf("expected empty DefaultDir; got %q", cfg.Projects.DefaultDir)
	}
}

// TestLoadUserConfig_ParsesDefaultDir parses a valid TOML file and
// returns the [projects].default_dir value.
func TestLoadUserConfig_ParsesDefaultDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(`[projects]
default_dir = "/tmp/my-projects"
`), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadUserConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := cfg.Projects.DefaultDir, "/tmp/my-projects"; got != want {
		t.Errorf("DefaultDir = %q, want %q", got, want)
	}
}

// TestLoadUserConfig_InvalidTOML returns an error on malformed input.
func TestLoadUserConfig_InvalidTOML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(`not valid [ toml`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadUserConfig(path)
	if err == nil {
		t.Fatal("expected error for invalid TOML; got nil")
	}
}

// TestResolveDefaultDir covers the resolution order:
// 1. explicit default_dir in config
// 2. built-in default (~/gemba/projects/)

func TestResolveDefaultDir_Explicit(t *testing.T) {
	cfg := UserConfig{Projects: ProjectsConfig{DefaultDir: "/tmp/explicit"}}
	got, err := ResolveDefaultDir(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "/tmp/explicit" {
		t.Errorf("got %q, want %q", got, "/tmp/explicit")
	}
}

func TestResolveDefaultDir_RelativeIsHomeRelative(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home dir:", err)
	}
	cfg := UserConfig{Projects: ProjectsConfig{DefaultDir: "myprojects"}}
	got, err := ResolveDefaultDir(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(home, "myprojects")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveDefaultDir_BuiltinDefault(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home dir:", err)
	}
	cfg := UserConfig{}
	got, err := ResolveDefaultDir(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(home, DefaultProjectsDir)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestHasProjectUnder covers the three branches:
// 1. empty dir → no project
// 2. dir with subdirectory lacking .gemba/workspace.toml → no project
// 3. dir with subdirectory containing .gemba/workspace.toml → project found
// 4. non-existent dir → false, nil (not an error)

func TestHasProjectUnder_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	ok, err := HasProjectUnder(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("want false for empty dir; got true")
	}
}

func TestHasProjectUnder_NoWorkspaceToml(t *testing.T) {
	dir := t.TempDir()
	// Subdirectory without .gemba/workspace.toml
	if err := os.MkdirAll(filepath.Join(dir, "myproject", ".gemba"), 0o755); err != nil {
		t.Fatal(err)
	}
	ok, err := HasProjectUnder(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("want false when .gemba/ exists but workspace.toml is absent; got true")
	}
}

func TestHasProjectUnder_WithWorkspaceToml(t *testing.T) {
	dir := t.TempDir()
	gembaDir := filepath.Join(dir, "myproject", ".gemba")
	if err := os.MkdirAll(gembaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gembaDir, "workspace.toml"), []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}

	ok, err := HasProjectUnder(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("want true when workspace.toml is present; got false")
	}
}

func TestHasProjectUnder_NonExistentDir(t *testing.T) {
	ok, err := HasProjectUnder("/definitely/not/a/real/directory/gm-root-17-4")
	if err != nil {
		t.Fatalf("want nil error for missing dir; got %v", err)
	}
	if ok {
		t.Error("want false for non-existent dir; got true")
	}
}

func TestHasProjectUnder_EmptyPath(t *testing.T) {
	ok, err := HasProjectUnder("")
	if err != nil {
		t.Fatalf("want nil error for empty path; got %v", err)
	}
	if ok {
		t.Error("want false for empty path; got true")
	}
}

// TestHasProjectUnder_MultipleChildren ensures a mix of non-project and
// project children returns true as soon as one project is found.
func TestHasProjectUnder_MultipleChildren(t *testing.T) {
	dir := t.TempDir()

	// Non-project child
	if err := os.MkdirAll(filepath.Join(dir, "notaproject"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Project child
	gembaDir := filepath.Join(dir, "realproject", ".gemba")
	if err := os.MkdirAll(gembaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gembaDir, "workspace.toml"), []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}

	ok, err := HasProjectUnder(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("want true when at least one project exists; got false")
	}
}

// TestListProjectsUnder covers ListProjectsUnder (gm-root.18 shared
// primitive). Unlike HasProjectUnder it returns all entries, so we
// test ordering, content, and edge cases separately.

func TestListProjectsUnder_EmptyPath(t *testing.T) {
	projects, err := ListProjectsUnder("")
	if err != nil {
		t.Fatalf("want nil error for empty path; got %v", err)
	}
	if len(projects) != 0 {
		t.Errorf("want empty slice for empty path; got %v", projects)
	}
}

func TestListProjectsUnder_NonExistentDir(t *testing.T) {
	projects, err := ListProjectsUnder("/definitely/not/real/gm-root-18")
	if err != nil {
		t.Fatalf("want nil error for missing dir; got %v", err)
	}
	if len(projects) != 0 {
		t.Errorf("want empty slice for non-existent dir; got %v", projects)
	}
}

func TestListProjectsUnder_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	projects, err := ListProjectsUnder(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(projects) != 0 {
		t.Errorf("want empty slice for empty dir; got %v", projects)
	}
}

func TestListProjectsUnder_SkipsNonProjects(t *testing.T) {
	dir := t.TempDir()
	// A plain file — must be skipped.
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	// A subdirectory without .gemba/workspace.toml — must be skipped.
	if err := os.MkdirAll(filepath.Join(dir, "notaproject", ".gemba"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A real project.
	gembaDir := filepath.Join(dir, "realproject", ".gemba")
	if err := os.MkdirAll(gembaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gembaDir, "workspace.toml"), []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}

	projects, err := ListProjectsUnder(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("want 1 project; got %d: %v", len(projects), projects)
	}
	if projects[0].Name != "realproject" {
		t.Errorf("want Name=realproject; got %q", projects[0].Name)
	}
	if projects[0].Path != filepath.Join(dir, "realproject") {
		t.Errorf("want Path=%q; got %q", filepath.Join(dir, "realproject"), projects[0].Path)
	}
}

func TestListProjectsUnder_MultipleProjects_AlphaOrder(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"zebra", "alpha", "middle"} {
		gembaDir := filepath.Join(dir, name, ".gemba")
		if err := os.MkdirAll(gembaDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(gembaDir, "workspace.toml"), []byte(""), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	projects, err := ListProjectsUnder(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(projects) != 3 {
		t.Fatalf("want 3 projects; got %d: %v", len(projects), projects)
	}
	// ReadDir returns alphabetical order.
	names := make([]string, len(projects))
	for i, p := range projects {
		names[i] = p.Name
	}
	want := []string{"alpha", "middle", "zebra"}
	for i, w := range want {
		if names[i] != w {
			t.Errorf("position %d: want %q, got %q", i, w, names[i])
		}
	}
}

// HasProjectUnder must stay consistent with ListProjectsUnder.
func TestHasProjectUnder_DelegatesCorrectly(t *testing.T) {
	dir := t.TempDir()
	// Initially empty.
	ok, err := HasProjectUnder(dir)
	if err != nil || ok {
		t.Fatalf("want false/nil for empty dir; got ok=%v err=%v", ok, err)
	}
	// Add a project.
	gembaDir := filepath.Join(dir, "p1", ".gemba")
	if err := os.MkdirAll(gembaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gembaDir, "workspace.toml"), []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	ok, err = HasProjectUnder(dir)
	if err != nil || !ok {
		t.Fatalf("want true/nil after adding project; got ok=%v err=%v", ok, err)
	}
}

// ResolveBeadsURL tests — gm-root.19 precedence:
// CLI flag > config.toml [beads].url > built-in DefaultBeadsURL.

func TestResolveBeadsURL_CLIFlagWins(t *testing.T) {
	cfg := UserConfig{Beads: BeadsConfig{URL: "mysql://config@host:3307/db"}}
	got := ResolveBeadsURL("mysql://cli@host:3307/db", cfg)
	if want := "mysql://cli@host:3307/db"; got != want {
		t.Errorf("got %q, want %q (CLI flag must win)", got, want)
	}
}

func TestResolveBeadsURL_ConfigWinsOverDefault(t *testing.T) {
	cfg := UserConfig{Beads: BeadsConfig{URL: "mysql://config@host:3307/db"}}
	got := ResolveBeadsURL("", cfg)
	if want := "mysql://config@host:3307/db"; got != want {
		t.Errorf("got %q, want %q (config.toml must win over built-in default)", got, want)
	}
}

func TestResolveBeadsURL_BuiltInDefault(t *testing.T) {
	cfg := UserConfig{}
	got := ResolveBeadsURL("", cfg)
	if got != DefaultBeadsURL {
		t.Errorf("got %q, want DefaultBeadsURL %q", got, DefaultBeadsURL)
	}
}

func TestResolveBeadsURL_DefaultIsNonEmpty(t *testing.T) {
	if DefaultBeadsURL == "" {
		t.Fatal("DefaultBeadsURL must not be empty")
	}
}

// TestLoadUserConfig_ParsesBeadsURL verifies that [beads].url is
// populated from the TOML file (gm-root.19).
func TestLoadUserConfig_ParsesBeadsURL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	content := `[beads]
url = "mysql://myuser@10.0.0.1:3307/mydb"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadUserConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := cfg.Beads.URL, "mysql://myuser@10.0.0.1:3307/mydb"; got != want {
		t.Errorf("Beads.URL = %q, want %q", got, want)
	}
}

// RedactBeadsURL tests — credentials must never appear in logs.

func TestRedactBeadsURL_PasswordStripped(t *testing.T) {
	got := RedactBeadsURL("mysql://user:secret@host:3307/db")
	if got == "" {
		t.Fatal("got empty string")
	}
	if strings.Contains(got, "secret") {
		t.Errorf("RedactBeadsURL leaked password: %q", got)
	}
	if !strings.Contains(got, "user") {
		t.Errorf("RedactBeadsURL removed user: %q", got)
	}
}

func TestRedactBeadsURL_NoPassword(t *testing.T) {
	raw := "mysql://user@host:3307/db"
	got := RedactBeadsURL(raw)
	if got != raw {
		t.Errorf("RedactBeadsURL modified URL with no password: got %q, want %q", got, raw)
	}
}

func TestRedactBeadsURL_Unparseable(t *testing.T) {
	raw := "not-a-url"
	got := RedactBeadsURL(raw)
	if got != raw {
		t.Errorf("RedactBeadsURL modified unparseable URL: got %q, want %q", got, raw)
	}
}
