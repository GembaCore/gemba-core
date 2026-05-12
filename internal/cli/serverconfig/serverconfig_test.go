package serverconfig

import (
	"os"
	"path/filepath"
	"testing"
)

// setIsolatedConfigHome points XDG_CONFIG_HOME at a temp dir for the
// duration of the test. Each test gets a clean filesystem so they
// don't fight over ~/.config/gemba/config.toml.
func setIsolatedConfigHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir) // belt-and-suspenders for tests that ignore XDG
	t.Setenv(EnvVar, "")  // start clean — no inherited GEMBA_SERVER
	return dir
}

func writeConfig(t *testing.T, xdg, url string) string {
	t.Helper()
	path := filepath.Join(xdg, "gemba", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := "[server]\nurl = \"" + url + "\"\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

// TestResolve_Flag — layer 1: an explicit flag value beats env + file
// + default. Required so `gemba bead list --server http://x` does what
// it says regardless of the environment.
func TestResolve_Flag(t *testing.T) {
	xdg := setIsolatedConfigHome(t)
	writeConfig(t, xdg, "http://from-file")
	t.Setenv(EnvVar, "http://from-env")

	got, err := Resolve("http://from-flag")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != "http://from-flag" {
		t.Fatalf("flag did not win: got %q", got)
	}
}

// TestResolve_Env — layer 2: GEMBA_SERVER overrides file + default
// when the flag is empty (or whitespace).
func TestResolve_Env(t *testing.T) {
	xdg := setIsolatedConfigHome(t)
	writeConfig(t, xdg, "http://from-file")
	t.Setenv(EnvVar, "http://from-env")

	got, err := Resolve("   ")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != "http://from-env" {
		t.Fatalf("env did not win: got %q", got)
	}
}

// TestResolve_File — layer 3: config.toml supplies the URL when no
// flag and no env var is set.
func TestResolve_File(t *testing.T) {
	xdg := setIsolatedConfigHome(t)
	writeConfig(t, xdg, "http://from-file")

	got, err := Resolve("")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != "http://from-file" {
		t.Fatalf("file did not win: got %q", got)
	}
}

// TestResolve_Default — layer 4: with nothing else set the resolver
// returns the compiled-in DefaultURL. This is the local-dev happy path.
func TestResolve_Default(t *testing.T) {
	setIsolatedConfigHome(t)
	got, err := Resolve("")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != DefaultURL {
		t.Fatalf("default did not win: got %q want %q", got, DefaultURL)
	}
}

// TestResolveDetail_Sources verifies the source string reported for
// each precedence layer. `gemba config show` depends on this to print
// the right "(source: env)" hint.
func TestResolveDetail_Sources(t *testing.T) {
	xdg := setIsolatedConfigHome(t)

	// default
	r, err := ResolveDetail("")
	if err != nil || r.Source != SourceDefault {
		t.Fatalf("default: %+v err=%v", r, err)
	}

	// file
	cfgPath := writeConfig(t, xdg, "http://from-file")
	r, err = ResolveDetail("")
	if err != nil || r.Source != SourceFile || r.File != cfgPath {
		t.Fatalf("file: %+v err=%v want path=%s", r, err, cfgPath)
	}

	// env beats file
	t.Setenv(EnvVar, "http://from-env")
	r, err = ResolveDetail("")
	if err != nil || r.Source != SourceEnv {
		t.Fatalf("env: %+v err=%v", r, err)
	}

	// flag beats env
	r, err = ResolveDetail("http://flag")
	if err != nil || r.Source != SourceFlag {
		t.Fatalf("flag: %+v err=%v", r, err)
	}
}

// TestLoadConfig_Missing — a missing config.toml is not an error.
// Important: every CLI verb calls Resolve on cold machines that have
// never run `gemba` before, so a missing file MUST be a silent layer.
func TestLoadConfig_Missing(t *testing.T) {
	setIsolatedConfigHome(t)
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig should not error on missing file: %v", err)
	}
	if cfg == nil {
		t.Fatalf("LoadConfig returned nil")
	}
	if cfg.Server.URL != "" {
		t.Fatalf("missing config should be empty, got %q", cfg.Server.URL)
	}
}

// TestLoadConfig_Malformed — a present-but-broken TOML file IS an
// error. We don't want to silently fall through to the default and
// leave the operator confused about why their config was ignored.
func TestLoadConfig_Malformed(t *testing.T) {
	xdg := setIsolatedConfigHome(t)
	path := filepath.Join(xdg, "gemba", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("not = valid = toml = at = all"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := LoadConfig(); err == nil {
		t.Fatal("expected error on malformed TOML")
	}
	if _, err := Resolve(""); err == nil {
		t.Fatal("expected Resolve to propagate malformed TOML error")
	}
}

// TestConfigPath_XDG honors XDG_CONFIG_HOME explicitly.
func TestConfigPath_XDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/custom/xdg")
	got, err := ConfigPath()
	if err != nil {
		t.Fatalf("ConfigPath: %v", err)
	}
	want := "/custom/xdg/gemba/config.toml"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

// TestConfigPath_HomeDefault falls back to $HOME/.config when XDG is
// unset.
func TestConfigPath_HomeDefault(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/tmp/fake-home")
	got, err := ConfigPath()
	if err != nil {
		t.Fatalf("ConfigPath: %v", err)
	}
	want := "/tmp/fake-home/.config/gemba/config.toml"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
