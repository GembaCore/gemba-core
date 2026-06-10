// Tests for POST /api/v1/projects/bind (gm-root.17.14).
//
// Covers:
//   - mode "create" happy path: directory becomes a git repo + has
//     .gemba/workspace.toml.
//   - mode "navigate" happy path: beads DB copied into target repo's
//     .beads/ subdir; .gemba/workspace.toml written at target.
//   - validation: bad mode → 400.
//   - validation: missing target dir for navigate → 400.
//   - validation: target is not a git repo for navigate → 400.
//   - validation: beads_db_path doesn't exist → 400.
//   - validation: beads_db_path missing .beads/ → 400.
//   - validation: mode=create but target != beads_db_path → 400.
//   - conflict: navigate where target already has .beads/ → 409.
//   - nonce: missing X-GEMBA-Confirm → 400.

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/GembaCore/gemba-core/internal/config"
)

// bindRouter builds a Router with stub GitInitRunner so the test
// doesn't need a real git binary. Pinger is unused on the bind path
// but must be supplied because AttachConfig.GitInitRunner is shared
// with the attach handler.
func bindRouter(t *testing.T) (*Router, *bindStubs) {
	t.Helper()
	home := t.TempDir()
	projectsDir := filepath.Join(home, "projects")
	cfgPath := makeConfigTOML(t, home, projectsDir)
	r := NewRouter(config.ServeConfig{ConfigPath: cfgPath}, fakeSPA(), nil)
	stubs := &bindStubs{}
	r.AttachProjects(AttachConfig{
		GitInitRunner: stubs.Run,
		Now:           func() time.Time { return time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC) },
	})
	return r, stubs
}

type bindStubs struct {
	runErr   error
	runCalls [][]string
	// onRun lets a test simulate `git init`'s side effect (creating
	// a .git/ directory) so the post-init classification picks up the
	// repo. Default is nil — the stub just records the call.
	onRun func(dir string, args []string) error
}

func (s *bindStubs) Run(_ context.Context, dir, name string, args ...string) ([]byte, error) {
	cmd := append([]string{name}, args...)
	s.runCalls = append(s.runCalls, cmd)
	if s.onRun != nil {
		if err := s.onRun(dir, cmd); err != nil {
			return nil, err
		}
	}
	if s.runErr != nil {
		return nil, s.runErr
	}
	return nil, nil
}

func bindReq(t *testing.T, body any) *http.Request {
	t.Helper()
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/bind", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(ConfirmHeader, "test-bind-nonce-"+t.Name()+"-"+time.Now().Format("150405.000000000"))
	return req
}

// seedBeadsDir creates a directory with a .beads/ subdir containing a
// dummy file the test can verify gets copied to the target.
func seedBeadsDir(t *testing.T, parent, name string) string {
	t.Helper()
	dir := filepath.Join(parent, name)
	beads := filepath.Join(dir, ".beads")
	if err := os.MkdirAll(beads, 0o755); err != nil {
		t.Fatalf("mkdir .beads: %v", err)
	}
	if err := os.WriteFile(filepath.Join(beads, "dolt.db"), []byte("fake-dolt-data"), 0o644); err != nil {
		t.Fatalf("seed dolt.db: %v", err)
	}
	return dir
}

func TestBindProject_Create_HappyPath(t *testing.T) {
	activeProjectMu.Lock()
	activeProjectName = ""
	activeProjectMu.Unlock()

	r, stubs := bindRouter(t)
	parent := t.TempDir()
	src := seedBeadsDir(t, parent, "lonely")

	// Simulate `git init`'s side effect: create a .git/ dir.
	stubs.onRun = func(dir string, args []string) error {
		if len(args) >= 2 && args[0] == "git" && args[1] == "init" {
			return os.MkdirAll(filepath.Join(dir, ".git"), 0o755)
		}
		return nil
	}

	body := BindProjectRequest{
		BeadsDBPath:    src,
		TargetRepoPath: src,
		Mode:           "create",
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, bindReq(t, body))

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d; body=%s", rec.Code, rec.Body.String())
	}

	// .git was created (via the runner stub).
	if _, err := os.Stat(filepath.Join(src, ".git")); err != nil {
		t.Errorf(".git not present at target: %v", err)
	}
	// .gemba/workspace.toml was written.
	wsPath := filepath.Join(src, ".gemba", "workspace.toml")
	data, err := os.ReadFile(wsPath)
	if err != nil {
		t.Fatalf("workspace.toml not written: %v", err)
	}
	if !bytes.Contains(data, []byte(`name = "lonely"`)) {
		t.Errorf("workspace.toml missing name; got %s", data)
	}

	// Response shape.
	var resp projectEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode resp: %v", err)
	}
	if resp.Name != "lonely" {
		t.Errorf("resp.Name=%q", resp.Name)
	}
	if resp.Path != src {
		t.Errorf("resp.Path=%q want %q", resp.Path, src)
	}
	if resp.Kind != config.KindComplete {
		t.Errorf("resp.Kind=%q, want complete", resp.Kind)
	}
	if !resp.Active {
		t.Errorf("resp.Active=false; want true")
	}

	// Runner was called with `git init`.
	if len(stubs.runCalls) == 0 {
		t.Errorf("expected git init runner invocation")
	}
}

