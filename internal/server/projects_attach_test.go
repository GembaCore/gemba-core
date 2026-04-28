// Tests for POST /api/v1/projects/attach (gm-xwa8).
//
// Covers:
//   - happy path: create_at — directory + git init + workspace.toml,
//     pinger called, response is a projectEntry.
//   - happy path: repo_path — existing git worktree gets workspace.toml,
//     no git init runs.
//   - validation: both repo_path and create_at → 400.
//   - validation: neither → 400.
//   - validation: repo_path exists but isn't a git worktree → 400.
//   - validation: project_name missing → 400.
//   - validation: bad mysql:// URL → 502 (pinger surfaces failure).
//   - rollback: workspace.toml write fails for create_at — directory we
//     created is removed.
//   - nonce: missing X-GEMBA-Confirm header → 400.

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MikeBengtson/gemba/internal/config"
)

// attachRouter builds a Router with stub Pinger + GitInitRunner so the
// test doesn't need a real Dolt server or git binary.
func attachRouter(t *testing.T) (*Router, *attachStubs) {
	t.Helper()
	home := t.TempDir()
	projectsDir := filepath.Join(home, "projects")
	cfgPath := makeConfigTOML(t, home, projectsDir)
	r := NewRouter(config.ServeConfig{ConfigPath: cfgPath}, fakeSPA(), nil)
	stubs := &attachStubs{}
	r.AttachProjects(AttachConfig{
		Pinger:        stubs.Ping,
		GitInitRunner: stubs.Run,
		Now:           func() time.Time { return time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC) },
	})
	return r, stubs
}

type attachStubs struct {
	pingErr   error
	pingCalls int
	pingURL   string

	runErr   error
	runCalls [][]string
}

func (s *attachStubs) Ping(_ context.Context, url string) error {
	s.pingCalls++
	s.pingURL = url
	return s.pingErr
}

func (s *attachStubs) Run(_ context.Context, _, name string, args ...string) ([]byte, error) {
	cmd := append([]string{name}, args...)
	s.runCalls = append(s.runCalls, cmd)
	if s.runErr != nil {
		return nil, s.runErr
	}
	return nil, nil
}

func attachReq(body any) *http.Request {
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/attach", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(ConfirmHeader, "test-nonce-"+time.Now().Format("150405.000000000"))
	return req
}

func TestAttachProject_CreateAt_HappyPath(t *testing.T) {
	activeProjectMu.Lock()
	activeProjectName = ""
	activeProjectMu.Unlock()

	r, stubs := attachRouter(t)
	parent := t.TempDir()
	target := filepath.Join(parent, "adopted-proj")

	body := attachProjectRequest{
		BeadsDB:     "mysql://root@127.0.0.1:3307/adopted_proj",
		ProjectName: "adopted-proj",
		CreateAt:    target,
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, attachReq(body))

	if rec.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d; body=%s", rec.Code, rec.Body.String())
	}
	// Pinger called with the requested URL.
	if stubs.pingCalls != 1 {
		t.Errorf("want 1 ping, got %d", stubs.pingCalls)
	}
	if stubs.pingURL != body.BeadsDB {
		t.Errorf("ping URL mismatch: got %q", stubs.pingURL)
	}
	// git init was attempted.
	if len(stubs.runCalls) == 0 {
		t.Errorf("expected git init runner invocation")
	}
	// .gemba/workspace.toml was written.
	wsPath := filepath.Join(target, ".gemba", "workspace.toml")
	data, err := os.ReadFile(wsPath)
	if err != nil {
		t.Fatalf("workspace.toml not written: %v", err)
	}
	if !bytes.Contains(data, []byte(`name = "adopted-proj"`)) {
		t.Errorf("workspace.toml missing name; got %s", data)
	}
	if !bytes.Contains(data, []byte(`beads_db = "mysql://root@127.0.0.1:3307/adopted_proj"`)) {
		t.Errorf("workspace.toml missing beads_db; got %s", data)
	}
	// Response shape.
	var resp attachProjectResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode resp: %v", err)
	}
	if resp.Name != "adopted-proj" {
		t.Errorf("resp.Name=%q", resp.Name)
	}
	if resp.Path != target {
		t.Errorf("resp.Path=%q want %q", resp.Path, target)
	}
	if !resp.Active {
		t.Errorf("resp.Active=false; want true")
	}
}

