package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/GembaCore/gemba-core/internal/config"
)

// TestApplyPromURLDefault covers the gm-e9m0 resolver: CLI > env >
// [metrics].prom_url > "" (unconfigured is allowed).
func TestApplyPromURLDefault(t *testing.T) {
	t.Setenv("PROM_URL", "")

	t.Run("CLI wins outright", func(t *testing.T) {
		t.Setenv("PROM_URL", "http://env:9090")
		cfg := config.ServeConfig{PromURL: "http://cli:9090"}
		if err := applyPromURLDefault(&cfg); err != nil {
			t.Fatalf("unexpected: %v", err)
		}
		if cfg.PromURL != "http://cli:9090" {
			t.Errorf("CLI should win; got %q", cfg.PromURL)
		}
	})

	t.Run("env wins when CLI empty", func(t *testing.T) {
		t.Setenv("PROM_URL", "http://env:9090")
		cfg := config.ServeConfig{ConfigPath: "/no/such/file"}
		if err := applyPromURLDefault(&cfg); err != nil {
			t.Fatalf("unexpected: %v", err)
		}
		if cfg.PromURL != "http://env:9090" {
			t.Errorf("env should populate; got %q", cfg.PromURL)
		}
	})

	t.Run("config file wins when CLI and env empty", func(t *testing.T) {
		t.Setenv("PROM_URL", "")
		dir := t.TempDir()
		path := filepath.Join(dir, "config.toml")
		if err := os.WriteFile(path, []byte(`[metrics]
prom_url = "http://from-config:9090"
`), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg := config.ServeConfig{ConfigPath: path}
		if err := applyPromURLDefault(&cfg); err != nil {
			t.Fatalf("unexpected: %v", err)
		}
		if cfg.PromURL != "http://from-config:9090" {
			t.Errorf("config should populate; got %q", cfg.PromURL)
		}
	})

	t.Run("unconfigured leaves empty without error", func(t *testing.T) {
		t.Setenv("PROM_URL", "")
		cfg := config.ServeConfig{ConfigPath: "/no/such/file"}
		if err := applyPromURLDefault(&cfg); err != nil {
			t.Fatalf("unexpected: %v", err)
		}
		if cfg.PromURL != "" {
			t.Errorf("expected empty PromURL, got %q", cfg.PromURL)
		}
	})
}

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

// applyBeadsURLDefault tests — gm-root.19 precedence:
// CLI flag > config.toml [beads].url > built-in DefaultBeadsURL.

// TestApplyBeadsURLDefault_CLIFlagWins ensures --dolt-url on the CLI
// is never overwritten.
func TestApplyBeadsURLDefault_CLIFlagWins(t *testing.T) {
	cfg := config.ServeConfig{DoltURL: "mysql://root@127.0.0.1:3307/mydb"}
	if err := applyBeadsURLDefault(&cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := cfg.DoltURL, "mysql://root@127.0.0.1:3307/mydb"; got != want {
		t.Errorf("DoltURL = %q, want %q (CLI flag must win)", got, want)
	}
}

// TestApplyBeadsURLDefault_BeadsDirWins ensures --beads-dir is not
// overwritten — the bd-CLI path takes priority over the dolt path.
func TestApplyBeadsURLDefault_BeadsDirWins(t *testing.T) {
	cfg := config.ServeConfig{BeadsDir: "/tmp/myrig"}
	if err := applyBeadsURLDefault(&cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DoltURL != "" {
		t.Errorf("DoltURL must stay empty when BeadsDir is set; got %q", cfg.DoltURL)
	}
}

// TestApplyBeadsURLDefault_NoopWins ensures --noop is not overridden.
func TestApplyBeadsURLDefault_NoopWins(t *testing.T) {
	cfg := config.ServeConfig{Noop: true}
	if err := applyBeadsURLDefault(&cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DoltURL != "" {
		t.Errorf("DoltURL must stay empty when Noop is set; got %q", cfg.DoltURL)
	}
}

// TestApplyBeadsURLDefault_ConfigTOMLURL verifies that [beads].url in the
// config file takes precedence over the built-in default.
func TestApplyBeadsURLDefault_ConfigTOMLURL(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	content := `[beads]
url = "mysql://user@10.0.0.1:3307/mydb"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.ServeConfig{ConfigPath: cfgPath}
	if err := applyBeadsURLDefault(&cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := cfg.DoltURL, "mysql://user@10.0.0.1:3307/mydb"; got != want {
		t.Errorf("DoltURL = %q, want %q (config.toml must win over built-in default)", got, want)
	}
}

// TestApplyBeadsURLDefault_BuiltInFallback verifies that when neither CLI
// flag nor config.toml provides a URL, the built-in DefaultBeadsURL is used.
func TestApplyBeadsURLDefault_BuiltInFallback(t *testing.T) {
	// Use a non-existent config path to force the built-in default.
	cfg := config.ServeConfig{ConfigPath: filepath.Join(t.TempDir(), "missing.toml")}
	if err := applyBeadsURLDefault(&cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := cfg.DoltURL, config.DefaultBeadsURL; got != want {
		t.Errorf("DoltURL = %q, want %q (built-in default must apply when nothing else is set)", got, want)
	}
}

// TestApplyBeadsURLDefault_MalformedConfigReturnsError ensures that a
// malformed config.toml surfaces an error rather than silently falling
// back to the default — protecting the operator from a misconfigured file
// being silently ignored.
func TestApplyBeadsURLDefault_MalformedConfigReturnsError(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte("not valid [ toml"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.ServeConfig{ConfigPath: cfgPath}
	err := applyBeadsURLDefault(&cfg)
	if err == nil {
		t.Fatal("want error for malformed config.toml, got nil")
	}
}