func TestBindProject_Navigate_HappyPath(t *testing.T) {
	activeProjectMu.Lock()
	activeProjectName = ""
	activeProjectMu.Unlock()

	r, _ := bindRouter(t)
	parent := t.TempDir()
	src := seedBeadsDir(t, parent, "orphan")

	// Pre-existing repo as the target.
	repo := filepath.Join(parent, "host-repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatalf("seed host-repo/.git: %v", err)
	}

	body := BindProjectRequest{
		BeadsDBPath:    src,
		TargetRepoPath: repo,
		Mode:           "navigate",
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, bindReq(t, body))

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d; body=%s", rec.Code, rec.Body.String())
	}

	// .beads was COPIED into the target.
	dstDB := filepath.Join(repo, ".beads", "dolt.db")
	got, err := os.ReadFile(dstDB)
	if err != nil {
		t.Fatalf("copy missing at target: %v", err)
	}
	if string(got) != "fake-dolt-data" {
		t.Errorf("copied content mismatch: %q", got)
	}
	// Source still exists (copy, not move).
	if _, err := os.Stat(filepath.Join(src, ".beads", "dolt.db")); err != nil {
		t.Errorf("source .beads should remain after navigate; err=%v", err)
	}
	// workspace.toml at target.
	wsPath := filepath.Join(repo, ".gemba", "workspace.toml")
	data, err := os.ReadFile(wsPath)
	if err != nil {
		t.Fatalf("workspace.toml not written: %v", err)
	}
	if !bytes.Contains(data, []byte(`name = "host-repo"`)) {
		t.Errorf("workspace.toml missing name; got %s", data)
	}

	// Response.
	var resp projectEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode resp: %v", err)
	}
	if resp.Path != repo {
		t.Errorf("resp.Path=%q want %q", resp.Path, repo)
	}
	if resp.Kind != config.KindComplete {
		t.Errorf("resp.Kind=%q, want complete", resp.Kind)
	}
}

func TestBindProject_BadMode(t *testing.T) {
	r, _ := bindRouter(t)
	parent := t.TempDir()
	src := seedBeadsDir(t, parent, "p")
	body := BindProjectRequest{
		BeadsDBPath:    src,
		TargetRepoPath: src,
		Mode:           "wat",
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, bindReq(t, body))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d; body=%s", rec.Code, rec.Body.String())
	}
}

func TestBindProject_Navigate_TargetMissing(t *testing.T) {
	r, _ := bindRouter(t)
	parent := t.TempDir()
	src := seedBeadsDir(t, parent, "p")
	body := BindProjectRequest{
		BeadsDBPath:    src,
		TargetRepoPath: filepath.Join(parent, "no-such-dir"),
		Mode:           "navigate",
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, bindReq(t, body))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d; body=%s", rec.Code, rec.Body.String())
	}
}

func TestBindProject_Navigate_TargetNotGitRepo(t *testing.T) {
	r, _ := bindRouter(t)
	parent := t.TempDir()
	src := seedBeadsDir(t, parent, "p")
	notARepo := filepath.Join(parent, "plain-dir")
	if err := os.MkdirAll(notARepo, 0o755); err != nil {
		t.Fatal(err)
	}
	body := BindProjectRequest{
		BeadsDBPath:    src,
		TargetRepoPath: notARepo,
		Mode:           "navigate",
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, bindReq(t, body))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d; body=%s", rec.Code, rec.Body.String())
	}
}

func TestBindProject_BeadsDBPathMissing(t *testing.T) {
	r, _ := bindRouter(t)
	body := BindProjectRequest{
		BeadsDBPath:    "/definitely/not/a/real/dir/gm-root-17-14",
		TargetRepoPath: "/definitely/not/a/real/dir/gm-root-17-14",
		Mode:           "create",
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, bindReq(t, body))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d; body=%s", rec.Code, rec.Body.String())
	}
}

func TestBindProject_BeadsDBPathLacksBeadsDir(t *testing.T) {
	r, _ := bindRouter(t)
	parent := t.TempDir()
	plain := filepath.Join(parent, "plain")
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	body := BindProjectRequest{
		BeadsDBPath:    plain,
		TargetRepoPath: plain,
		Mode:           "create",
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, bindReq(t, body))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d; body=%s", rec.Code, rec.Body.String())
	}
}

func TestBindProject_Create_TargetMustEqualBeadsDB(t *testing.T) {
	r, _ := bindRouter(t)
	parent := t.TempDir()
	src := seedBeadsDir(t, parent, "p")
	other := filepath.Join(parent, "elsewhere")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	body := BindProjectRequest{
		BeadsDBPath:    src,
		TargetRepoPath: other,
		Mode:           "create",
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, bindReq(t, body))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d; body=%s", rec.Code, rec.Body.String())
	}
}

func TestBindProject_Navigate_TargetAlreadyHasBeadsDir(t *testing.T) {
	r, _ := bindRouter(t)
	parent := t.TempDir()
	src := seedBeadsDir(t, parent, "p")

	// Existing git repo that ALREADY has a .beads/. Refuse to clobber.
	repo := filepath.Join(parent, "host")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, ".beads"), 0o755); err != nil {
		t.Fatal(err)
	}

	body := BindProjectRequest{
		BeadsDBPath:    src,
		TargetRepoPath: repo,
		Mode:           "navigate",
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, bindReq(t, body))
	if rec.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d; body=%s", rec.Code, rec.Body.String())
	}
}

func TestBindProject_MissingNonce(t *testing.T) {
	r, _ := bindRouter(t)
	parent := t.TempDir()
	src := seedBeadsDir(t, parent, "p")
	body := BindProjectRequest{
		BeadsDBPath:    src,
		TargetRepoPath: src,
		Mode:           "create",
	}
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/bind", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	// no X-GEMBA-Confirm
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for missing nonce, got %d; body=%s", rec.Code, rec.Body.String())
	}
}
