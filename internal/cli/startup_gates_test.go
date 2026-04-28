package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/MikeBengtson/gemba/internal/config"
)

// probeBd tests — the real `bd` binary may or may not be on PATH in
// the test environment, so we test the path-not-found branch only (we
// cannot force bd --version to fail without a fake binary, but we can
// confirm the install instructions are printed and a non-nil error is
// returned when the binary is missing).

// TestProbeBd_Missing verifies that when bd is not on PATH the gate
// prints install instructions to stderr and returns a non-nil error.
func TestProbeBd_Missing(t *testing.T) {
	// Override PATH to an empty directory so bd is not found.
	tmp := t.TempDir()
	old := os.Getenv("PATH")
	t.Setenv("PATH", tmp)
	defer t.Setenv("PATH", old) //nolint:usetesting // explicit restore

	var stderr bytes.Buffer
	err := probeBd(&stderr)
	if err == nil {
		t.Fatal("want non-nil error when bd is missing; got nil")
	}
	out := stderr.String()
	for _, want := range []string{
		"bd",
		"brew install",
		"pipx install",
		"https://",
	} {
		if !bytes.Contains([]byte(out), []byte(want)) {
			t.Errorf("install instructions missing %q\nfull output:\n%s", want, out)
		}
	}
}

// coldStartRedirect tests — table-driven, using a tmpdir as the
// default_dir override via a temp config.toml.

func writeTempConfig(t *testing.T, defaultDir string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	content := "[projects]\ndefault_dir = " + `"` + defaultDir + `"` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestColdStartRedirect is the primary integration test (gm-root.17.4 DoD):
//   - cold-start (bd present, empty default_dir) → redirect
//   - one-project default_dir → no redirect
func TestColdStartRedirect(t *testing.T) {
	t.Run("empty_default_dir_redirects", func(t *testing.T) {
		defaultDir := t.TempDir()
		configPath := writeTempConfig(t, defaultDir)
		cfg := config.ServeConfig{ConfigPath: configPath}
		got, err := coldStartRedirect(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !got {
			t.Error("want redirect=true for empty default_dir; got false")
		}
	})

	t.Run("one_project_no_redirect", func(t *testing.T) {
		defaultDir := t.TempDir()
		makeProjectT(t, defaultDir, "my-project")
		configPath := writeTempConfig(t, defaultDir)
		cfg := config.ServeConfig{ConfigPath: configPath}
		got, err := coldStartRedirect(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got {
			t.Error("want redirect=false when a project exists; got true")
		}
	})

	t.Run("dir_without_workspace_toml_redirects", func(t *testing.T) {
		defaultDir := t.TempDir()
		// Subdirectory that looks like a project but has no workspace.toml.
		if err := os.MkdirAll(filepath.Join(defaultDir, "fake-project"), 0o755); err != nil {
			t.Fatal(err)
		}
		configPath := writeTempConfig(t, defaultDir)
		cfg := config.ServeConfig{ConfigPath: configPath}
		got, err := coldStartRedirect(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !got {
			t.Error("want redirect=true when dir has no workspace.toml; got false")
		}
	})
}

// TestColdStartRedirect_MissingConfig verifies that a missing config
// file is not an error and uses the built-in default_dir. When the
// built-in dir does not exist either, the result is redirect=true.
func TestColdStartRedirect_MissingConfig(t *testing.T) {
	cfg := config.ServeConfig{ConfigPath: "/definitely/not/a/real/path/config.toml"}
	// The built-in default_dir (~/gemba/projects/) may or may not exist on
	// the test machine, but the function must not return an error.
	_, err := coldStartRedirect(cfg)
	if err != nil {
		t.Fatalf("want nil error for missing config; got %v", err)
	}
}

// TestColdStartRedirect_NonExistentDefaultDir verifies that a
// non-existent default_dir is treated as "no projects" (redirect=true)
// rather than an error.
func TestColdStartRedirect_NonExistentDefaultDir(t *testing.T) {
	nonexistent := filepath.Join(t.TempDir(), "does-not-exist")
	configPath := writeTempConfig(t, nonexistent)
	cfg := config.ServeConfig{ConfigPath: configPath}
	got, err := coldStartRedirect(cfg)
	if err != nil {
		t.Fatalf("want nil error for non-existent dir; got %v", err)
	}
	if !got {
		t.Error("want redirect=true for non-existent default_dir; got false")
	}
}

// makeProject helper that takes a *testing.T (for use inside table-
// driven tests where the outer t is accessible).
func makeProjectT(t *testing.T, defaultDir, name string) {
	t.Helper()
	gembaDir := filepath.Join(defaultDir, name, ".gemba")
	if err := os.MkdirAll(gembaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gembaDir, "workspace.toml"), []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
}
