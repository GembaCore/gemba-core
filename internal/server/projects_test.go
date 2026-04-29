// Tests for GET /api/v1/projects and POST /api/v1/projects/switch (gm-root.18).
//
// Scenarios:
//   - happy path: default_dir with two projects, returns both.
//   - empty state: default_dir exists but has no project subdirs.
//   - missing default_dir: treated as empty list, not an error.
//   - switch to a known project: returns it as active.
//   - switch to unknown project: 404.
//   - switch with empty body: 400 validation.
//   - list after switch: active flag set on the switched-to project.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/MikeBengtson/gemba/internal/config"
)

// makeProject creates a minimal project directory with a .gemba/workspace.toml
// file in the given parent dir. Returns the created project path.
func makeProject(t *testing.T, parent, name string) string {
	t.Helper()
	projectDir := filepath.Join(parent, name)
	gembaDir := filepath.Join(projectDir, ".gemba")
	if err := os.MkdirAll(gembaDir, 0o755); err != nil {
		t.Fatalf("makeProject %s: mkdir: %v", name, err)
	}
	wsToml := filepath.Join(gembaDir, "workspace.toml")
	if err := os.WriteFile(wsToml, []byte("[workspace]\nname = \""+name+"\"\n"), 0o644); err != nil {
		t.Fatalf("makeProject %s: write workspace.toml: %v", name, err)
	}
	return projectDir
}

// makeConfigTOML writes a ~/.gemba/config.toml pointing at defaultDir.
// Returns the path to the config file.
func makeConfigTOML(t *testing.T, home, defaultDir string) string {
	t.Helper()
	gembaDir := filepath.Join(home, ".gemba")
	if err := os.MkdirAll(gembaDir, 0o755); err != nil {
		t.Fatalf("makeConfigTOML: mkdir: %v", err)
	}
	cfgPath := filepath.Join(gembaDir, "config.toml")
	content := "[projects]\ndefault_dir = \"" + defaultDir + "\"\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatalf("makeConfigTOML: write: %v", err)
	}
	return cfgPath
}

// projectsRouter builds a Router with a config pointing at the given
// projects dir. The config.toml is written to a temp home dir so the
// test doesn't touch the real ~/.gemba/config.toml.
func projectsRouter(t *testing.T, projectsDir string) *Router {
	t.Helper()
	home := t.TempDir()
	cfgPath := makeConfigTOML(t, home, projectsDir)
	return NewRouter(config.ServeConfig{ConfigPath: cfgPath}, fakeSPA(), nil)
}

func TestListProjects_HappyPath(t *testing.T) {
	projectsDir := t.TempDir()
	makeProject(t, projectsDir, "alpha")
	makeProject(t, projectsDir, "beta")

	h := projectsRouter(t, projectsDir)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d; body=%q", rec.Code, rec.Body.String())
	}
	var env projectsEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v; body=%q", err, rec.Body.String())
	}
	if env.Total != 2 {
		t.Errorf("want Total=2, got %d", env.Total)
	}
	if len(env.Projects) != 2 {
		t.Fatalf("want 2 projects, got %d", len(env.Projects))
	}
	// ReadDir returns alphabetical order — alpha before beta.
	if env.Projects[0].Name != "alpha" || env.Projects[1].Name != "beta" {
		t.Errorf("order: %v", env.Projects)
	}
	for _, p := range env.Projects {
		if p.Path == "" {
			t.Errorf("project %s has empty Path", p.Name)
		}
		if p.Active {
			t.Errorf("project %s should not be active before any switch", p.Name)
		}
	}
}

func TestListProjects_EmptyDir(t *testing.T) {
	projectsDir := t.TempDir() // exists but has no project subdirs

	h := projectsRouter(t, projectsDir)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d; body=%q", rec.Code, rec.Body.String())
	}
	var env projectsEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Total != 0 {
		t.Errorf("want Total=0, got %d", env.Total)
	}
	if len(env.Projects) != 0 {
		t.Errorf("want empty Projects, got %v", env.Projects)
	}
}

func TestListProjects_MissingDefaultDir(t *testing.T) {
	// Point default_dir at a path that doesn't exist — should not error.
	projectsDir := filepath.Join(t.TempDir(), "nonexistent-projects-dir")

	h := projectsRouter(t, projectsDir)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d; body=%q", rec.Code, rec.Body.String())
	}
	var env projectsEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Total != 0 {
		t.Errorf("want Total=0, got %d", env.Total)
	}
}

