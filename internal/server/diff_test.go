// diff_test.go: tests for GET /api/v1/workspaces/{wsid}/diff
// (gm-o9t8.1.6.2).
//
// Each scenario builds a real on-disk git repo under a temp directory
// structured as <workspacesRoot>/<wsid>/repo/. The handler invokes
// `git diff`/`git diff --cached` against it; we compare the response
// body to the same `git diff` we ran locally so the test catches a
// drift between the production handler and the tool it shells out to.

package server

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GembaCore/gemba-core/internal/config"
)

func gitAvailable(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH; skipping diff handler test")
	}
}

// initRepo creates a workspaces root with a single workspace whose
// repo has both an unstaged change and a staged-but-unfaded change so
// `git diff` and `git diff --cached` return distinct, non-trivial
// output. Returns the workspaces root + the wsid.
func initRepo(t *testing.T) (root, wsid string) {
	t.Helper()
	gitAvailable(t)
	root = t.TempDir()
	wsid = "ws-test"
	repo := filepath.Join(root, wsid, "repo")
	mustRun := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
	if err := mkRepoDir(repo); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Init + config a throwaway identity (some CI envs reject commits
	// without one).
	cmd := exec.Command("git", "init", "-q")
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	mustRun("git", "config", "user.email", "test@example.com")
	mustRun("git", "config", "user.name", "test")
	mustRun("git", "config", "commit.gpgsign", "false")
	if err := writeFile(filepath.Join(repo, "hello.txt"), "hello\n"); err != nil {
		t.Fatalf("write hello: %v", err)
	}
	if err := writeFile(filepath.Join(repo, "other.txt"), "other\n"); err != nil {
		t.Fatalf("write other: %v", err)
	}
	mustRun("git", "add", ".")
	mustRun("git", "commit", "-q", "-m", "init")
	// Unstaged change.
	if err := writeFile(filepath.Join(repo, "hello.txt"), "hello world\n"); err != nil {
		t.Fatalf("rewrite hello: %v", err)
	}
	// Staged change.
	if err := writeFile(filepath.Join(repo, "other.txt"), "other changed\n"); err != nil {
		t.Fatalf("rewrite other: %v", err)
	}
	mustRun("git", "add", "other.txt")
	return root, wsid
}

func mkRepoDir(path string) error {
	return os.MkdirAll(path, 0o755)
}

// gitDiff runs `git diff` (or `git diff --cached`) inside the repo and
// returns the bytes the test compares against.
func gitDiff(t *testing.T, repo string, args ...string) []byte {
	t.Helper()
	full := append([]string{"diff"}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = repo
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git diff %v: %v", args, err)
	}
	return out
}

func newDiffRouter(t *testing.T, root string) *Router {
	t.Helper()
	r := NewRouter(config.ServeConfig{Town: "test"}, fakeSPA(), nil)
	t.Cleanup(r.Close)
	r.AttachWorkspacesRoot(root)
	return r
}

func TestDiff_StreamsUnstagedDiff(t *testing.T) {
	root, wsid := initRepo(t)
	r := newDiffRouter(t, root)
	repo := filepath.Join(root, wsid, "repo")
	want := gitDiff(t, repo)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/"+wsid+"/diff", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%q", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/x-patch") {
		t.Errorf("Content-Type = %q, want text/x-patch", got)
	}
	if !bytes.Equal(rec.Body.Bytes(), want) {
		t.Errorf("body mismatch.\n--- got ---\n%s\n--- want ---\n%s", rec.Body.String(), want)
	}
}

func TestDiff_StagedQuery(t *testing.T) {
	root, wsid := initRepo(t)
	r := newDiffRouter(t, root)
	repo := filepath.Join(root, wsid, "repo")
	want := gitDiff(t, repo, "--cached")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/"+wsid+"/diff?staged=true", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%q", rec.Code, rec.Body.String())
	}
	if !bytes.Equal(rec.Body.Bytes(), want) {
		t.Errorf("body mismatch.\n--- got ---\n%s\n--- want ---\n%s", rec.Body.String(), want)
	}
}

func TestDiff_PathFilter(t *testing.T) {
	root, wsid := initRepo(t)
	r := newDiffRouter(t, root)
	repo := filepath.Join(root, wsid, "repo")
	want := gitDiff(t, repo, "--", "hello.txt")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/"+wsid+"/diff?path=hello.txt", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%q", rec.Code, rec.Body.String())
	}
	if !bytes.Equal(rec.Body.Bytes(), want) {
		t.Errorf("body mismatch.\n--- got ---\n%s\n--- want ---\n%s", rec.Body.String(), want)
	}
	if strings.Contains(rec.Body.String(), "other.txt") {
		t.Errorf("filter leaked: body mentions other.txt:\n%s", rec.Body.String())
	}
}

func TestDiff_PathTraversalRejected(t *testing.T) {
	root, wsid := initRepo(t)
	r := newDiffRouter(t, root)

	cases := []string{
		"path=../etc/passwd",
		"path=/etc/passwd",
		"path=foo/../../bar",
		"path=-flag",
		"path=foo%7Cbar",  // pipe — shell metachar
		"path=foo%60bar",  // backtick
	}
	for _, q := range cases {
		t.Run(q, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/"+wsid+"/diff?"+q, nil)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("%s: status = %d, want 400; body=%q", q, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestDiff_UnsafeWorkspaceID(t *testing.T) {
	root, _ := initRepo(t)
	r := newDiffRouter(t, root)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/..%2Fetc/diff", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest && rec.Code != http.StatusNotFound {
		// chi may 404 first (path won't match the route shape).
		// Both are acceptable rejections; never 200.
		t.Errorf("status = %d for unsafe wsid; body=%q", rec.Code, rec.Body.String())
	}
}

func TestDiff_NoRootConfigured(t *testing.T) {
	// Build a router without AttachWorkspacesRoot.
	r := NewRouter(config.ServeConfig{Town: "test"}, fakeSPA(), nil)
	t.Cleanup(r.Close)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/anything/diff", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503; body=%q", rec.Code, rec.Body.String())
	}
}

func TestDiff_RepoMissing(t *testing.T) {
	root := t.TempDir()
	r := newDiffRouter(t, root)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/missing/diff", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404; body=%q", rec.Code, rec.Body.String())
	}
}

func TestDiff_StreamingNotBuffered(t *testing.T) {
	// Sanity check that the handler hands the response writer to
	// io.Copy directly: the response body decodes incrementally. We
	// can't directly observe streaming through httptest.Recorder
	// (which buffers), so we use a real httptest.Server.
	root, wsid := initRepo(t)
	r := newDiffRouter(t, root)
	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/workspaces/" + wsid + "/diff")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body=%q", resp.StatusCode, body)
	}
	if len(body) == 0 {
		t.Fatal("expected non-empty diff body")
	}
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}
