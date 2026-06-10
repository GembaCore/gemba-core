package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// gm-e12.21.2 — clone endpoint tests.
//
// Coverage:
//   - URL validation (scheme allow list, scp-style, Windows-drive-rejection)
//   - target_dir validation (must be absolute + inside allow list)
//   - non-empty target_dir rejection
//   - happy path — fake runner records args + returns success
//   - unauthorized error mapped to 401
//   - unreachable error mapped to 502

func TestValidCloneURL(t *testing.T) {
	cases := []struct {
		url  string
		want bool
	}{
		{"https://github.com/foo/bar.git", true},
		{"http://example.com/foo.git", true},
		{"ssh://git@host/repo.git", true},
		{"git@github.com:owner/repo.git", true},
		{"file:///tmp/local-repo", false},
		{"C:/Users/op/repo", false}, // Windows drive — colon before /, no @
		{"", false},
		{"://malformed", false},
	}
	for _, tc := range cases {
		if got := validCloneURL(tc.url); got != tc.want {
			t.Errorf("validCloneURL(%q) = %v, want %v", tc.url, got, tc.want)
		}
	}
}

func cloneTestRouter(runner CommandRunner) (*Router, func()) {
	r := &Router{cloneRunner: runner}
	cache := NewNonceCache(0, 0)
	r.nonceCache = cache
	return r, func() {}
}

func postClone(t *testing.T, r *Router, body cloneProjectRequest) *httptest.ResponseRecorder {
	t.Helper()
	bs, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/clone", bytes.NewReader(bs))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.cloneProject(w, req)
	return w
}

func TestCloneProject_RejectsBadURL(t *testing.T) {
	r, cleanup := cloneTestRouter(nil)
	defer cleanup()
	w := postClone(t, r, cloneProjectRequest{
		URL:       "file:///tmp/x",
		TargetDir: "/tmp/test-clone",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400; body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "clone_unsupported_url") {
		t.Errorf("body: got %s", w.Body.String())
	}
}

func TestCloneProject_RejectsRelativeTarget(t *testing.T) {
	r, cleanup := cloneTestRouter(nil)
	defer cleanup()
	w := postClone(t, r, cloneProjectRequest{
		URL:       "https://github.com/foo/bar.git",
		TargetDir: "relative/dir",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d", w.Code)
	}
}

func TestCloneProject_RejectsOutsideAllowList(t *testing.T) {
	tmp := t.TempDir()
	allowOnlyTempRoot(t, tmp)
	r, cleanup := cloneTestRouter(nil)
	defer cleanup()
	w := postClone(t, r, cloneProjectRequest{
		URL:       "https://github.com/foo/bar.git",
		TargetDir: "/usr/local/share/x",
	})
	if w.Code != http.StatusForbidden {
		t.Fatalf("status: got %d body=%s", w.Code, w.Body.String())
	}
}

func TestCloneProject_RejectsNonEmptyTarget(t *testing.T) {
	tmp := t.TempDir()
	root := allowOnlyTempRoot(t, tmp)
	target := filepath.Join(root, "existing")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(target, "f"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	r, cleanup := cloneTestRouter(nil)
	defer cleanup()
	w := postClone(t, r, cloneProjectRequest{
		URL:       "https://github.com/foo/bar.git",
		TargetDir: target,
	})
	if w.Code != http.StatusConflict {
		t.Fatalf("status: got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "clone_already_exists") {
		t.Errorf("body: got %s", w.Body.String())
	}
}

func TestCloneProject_HappyPath(t *testing.T) {
	tmp := t.TempDir()
	root := allowOnlyTempRoot(t, tmp)
	target := filepath.Join(root, "fresh-clone")

	gotArgs := [][]string{}
	runner := func(_ context.Context, dir, name string, args ...string) ([]byte, error) {
		gotArgs = append(gotArgs, append([]string{name}, args...))
		// Simulate the post-clone metadata pulls.
		switch {
		case len(args) >= 2 && args[0] == "rev-parse" && args[1] == "HEAD":
			return []byte("deadbeef\n"), nil
		case len(args) >= 3 && args[0] == "rev-parse" && args[2] == "HEAD":
			return []byte("main\n"), nil
		}
		// Pretend git clone created the dir; the response handler
		// only re-reads metadata, so we only need the dir to exist.
		_ = os.MkdirAll(dir, 0o755)
		_ = os.MkdirAll(target, 0o755)
		return nil, nil
	}

	r, cleanup := cloneTestRouter(runner)
	defer cleanup()
	w := postClone(t, r, cloneProjectRequest{
		URL:       "https://github.com/foo/bar.git",
		TargetDir: target,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d body=%s", w.Code, w.Body.String())
	}
	var resp cloneProjectResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.RepoPath != target {
		t.Errorf("repo_path: got %q want %q", resp.RepoPath, target)
	}
	if resp.HeadSHA != "deadbeef" {
		t.Errorf("head_sha: got %q", resp.HeadSHA)
	}
	if resp.DefaultBranch != "main" {
		t.Errorf("default_branch: got %q", resp.DefaultBranch)
	}
	// First call should have been the actual clone.
	if len(gotArgs) == 0 || gotArgs[0][0] != "git" || gotArgs[0][1] != "clone" {
		t.Errorf("first call: got %v want [git clone ...]", gotArgs)
	}
}

func TestCloneProject_UnauthorizedMapping(t *testing.T) {
	tmp := t.TempDir()
	root := allowOnlyTempRoot(t, tmp)
	target := filepath.Join(root, "auth-fail")
	runner := func(context.Context, string, string, ...string) ([]byte, error) {
		return nil, errFromString("Authentication failed for 'https://github.com/foo/bar.git'")
	}
	r, cleanup := cloneTestRouter(runner)
	defer cleanup()
	w := postClone(t, r, cloneProjectRequest{
		URL:       "https://github.com/foo/bar.git",
		TargetDir: target,
	})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "clone_unauthorized") {
		t.Errorf("body: got %s", w.Body.String())
	}
}

func TestCloneProject_UnreachableMapping(t *testing.T) {
	tmp := t.TempDir()
	root := allowOnlyTempRoot(t, tmp)
	target := filepath.Join(root, "unreachable")
	runner := func(context.Context, string, string, ...string) ([]byte, error) {
		return nil, errFromString("Could not resolve host: github.com")
	}
	r, cleanup := cloneTestRouter(runner)
	defer cleanup()
	w := postClone(t, r, cloneProjectRequest{
		URL:       "https://github.com/foo/bar.git",
		TargetDir: target,
	})
	if w.Code != http.StatusBadGateway {
		t.Fatalf("status: got %d body=%s", w.Code, w.Body.String())
	}
}

// errFromString is a tiny helper so the runner stubs can mint an
// error with a known message without pulling fmt.Errorf for every
// case.
type cloneTestErr struct{ msg string }

func (e *cloneTestErr) Error() string { return e.msg }

func errFromString(s string) error { return &cloneTestErr{msg: s} }
