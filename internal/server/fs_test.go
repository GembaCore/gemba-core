package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// gm-e12.21.1 — directory-listing endpoint tests.
//
// Coverage:
//   - happy path: lists a real temp dir, returns sorted entries
//   - has_beads_db hint: marked when a child contains .beads/
//   - rejects missing ?path
//   - rejects relative path
//   - rejects path outside the allow-list (escape via ../)
//   - rejects path that doesn't exist
//   - rejects path that is a file, not a directory
//   - parent slot is empty when at the allow-list root

// fsTestRouter mints a Router whose listFs handler can be exercised
// without spinning up the rest of the server. The endpoint is read-
// only and stateless, so a zero-value Router is sufficient.
func fsTestRouter() *Router {
	return &Router{}
}

// allowOnlyTempRoot replaces $HOME for the duration of t with a temp
// dir, so the allow-list check passes for paths under that root and
// fails for paths outside it. Returns the resolved absolute path.
func allowOnlyTempRoot(t *testing.T, dir string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", dir, err)
	}
	t.Setenv("HOME", resolved)
	// On macOS, /var/folders has multiple symlinks; tests under
	// t.TempDir() resolve through them. Set HOME to the resolved
	// canonical form so allowedRoots()'s symlink resolution lands on
	// a string the request path will also resolve to.
	return resolved
}