func TestSwitchProject_ByName(t *testing.T) {
	// Reset global state from any previous test run.
	activeProjectMu.Lock()
	activeProjectName = ""
	activeProjectMu.Unlock()

	projectsDir := t.TempDir()
	makeProject(t, projectsDir, "alpha")
	makeProject(t, projectsDir, "beta")

	h := projectsRouter(t, projectsDir)

	body, _ := json.Marshal(switchProjectRequest{Name: "beta"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/switch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d; body=%q", rec.Code, rec.Body.String())
	}
	var resp switchProjectResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v; body=%q", err, rec.Body.String())
	}
	if resp.Active.Name != "beta" {
		t.Errorf("want Active.Name=beta, got %q", resp.Active.Name)
	}
	if !resp.Active.Active {
		t.Error("want Active.Active=true")
	}
}

func TestSwitchProject_ByPath(t *testing.T) {
	activeProjectMu.Lock()
	activeProjectName = ""
	activeProjectMu.Unlock()

	projectsDir := t.TempDir()
	alphaPath := makeProject(t, projectsDir, "alpha")

	h := projectsRouter(t, projectsDir)

	body, _ := json.Marshal(switchProjectRequest{Path: alphaPath})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/switch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d; body=%q", rec.Code, rec.Body.String())
	}
	var resp switchProjectResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Active.Name != "alpha" {
		t.Errorf("want Active.Name=alpha, got %q", resp.Active.Name)
	}
}

func TestSwitchProject_NotFound(t *testing.T) {
	activeProjectMu.Lock()
	activeProjectName = ""
	activeProjectMu.Unlock()

	projectsDir := t.TempDir()
	makeProject(t, projectsDir, "alpha")

	h := projectsRouter(t, projectsDir)

	body, _ := json.Marshal(switchProjectRequest{Name: "does-not-exist"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/switch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d; body=%q", rec.Code, rec.Body.String())
	}
}

func TestSwitchProject_EmptyBody(t *testing.T) {
	projectsDir := t.TempDir()
	h := projectsRouter(t, projectsDir)

	body, _ := json.Marshal(switchProjectRequest{}) // both Name and Path empty
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/switch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d; body=%q", rec.Code, rec.Body.String())
	}
}

func TestListProjects_ActiveFlagAfterSwitch(t *testing.T) {
	activeProjectMu.Lock()
	activeProjectName = ""
	activeProjectMu.Unlock()

	projectsDir := t.TempDir()
	makeProject(t, projectsDir, "alpha")
	makeProject(t, projectsDir, "beta")

	h := projectsRouter(t, projectsDir)

	// Switch to alpha.
	switchBody, _ := json.Marshal(switchProjectRequest{Name: "alpha"})
	switchReq := httptest.NewRequest(http.MethodPost, "/api/v1/projects/switch", bytes.NewReader(switchBody))
	switchReq.Header.Set("Content-Type", "application/json")
	switchRec := httptest.NewRecorder()
	h.ServeHTTP(switchRec, switchReq)
	if switchRec.Code != http.StatusOK {
		t.Fatalf("switch: want 200, got %d", switchRec.Code)
	}

	// Now list — alpha should be active, beta should not.
	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
	listRec := httptest.NewRecorder()
	h.ServeHTTP(listRec, listReq)

	var env projectsEnvelope
	if err := json.Unmarshal(listRec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var activeCount int
	for _, p := range env.Projects {
		if p.Active {
			activeCount++
			if p.Name != "alpha" {
				t.Errorf("unexpected active project: %q", p.Name)
			}
		}
	}
	if activeCount != 1 {
		t.Errorf("want 1 active project, got %d; projects=%v", activeCount, env.Projects)
	}
}

// ─── gm-root.17.14: classification + extra_roots discovery ──────────

// makeBindableEntry creates parent/name with a .beads/ but no
// .gemba/workspace.toml — i.e. a KindNeedsWorkspace candidate.
func makeBindableEntry(t *testing.T, parent, name string) string {
	t.Helper()
	dir := filepath.Join(parent, name, ".beads")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("makeBindableEntry: %v", err)
	}
	return filepath.Join(parent, name)
}

