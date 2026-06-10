// Tests for /api/repositories (gm-uipx.8). Three scenarios:
//
//   - happy: a populated .gemba/repositories/ dir lists every repo.
//   - missing dir: graceful 200 + empty list with a notice.
//   - explicit BeadsDir: handler probes <BeadsDir>/.gemba/repositories
//     not the cwd.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/config"
)

func writeRepoTOML(t *testing.T, dir, id string) {
	t.Helper()
	body := "id = \"" + id + "\"\n" +
		"path = \"/tmp/repos/" + id + "\"\n" +
		"default_branch = \"main\"\n" +
		"bead_prefix = \"" + id + "\"\n"
	if err := os.WriteFile(filepath.Join(dir, id+".toml"), []byte(body), 0o644); err != nil {
		t.Fatalf("write %s.toml: %v", id, err)
	}
}

func TestListRepositories_HappyPath(t *testing.T) {
	beads := t.TempDir()
	repos := filepath.Join(beads, ".gemba", "repositories")
	if err := os.MkdirAll(repos, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeRepoTOML(t, repos, "ge")
	writeRepoTOML(t, repos, "fe")

	h := NewRouter(config.ServeConfig{BeadsDir: beads}, fakeSPA(), nil)
	req := httptest.NewRequest(http.MethodGet, "/api/repositories", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d; body=%q", rec.Code, rec.Body.String())
	}
	var env struct {
		Repositories []core.Repository `json:"repositories"`
		Notice       string            `json:"notice,omitempty"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(env.Repositories) != 2 {
		t.Fatalf("expected 2 repos; got %d (%v)", len(env.Repositories), env.Repositories)
	}
	// Sorted ascending by id (RepositoryRegistry.List contract).
	if env.Repositories[0].ID != "fe" || env.Repositories[1].ID != "ge" {
		t.Errorf("ordering: %+v", env.Repositories)
	}
	if env.Notice != "" {
		t.Errorf("expected no notice on happy path; got %q", env.Notice)
	}
}

func TestListRepositories_MissingDirReturnsEmpty(t *testing.T) {
	beads := t.TempDir() // no .gemba/repositories
	h := NewRouter(config.ServeConfig{BeadsDir: beads}, fakeSPA(), nil)

	req := httptest.NewRequest(http.MethodGet, "/api/repositories", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d; body=%q", rec.Code, rec.Body.String())
	}
	var env struct {
		Repositories []core.Repository `json:"repositories"`
		Notice       string            `json:"notice,omitempty"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(env.Repositories) != 0 {
		t.Errorf("expected empty list; got %v", env.Repositories)
	}
	if env.Notice == "" {
		t.Error("expected non-empty notice when dir is missing")
	}
}