func TestAttachProject_RepoPath_HappyPath(t *testing.T) {
	activeProjectMu.Lock()
	activeProjectName = ""
	activeProjectMu.Unlock()

	r, stubs := attachRouter(t)
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatalf("seed .git: %v", err)
	}

	body := attachProjectRequest{
		BeadsDB:     "mysql://root@127.0.0.1:3307/legacy",
		ProjectName: "legacy",
		RepoPath:    repo,
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, attachReq(body))

	if rec.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d; body=%s", rec.Code, rec.Body.String())
	}
	if len(stubs.runCalls) != 0 {
		t.Errorf("want no git init for existing repo, got %d calls", len(stubs.runCalls))
	}
	wsPath := filepath.Join(repo, ".gemba", "workspace.toml")
	if _, err := os.Stat(wsPath); err != nil {
		t.Fatalf("workspace.toml not written: %v", err)
	}
}

func TestAttachProject_BothPathsSupplied(t *testing.T) {
	r, _ := attachRouter(t)
	body := attachProjectRequest{
		BeadsDB:     "mysql://root@127.0.0.1:3307/x",
		ProjectName: "x",
		RepoPath:    "/tmp/foo",
		CreateAt:    "/tmp/bar",
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, attachReq(body))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d; body=%s", rec.Code, rec.Body.String())
	}
}

func TestAttachProject_NeitherPath(t *testing.T) {
	r, _ := attachRouter(t)
	body := attachProjectRequest{
		BeadsDB:     "mysql://root@127.0.0.1:3307/x",
		ProjectName: "x",
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, attachReq(body))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d; body=%s", rec.Code, rec.Body.String())
	}
}

func TestAttachProject_RepoPathNotGit(t *testing.T) {
	r, _ := attachRouter(t)
	repo := t.TempDir() // exists but no .git
	body := attachProjectRequest{
		BeadsDB:     "mysql://root@127.0.0.1:3307/x",
		ProjectName: "x",
		RepoPath:    repo,
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, attachReq(body))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d; body=%s", rec.Code, rec.Body.String())
	}
}

func TestAttachProject_MissingProjectName(t *testing.T) {
	r, _ := attachRouter(t)
	body := attachProjectRequest{
		BeadsDB:  "mysql://root@127.0.0.1:3307/x",
		CreateAt: filepath.Join(t.TempDir(), "x"),
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, attachReq(body))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d; body=%s", rec.Code, rec.Body.String())
	}
}

func TestAttachProject_PingerFailureRollsBackCreatedDir(t *testing.T) {
	r, stubs := attachRouter(t)
	stubs.pingErr = errors.New("connection refused")

	parent := t.TempDir()
	target := filepath.Join(parent, "doomed")

	body := attachProjectRequest{
		BeadsDB:     "mysql://root@127.0.0.1:3307/doomed",
		ProjectName: "doomed",
		CreateAt:    target,
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, attachReq(body))

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("want 502, got %d; body=%s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Errorf("created dir was not rolled back: stat err=%v", err)
	}
}

func TestAttachProject_MissingNonce(t *testing.T) {
	r, _ := attachRouter(t)
	body := attachProjectRequest{
		BeadsDB:     "mysql://root@127.0.0.1:3307/x",
		ProjectName: "x",
		CreateAt:    filepath.Join(t.TempDir(), "x"),
	}
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/attach", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	// no X-GEMBA-Confirm
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for missing nonce, got %d; body=%s", rec.Code, rec.Body.String())
	}
}