// makeNeedsRepoEntry creates parent/name with both a .beads/ AND a
// .gemba/workspace.toml but no .git/ — i.e. a KindNeedsRepo candidate.
func makeNeedsRepoEntry(t *testing.T, parent, name string) string {
	t.Helper()
	dir := filepath.Join(parent, name)
	if err := os.MkdirAll(filepath.Join(dir, ".beads"), 0o755); err != nil {
		t.Fatalf("seed beads: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".gemba"), 0o755); err != nil {
		t.Fatalf("seed gemba: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gemba", "workspace.toml"),
		[]byte("[workspace]\nname = \""+name+"\"\n"), 0o644); err != nil {
		t.Fatalf("seed workspace.toml: %v", err)
	}
	return dir
}

// makeCompleteEntry creates parent/name with .beads/, .gemba/workspace.toml,
// AND .git/ — KindComplete.
func makeCompleteEntry(t *testing.T, parent, name string) string {
	t.Helper()
	dir := makeNeedsRepoEntry(t, parent, name)
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatalf("seed .git: %v", err)
	}
	return dir
}

func TestListProjects_ClassifiesKinds(t *testing.T) {
	projectsDir := t.TempDir()
	makeBindableEntry(t, projectsDir, "needs-ws")
	makeNeedsRepoEntry(t, projectsDir, "needs-repo")
	makeCompleteEntry(t, projectsDir, "complete")

	h := projectsRouter(t, projectsDir)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d; body=%q", rec.Code, rec.Body.String())
	}
	var env projectsEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Total != 3 {
		t.Fatalf("want 3, got %d: %+v", env.Total, env.Projects)
	}
	byName := map[string]projectEntry{}
	for _, p := range env.Projects {
		byName[p.Name] = p
	}
	if byName["needs-ws"].Kind != "needs_workspace" {
		t.Errorf("needs-ws kind = %q", byName["needs-ws"].Kind)
	}
	if byName["needs-repo"].Kind != "needs_repo" {
		t.Errorf("needs-repo kind = %q", byName["needs-repo"].Kind)
	}
	if byName["complete"].Kind != "complete" {
		t.Errorf("complete kind = %q", byName["complete"].Kind)
	}
}

// projectsRouterWithExtraRoots writes a config.toml with both
// default_dir and extra_roots, and returns a router pointed at it.
func projectsRouterWithExtraRoots(t *testing.T, projectsDir string, extraRoots []string) *Router {
	t.Helper()
	home := t.TempDir()
	gembaDir := filepath.Join(home, ".gemba")
	if err := os.MkdirAll(gembaDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cfgPath := filepath.Join(gembaDir, "config.toml")
	body := "[projects]\ndefault_dir = \"" + projectsDir + "\"\nextra_roots = ["
	for i, r := range extraRoots {
		if i > 0 {
			body += ", "
		}
		body += "\"" + r + "\""
	}
	body += "]\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o644); err != nil {
		t.Fatalf("write cfg: %v", err)
	}
	return NewRouter(config.ServeConfig{ConfigPath: cfgPath}, fakeSPA(), nil)
}

func TestListProjects_ExtraRoots_Scanned(t *testing.T) {
	projectsDir := t.TempDir()
	makeProject(t, projectsDir, "primary")

	extraRoot := t.TempDir()
	makeBindableEntry(t, extraRoot, "extra-bd-only")

	h := projectsRouterWithExtraRoots(t, projectsDir, []string{extraRoot})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d; body=%q", rec.Code, rec.Body.String())
	}
	var env projectsEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Total != 2 {
		t.Fatalf("want 2 entries (primary + extra), got %d: %+v", env.Total, env.Projects)
	}
	names := []string{env.Projects[0].Name, env.Projects[1].Name}
	if names[0] != "primary" || names[1] != "extra-bd-only" {
		t.Errorf("ordering: %v", names)
	}
}

func TestListProjects_ExtraRoots_MissingDoesNotFail(t *testing.T) {
	projectsDir := t.TempDir()
	makeProject(t, projectsDir, "primary")

	missing := filepath.Join(t.TempDir(), "no-such-extra-root")

	h := projectsRouterWithExtraRoots(t, projectsDir, []string{missing})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200 (missing extra_root must not fail), got %d; body=%q", rec.Code, rec.Body.String())
	}
	var env projectsEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Total != 1 || env.Projects[0].Name != "primary" {
		t.Errorf("want only primary, got %+v", env.Projects)
	}
}

func TestListProjects_ExtraRoots_DedupsAcrossRoots(t *testing.T) {
	projectsDir := t.TempDir()
	makeProject(t, projectsDir, "shared")

	// Same dir as both default_dir and extra_root → dedup must collapse.
	h := projectsRouterWithExtraRoots(t, projectsDir, []string{projectsDir})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	var env projectsEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Total != 1 {
		t.Errorf("want 1 deduped entry, got %d: %+v", env.Total, env.Projects)
	}
}
