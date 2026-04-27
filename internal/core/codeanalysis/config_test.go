package codeanalysis

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// configTestSetup registers a "stub" backend and returns a
// cleanup that restores the registry. Most config tests need a
// non-gitnexus backend to exercise the per-repo backend
// override path.
func configTestSetup(t *testing.T) {
	t.Helper()
	resetRegistry()
	t.Cleanup(resetRegistry)
	if err := Register("stub", func() (Provider, error) {
		return stubProvider{}, nil
	}); err != nil {
		t.Fatalf("seed register: %v", err)
	}
}

func TestDecodeConfig_HappyPath(t *testing.T) {
	configTestSetup(t)
	raw := []byte(`
[code_analysis]
default_backend = "gitnexus"

[[repos]]
name = "gemba"
path = "./"
reindex_policy = "post_merge"

[[repos]]
name = "gemba_prime"
path = "../gemba_prime"
backend = "stub"

[backend.gitnexus]
embeddings = false
max_repo_size_mb = 500
`)
	c, err := DecodeConfig(raw)
	if err != nil {
		t.Fatalf("DecodeConfig: %v", err)
	}
	if c.CodeAnalysis.DefaultBackend != BackendGitNexus {
		t.Errorf("default_backend = %q, want %q",
			c.CodeAnalysis.DefaultBackend, BackendGitNexus)
	}
	if len(c.Repos) != 2 {
		t.Fatalf("len(Repos) = %d, want 2", len(c.Repos))
	}
	if c.Repos[0].ReindexPolicy != PolicyPostMerge {
		t.Errorf("repo[0].ReindexPolicy = %q, want %q",
			c.Repos[0].ReindexPolicy, PolicyPostMerge)
	}
	if got := c.Repos[1].ResolvedBackend(c.CodeAnalysis.DefaultBackend); got != "stub" {
		t.Errorf("repo[1] resolved backend = %q, want stub", got)
	}
	if got := c.Repos[0].ResolvedBackend(c.CodeAnalysis.DefaultBackend); got != BackendGitNexus {
		t.Errorf("repo[0] resolved backend = %q, want gitnexus (default fallback)", got)
	}
	gnConfig, ok := c.Backend["gitnexus"]
	if !ok {
		t.Fatal("expected backend.gitnexus block to be parsed")
	}
	if v, _ := gnConfig["embeddings"].(bool); v {
		t.Error("embeddings = true, want false")
	}
}

func TestDecodeConfig_DefaultBackendFallback(t *testing.T) {
	configTestSetup(t)
	// Omit [code_analysis] entirely; default_backend should
	// fall back to "gitnexus".
	raw := []byte(`
[[repos]]
name = "gemba"
path = "./"
`)
	c, err := DecodeConfig(raw)
	if err != nil {
		t.Fatalf("DecodeConfig: %v", err)
	}
	if c.CodeAnalysis.DefaultBackend != BackendGitNexus {
		t.Errorf("default_backend fallback = %q, want %q",
			c.CodeAnalysis.DefaultBackend, BackendGitNexus)
	}
}

func TestDecodeConfig_InvalidTOML(t *testing.T) {
	configTestSetup(t)
	raw := []byte(`this is not = "toml" [[`)
	_, err := DecodeConfig(raw)
	if err == nil {
		t.Fatal("expected decode error on malformed TOML")
	}
}

func TestDecodeConfig_UnknownDefaultBackend(t *testing.T) {
	configTestSetup(t)
	raw := []byte(`
[code_analysis]
default_backend = "no-such-backend"

[[repos]]
name = "x"
path = "./"
`)
	_, err := DecodeConfig(raw)
	if err == nil {
		t.Fatal("expected unknown-backend error")
	}
	if !errors.Is(err, ErrUnknownBackend) {
		t.Errorf("expected ErrUnknownBackend, got %v", err)
	}
}

func TestDecodeConfig_UnknownPerRepoBackend(t *testing.T) {
	configTestSetup(t)
	raw := []byte(`
[[repos]]
name = "x"
path = "./"
backend = "wat"
`)
	_, err := DecodeConfig(raw)
	if err == nil {
		t.Fatal("expected unknown per-repo backend error")
	}
	if !errors.Is(err, ErrUnknownBackend) {
		t.Errorf("expected ErrUnknownBackend, got %v", err)
	}
}

func TestDecodeConfig_DuplicateRepoNames(t *testing.T) {
	configTestSetup(t)
	raw := []byte(`
[[repos]]
name = "dup"
path = "./a"

[[repos]]
name = "dup"
path = "./b"
`)
	_, err := DecodeConfig(raw)
	if err == nil {
		t.Fatal("expected duplicate-name error")
	}
}

func TestDecodeConfig_MissingPath(t *testing.T) {
	configTestSetup(t)
	raw := []byte(`
[[repos]]
name = "x"
`)
	_, err := DecodeConfig(raw)
	if err == nil {
		t.Fatal("expected missing-path error")
	}
}

func TestDecodeConfig_MissingName(t *testing.T) {
	configTestSetup(t)
	raw := []byte(`
[[repos]]
path = "./"
`)
	_, err := DecodeConfig(raw)
	if err == nil {
		t.Fatal("expected missing-name error")
	}
}

func TestDecodeConfig_InvalidReindexPolicy(t *testing.T) {
	configTestSetup(t)
	raw := []byte(`
[[repos]]
name = "x"
path = "./"
reindex_policy = "weekly"
`)
	_, err := DecodeConfig(raw)
	if err == nil {
		t.Fatal("expected invalid-policy error")
	}
}

func TestLoadConfig_MissingFileIsEmpty(t *testing.T) {
	configTestSetup(t)
	dir := t.TempDir()
	c, err := LoadConfig(filepath.Join(dir, "does_not_exist.toml"))
	if err != nil {
		t.Fatalf("LoadConfig of missing file: %v", err)
	}
	if c == nil {
		t.Fatal("expected non-nil empty Config")
	}
	if len(c.Repos) != 0 {
		t.Errorf("expected zero repos in empty config, got %d", len(c.Repos))
	}
}

func TestLoadConfig_RoundTrip(t *testing.T) {
	configTestSetup(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "code_analysis.toml")
	raw := `
[code_analysis]
default_backend = "stub"

[[repos]]
name = "alpha"
path = "./alpha"
reindex_policy = "manual"
`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	c, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if c.CodeAnalysis.DefaultBackend != "stub" {
		t.Errorf("default_backend = %q, want stub", c.CodeAnalysis.DefaultBackend)
	}
	r, ok := c.RepoByName("alpha")
	if !ok {
		t.Fatal("RepoByName(alpha) not found")
	}
	if r.Path != "./alpha" {
		t.Errorf("Path = %q, want ./alpha", r.Path)
	}
	if r.ReindexPolicy != PolicyManual {
		t.Errorf("ReindexPolicy = %q, want %q", r.ReindexPolicy, PolicyManual)
	}
	ref := r.ToRepoRef()
	if ref.Name != "alpha" || ref.Path != "./alpha" {
		t.Errorf("ToRepoRef = %+v", ref)
	}
}