func TestListFs_HappyPath(t *testing.T) {
	tmp := t.TempDir()
	root := allowOnlyTempRoot(t, tmp)
	for _, sub := range []string{"alpha", "beta", "zulu"} {
		if err := os.Mkdir(filepath.Join(root, sub), 0o755); err != nil {
			t.Fatalf("mkdir %q: %v", sub, err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "readme.md"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	r := fsTestRouter()
	req := httptest.NewRequest(http.MethodGet, "/v1/fs/list?path="+root, nil)
	w := httptest.NewRecorder()
	r.listFs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var env fsListEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, w.Body.String())
	}
	if env.Path != root {
		t.Errorf("path: got %q want %q", env.Path, root)
	}
	if env.Home != root {
		t.Errorf("home: got %q want %q", env.Home, root)
	}
	// Dirs first (alpha, beta, zulu), then files (readme.md).
	gotNames := make([]string, 0, len(env.Entries))
	for _, e := range env.Entries {
		gotNames = append(gotNames, e.Name)
	}
	want := []string{"alpha", "beta", "zulu", "readme.md"}
	if strings.Join(gotNames, ",") != strings.Join(want, ",") {
		t.Errorf("entries: got %v want %v", gotNames, want)
	}
	// The first three are dirs; readme.md is a file.
	for i, e := range env.Entries[:3] {
		if e.Kind != "dir" {
			t.Errorf("entry %d %q: kind=%q want dir", i, e.Name, e.Kind)
		}
	}
	if env.Entries[3].Kind != "file" {
		t.Errorf("readme.md kind: got %q want file", env.Entries[3].Kind)
	}
	// Parent slot is empty when at the root (HOME).
	if env.Parent != "" {
		t.Errorf("parent at root: got %q want empty", env.Parent)
	}
}

func TestListFs_HasBeadsDBHint(t *testing.T) {
	tmp := t.TempDir()
	root := allowOnlyTempRoot(t, tmp)
	// project-with-beads/ contains a .beads/ subdir; project-naked/ does not.
	if err := os.MkdirAll(filepath.Join(root, "project-with-beads", ".beads"), 0o755); err != nil {
		t.Fatalf("mkdir .beads: %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, "project-naked"), 0o755); err != nil {
		t.Fatalf("mkdir naked: %v", err)
	}

	r := fsTestRouter()
	req := httptest.NewRequest(http.MethodGet, "/v1/fs/list?path="+root, nil)
	w := httptest.NewRecorder()
	r.listFs(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d", w.Code)
	}
	var env fsListEnvelope
	_ = json.Unmarshal(w.Body.Bytes(), &env)

	byName := map[string]fsListEntry{}
	for _, e := range env.Entries {
		byName[e.Name] = e
	}
	if !byName["project-with-beads"].HasBeadsDB {
		t.Error("project-with-beads: has_beads_db should be true")
	}
	if byName["project-naked"].HasBeadsDB {
		t.Error("project-naked: has_beads_db should be false")
	}
}

func TestListFs_RejectsMissingPath(t *testing.T) {
	r := fsTestRouter()
	req := httptest.NewRequest(http.MethodGet, "/v1/fs/list", nil)
	w := httptest.NewRecorder()
	r.listFs(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "missing required") {
		t.Errorf("body: got %s", w.Body.String())
	}
}

func TestListFs_RejectsRelativePath(t *testing.T) {
	r := fsTestRouter()
	req := httptest.NewRequest(http.MethodGet, "/v1/fs/list?path=relative/dir", nil)
	w := httptest.NewRecorder()
	r.listFs(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400", w.Code)
	}
}

func TestListFs_RejectsOutsideAllowList(t *testing.T) {
	tmp := t.TempDir()
	allowOnlyTempRoot(t, tmp)
	r := fsTestRouter()
	// Try /usr (a real path outside the test's HOME). Must be a path
	// that exists so the resolver succeeds; the allow-list rejection
	// is the layer under test.
	req := httptest.NewRequest(http.MethodGet, "/v1/fs/list?path=/usr", nil)
	w := httptest.NewRecorder()
	r.listFs(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status: got %d want 403; body=%s", w.Code, w.Body.String())
	}
}

func TestListFs_RejectsNonexistentPath(t *testing.T) {
	tmp := t.TempDir()
	allowOnlyTempRoot(t, tmp)
	r := fsTestRouter()
	req := httptest.NewRequest(http.MethodGet,
		"/v1/fs/list?path="+tmp+"/does-not-exist", nil)
	w := httptest.NewRecorder()
	r.listFs(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status: got %d want 404; body=%s", w.Code, w.Body.String())
	}
}

func TestListFs_RejectsFilePath(t *testing.T) {
	tmp := t.TempDir()
	root := allowOnlyTempRoot(t, tmp)
	filePath := filepath.Join(root, "a-file.txt")
	if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	r := fsTestRouter()
	req := httptest.NewRequest(http.MethodGet, "/v1/fs/list?path="+filePath, nil)
	w := httptest.NewRecorder()
	r.listFs(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400; body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "not a directory") {
		t.Errorf("body: got %s", w.Body.String())
	}
}

func TestListFs_ParentVisibleBelowRoot(t *testing.T) {
	tmp := t.TempDir()
	root := allowOnlyTempRoot(t, tmp)
	child := filepath.Join(root, "child")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	r := fsTestRouter()
	req := httptest.NewRequest(http.MethodGet, "/v1/fs/list?path="+child, nil)
	w := httptest.NewRecorder()
	r.listFs(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d body=%s", w.Code, w.Body.String())
	}
	var env fsListEnvelope
	_ = json.Unmarshal(w.Body.Bytes(), &env)
	if env.Parent != root {
		t.Errorf("parent: got %q want %q", env.Parent, root)
	}
}

func TestListFs_HiddenEntriesElided(t *testing.T) {
	tmp := t.TempDir()
	root := allowOnlyTempRoot(t, tmp)
	if err := os.Mkdir(filepath.Join(root, ".hidden"), 0o755); err != nil {
		t.Fatalf("mkdir hidden: %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, "visible"), 0o755); err != nil {
		t.Fatalf("mkdir visible: %v", err)
	}
	r := fsTestRouter()
	req := httptest.NewRequest(http.MethodGet, "/v1/fs/list?path="+root, nil)
	w := httptest.NewRecorder()
	r.listFs(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d body=%s", w.Code, w.Body.String())
	}
	var env fsListEnvelope
	_ = json.Unmarshal(w.Body.Bytes(), &env)
	for _, e := range env.Entries {
		if strings.HasPrefix(e.Name, ".") {
			t.Errorf("dotfile %q leaked into entries", e.Name)
		}
	}
	if len(env.Entries) != 1 || env.Entries[0].Name != "visible" {
		t.Errorf("entries: %v", env.Entries)
	}
}
