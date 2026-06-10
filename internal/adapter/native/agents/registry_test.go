package agents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDefault(t *testing.T) {
	r, err := Load(filepath.Join("testdata", "agents.toml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, want := len(r.Agents), 2; got != want {
		t.Fatalf("agents count: want %d got %d", want, got)
	}
	a, ok := r.Get("claude")
	if !ok {
		t.Fatal("claude agent missing")
	}
	if a.Model != "claude-opus-4-7" {
		t.Errorf("claude model: got %q", a.Model)
	}
	if a.Preamble != PreambleClaudeMD {
		t.Errorf("claude preamble: got %q", a.Preamble)
	}
	if a.Hooks != HookClaudeCode {
		t.Errorf("claude hooks: got %q", a.Hooks)
	}
	shell, ok := r.Get("shell-only")
	if !ok {
		t.Fatal("shell-only agent missing")
	}
	if shell.Preamble != PreambleStdoutBanner {
		t.Errorf("shell preamble: got %q", shell.Preamble)
	}
	if shell.Hooks != HookPromptCommand {
		t.Errorf("shell hooks: got %q", shell.Hooks)
	}
}

func TestLoadRejectsDuplicateName(t *testing.T) {
	path := writeTmp(t, `
[[agent]]
name = "claude"
binary = "claude"
preamble = "claude_md"
hooks = "claude_code"

[[agent]]
name = "claude"
binary = "claude-2"
preamble = "claude_md"
hooks = "claude_code"
`)
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("want duplicate-name error, got %v", err)
	}
}

func TestLoadRejectsUnknownPreamble(t *testing.T) {
	path := writeTmp(t, `
[[agent]]
name = "x"
binary = "x"
preamble = "space_opera"
hooks = "claude_code"
`)
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "preamble") {
		t.Fatalf("want preamble error, got %v", err)
	}
}

func TestLoadRejectsEmptyRegistry(t *testing.T) {
	path := writeTmp(t, ``)
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "at least one") {
		t.Fatalf("want empty-registry error, got %v", err)
	}
}

func TestLoadRejectsMissingBinary(t *testing.T) {
	path := writeTmp(t, `
[[agent]]
name = "x"
binary = ""
preamble = "claude_md"
hooks = "claude_code"
`)
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "binary required") {
		t.Fatalf("want binary error, got %v", err)
	}
}

func TestParallelFieldsRoundTrip(t *testing.T) {
	path := writeTmp(t, `
[[agent]]
name           = "claude"
binary         = "claude"
preamble       = "claude_md"
hooks          = "claude_code"
intra_parallel = true
max_parallel   = 3
`)
	r, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	a, _ := r.Get("claude")
	if !a.IntraParallel {
		t.Fatal("intra_parallel=true expected")
	}
	if got, want := a.MaxParallel, 3; got != want {
		t.Errorf("max_parallel: want %d got %d", want, got)
	}
	if got, want := a.ResolvedMaxParallel(), 3; got != want {
		t.Errorf("ResolvedMaxParallel: want %d got %d", want, got)
	}
}

func TestResolvedMaxParallelDefaultsToOne(t *testing.T) {
	a := AgentType{Name: "x"}
	if got, want := a.ResolvedMaxParallel(), 1; got != want {
		t.Errorf("non-intra_parallel cap: want %d got %d", want, got)
	}
	a.MaxParallel = 5 // ignored — IntraParallel still false
	if got, want := a.ResolvedMaxParallel(), 1; got != want {
		t.Errorf("drift cap: want %d got %d", want, got)
	}
}

func TestLoadRejectsIntraParallelWithoutCap(t *testing.T) {
	path := writeTmp(t, `
[[agent]]
name           = "claude"
binary         = "claude"
preamble       = "claude_md"
hooks          = "claude_code"
intra_parallel = true
`)
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "max_parallel") {
		t.Fatalf("want max_parallel error, got %v", err)
	}
}

func TestRegistryWarnsOnDriftedMaxParallel(t *testing.T) {
	path := writeTmp(t, `
[[agent]]
name         = "claude"
binary       = "claude"
preamble     = "claude_md"
hooks        = "claude_code"
max_parallel = 4
`)
	r, err := Load(path)
	if err != nil {
		t.Fatalf("Load (should warn not error): %v", err)
	}
	w := r.Warnings()
	if len(w) != 1 || !strings.Contains(w[0], "max_parallel=4") {
		t.Fatalf("want one max_parallel-drift warning, got %v", w)
	}
}

func TestResolveMissingReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	if p := Resolve(dir); p != "" {
		t.Errorf("want empty path when file absent, got %q", p)
	}
}

func TestResolveFindsFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".gemba"), 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, ".gemba", "agents.toml")
	if err := os.WriteFile(p, []byte("[[agent]]\nname=\"x\"\nbinary=\"x\"\npreamble=\"claude_md\"\nhooks=\"claude_code\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := Resolve(dir); got != p {
		t.Errorf("want %q, got %q", p, got)
	}
}

func writeTmp(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "agents.toml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}
