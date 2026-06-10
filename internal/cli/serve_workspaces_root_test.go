// serve_workspaces_root_test.go: tests for the --workspaces-root flag
// resolution and end-to-end wiring of /api/v1/workspaces/{wsid}/diff
// (gm-o9t8.1.16).
//
// Two scopes:
//
//   - TestResolveWorkspacesRoot pins the resolution precedence (explicit
//     flag > derived from --dolt-data-dir > empty).
//   - TestServe_AttachWorkspacesRoot_DiffEndpointWired constructs a real
//     Router via the production constructor, points a workspaces-root
//     at an on-disk repo, and asserts /diff returns a non-503 status
//     for a known wsid.

package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/GembaCore/gemba-core/internal/config"
	"github.com/GembaCore/gemba-core/internal/server"
)

func TestResolveWorkspacesRoot(t *testing.T) {
	cases := []struct {
		name string
		cfg  config.ServeConfig
		want string
	}{
		{
			name: "explicit flag honored",
			cfg:  config.ServeConfig{WorkspacesRoot: "/tmp/explicit"},
			want: "/tmp/explicit",
		},
		{
			name: "derived from dolt-data-dir parent",
			cfg:  config.ServeConfig{DoltDataDir: "/srv/gemba/data/dolt"},
			want: "/srv/gemba/data/workspaces",
		},
		{
			name: "explicit beats derivation",
			cfg: config.ServeConfig{
				WorkspacesRoot: "/explicit",
				DoltDataDir:    "/srv/gemba/data/dolt",
			},
			want: "/explicit",
		},
		{
			name: "neither set → empty",
			cfg:  config.ServeConfig{},
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveWorkspacesRoot(&tc.cfg)
			if got != tc.want {
				t.Errorf("resolveWorkspacesRoot = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestServe_AttachWorkspacesRoot_DiffEndpointWired verifies that when
// the serve-time resolver returns a non-empty path, AttachWorkspacesRoot
// flips /api/v1/workspaces/{wsid}/diff away from 503 for a known wsid.
// This is the production wiring contract for gm-o9t8.1.16.
func TestServe_AttachWorkspacesRoot_DiffEndpointWired(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH; skipping diff endpoint wiring test")
	}

	root := t.TempDir()
	wsid := "foo"
	repo := filepath.Join(root, wsid, "repo")
	if err := os.MkdirAll(repo, 0o750); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	mustRun := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
	mustRun("git", "init", "-q")
	mustRun("git", "config", "user.email", "test@example.com")
	mustRun("git", "config", "user.name", "test")
	mustRun("git", "config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(repo, "f.txt"), []byte("hello\n"), 0o640); err != nil {
		t.Fatalf("write: %v", err)
	}
	mustRun("git", "add", ".")
	mustRun("git", "commit", "-q", "-m", "init")

	// Sanity: with no AttachWorkspacesRoot call, the diff endpoint
	// must be 503 (otherwise the assertion below is meaningless).
	bare := server.NewRouter(config.ServeConfig{Town: "test"}, fstest.MapFS{}, nil)
	t.Cleanup(bare.Close)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/"+wsid+"/diff", nil)
	rec := httptest.NewRecorder()
	bare.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("baseline (no Attach) status = %d, want 503; body=%q",
			rec.Code, rec.Body.String())
	}

	// The full production wiring: resolve, attach, verify /diff is
	// no longer 503 for a real wsid.
	cfg := &config.ServeConfig{WorkspacesRoot: root}
	resolved := resolveWorkspacesRoot(cfg)
	if resolved != root {
		t.Fatalf("resolveWorkspacesRoot=%q, want %q", resolved, root)
	}
	r := server.NewRouter(config.ServeConfig{Town: "test"}, fstest.MapFS{}, nil)
	t.Cleanup(r.Close)
	r.AttachWorkspacesRoot(resolved)

	req = httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/"+wsid+"/diff", nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code == http.StatusServiceUnavailable {
		t.Fatalf("/diff still 503 after AttachWorkspacesRoot; body=%q",
			rec.Body.String())
	}
	// The repo has no unstaged changes, so a clean 200 with empty
	// body is the expected shape from git diff.
	if rec.Code != http.StatusOK {
		t.Fatalf("/diff status = %d, want 200; body=%q",
			rec.Code, rec.Body.String())
	}
	if !bytes.Equal(rec.Body.Bytes(), []byte("")) {
		t.Errorf("/diff body = %q, want empty (no unstaged changes)",
			rec.Body.String())
	}
}
