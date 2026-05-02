package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/GembaCore/gemba-core/internal/config"
)

func TestOnboardingSetup_ExistingProject_PreparesGuidanceAndAnalysis(t *testing.T) {
	dir := t.TempDir()
	mustMkdir(t, filepath.Join(dir, ".git"))

	var calls []string
	runner := func(_ context.Context, dir, name string, args ...string) ([]byte, error) {
		call := name + " " + strings.Join(args, " ")
		calls = append(calls, call)
		if call == "git status --porcelain" {
			return nil, nil
		}
		return []byte("ok"), nil
	}
	r := NewRouter(config.ServeConfig{}, fakeSPA(), nil)
	t.Cleanup(r.Close)
	r.AttachProjects(AttachConfig{
		GitInitRunner: runner,
		Now:           func() time.Time { return time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC) },
	})

	body := map[string]any{
		"origin":               "existing",
		"project_name":         "demo",
		"github_project":       "GembaCore/demo",
		"orchestration":        "native",
		"worktree_path":        dir,
		"source_analysis_tool": "gitnexus",
	}
	rec := httptest.NewRecorder()
	req := newProjectReq(t, http.MethodPost, "/api/v1/onboarding/setup", body)
	req.Header.Set(ConfirmHeader, "setup-existing")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	var resp onboardingSetupResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ProjectPath != dir {
		t.Fatalf("ProjectPath=%q want %q", resp.ProjectPath, dir)
	}
	if resp.Checks["source_analysis"] != "current" {
		t.Fatalf("source_analysis check=%q", resp.Checks["source_analysis"])
	}
	for _, path := range []string{
		filepath.Join(dir, "AGENTS.md"),
		filepath.Join(dir, "CLAUDE.md"),
		filepath.Join(dir, ".Codex", "settings.local.json"),
		filepath.Join(dir, ".gemba", "workspace.toml"),
		filepath.Join(dir, ".gemba", "codeanalysis.toml"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %s: %v", path, err)
		}
	}
	agents := mustRead(t, filepath.Join(dir, "AGENTS.md"))
	if !strings.Contains(agents, "Beads is the source of truth") {
		t.Fatalf("AGENTS.md missing runtime guidance:\n%s", agents)
	}
	joined := strings.Join(calls, "\n")
	for _, want := range []string{
		"git fetch --prune",
		"git pull --ff-only",
		"bd init --non-interactive",
		"gitnexus analyze --path " + dir,
		"gitnexus mcp --help",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing runner call %q in:\n%s", want, joined)
		}
	}
}

func TestOnboardingSetup_ExistingProject_DirtySkipsPull(t *testing.T) {
	dir := t.TempDir()
	mustMkdir(t, filepath.Join(dir, ".git"))

	var calls []string
	runner := func(_ context.Context, dir, name string, args ...string) ([]byte, error) {
		call := name + " " + strings.Join(args, " ")
		calls = append(calls, call)
		if call == "git status --porcelain" {
			return []byte(" M main.go\n"), nil
		}
		if call == "git fetch --prune" || call == "git pull --ff-only" {
			return nil, errors.New("should not sync dirty tree")
		}
		return []byte("ok"), nil
	}
	r := NewRouter(config.ServeConfig{}, fakeSPA(), nil)
	t.Cleanup(r.Close)
	r.AttachProjects(AttachConfig{GitInitRunner: runner})

	body := map[string]any{
		"origin":               "existing",
		"project_name":         "demo",
		"github_project":       "GembaCore/demo",
		"orchestration":        "native",
		"worktree_path":        dir,
		"source_analysis_tool": "none",
	}
	rec := httptest.NewRecorder()
	req := newProjectReq(t, http.MethodPost, "/api/v1/onboarding/setup", body)
	req.Header.Set(ConfirmHeader, "setup-dirty")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	var resp onboardingSetupResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Checks["git_sync"] != "skipped-dirty" {
		t.Fatalf("git_sync check=%q", resp.Checks["git_sync"])
	}
	joined := strings.Join(calls, "\n")
	if strings.Contains(joined, "git fetch --prune") || strings.Contains(joined, "git pull --ff-only") {
		t.Fatalf("dirty worktree should not fetch/pull:\n%s", joined)
	}
	if len(resp.Warnings) == 0 || !strings.Contains(resp.Warnings[0], "dirty") {
		t.Fatalf("expected dirty warning, got %#v", resp.Warnings)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}
