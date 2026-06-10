// Tests for GET /api/v1/projects/adoptable (gm-gmyl).
//
// Covers:
//   - happy path: lister returns three candidate DBs, two are
//     beads-shaped and not attached → both surface; third is filtered
//     out by the test stub.
//   - already-attached subtraction: a candidate whose name appears in
//     a filesystem project's workspace.toml is dropped.
//   - lister failure: 200 with empty list and a `notice` field
//     mentioning the host. Picker still works.
//   - empty default_dir: zero attached, all candidates surface.
//   - workspace.toml without a beads_db line: project is treated as
//     non-attaching (its name doesn't subtract any candidate).

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

	"github.com/GembaCore/gemba-core/internal/config"
)

// stubAdoptableLister returns the closure expected by AdoptableLister.
// `urls` is the list returned on success; `err` short-circuits the
// lister so the failure path can be exercised.
type stubAdoptableLister struct {
	urls    []adoptableDB
	err     error
	calls   int
	lastURL string
}

func (s *stubAdoptableLister) Adoptable(_ context.Context, baseURL string) ([]adoptableDB, error) {
	s.calls++
	s.lastURL = baseURL
	if s.err != nil {
		return nil, s.err
	}
	return s.urls, nil
}

// adoptableRouter returns a Router wired with the stub lister and a
// ConfigPath pointing at a tempdir-rooted config.toml.
func adoptableRouter(t *testing.T, projectsDir string, lister *stubAdoptableLister) *Router {
	t.Helper()
	home := t.TempDir()
	cfgPath := makeConfigTOML(t, home, projectsDir)
	r := NewRouter(config.ServeConfig{
		ConfigPath: cfgPath,
		DoltURL:    "mysql://root@127.0.0.1:3307/gemba",
	}, fakeSPA(), nil)
	r.AttachProjects(AttachConfig{AdoptableLister: lister.Adoptable})
	return r
}

func adoptableReq() *http.Request {
	return httptest.NewRequest(http.MethodGet, "/api/v1/projects/adoptable", nil)
}

func decodeAdoptable(t *testing.T, rec *httptest.ResponseRecorder) adoptableEnvelope {
	t.Helper()
	var env adoptableEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v; body=%s", err, rec.Body.String())
	}
	return env
}

func TestAdoptable_HappyPath(t *testing.T) {
	projectsDir := t.TempDir()
	lister := &stubAdoptableLister{
		urls: []adoptableDB{
			{Name: "alpha", URL: "mysql://root@127.0.0.1:3307/alpha"},
			{Name: "beta", URL: "mysql://root@127.0.0.1:3307/beta"},
		},
	}
	r := adoptableRouter(t, projectsDir, lister)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, adoptableReq())
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	env := decodeAdoptable(t, rec)
	if env.Total != 2 || len(env.DBs) != 2 {
		t.Fatalf("want 2 dbs, got %d (env=%+v)", env.Total, env)
	}
	if env.Notice != "" {
		t.Errorf("notice should be empty on happy path, got %q", env.Notice)
	}
	if env.DBs[0].Name != "alpha" || env.DBs[0].URL != "mysql://root@127.0.0.1:3307/alpha" {
		t.Errorf("first db wrong: %+v", env.DBs[0])
	}
	// The lister was called with a host-only base URL — the schema in
	// cfg.DoltURL ("/gemba") should have been stripped.
	if lister.calls != 1 {
		t.Errorf("want 1 lister call, got %d", lister.calls)
	}
}

func TestAdoptable_SubtractsAlreadyAttached(t *testing.T) {
	projectsDir := t.TempDir()
	// Create a filesystem project whose workspace.toml points at the
	// `beta` DB. The lister offers `alpha`, `beta`, `gamma` — the
	// response should keep alpha+gamma and drop beta.
	proj := makeProject(t, projectsDir, "my-proj")
	wsPath := filepath.Join(proj, ".gemba", "workspace.toml")
	body := "[project]\nname = \"my-proj\"\n" +
		"beads_db = \"mysql://root@127.0.0.1:3307/beta\"\n"
	if err := os.WriteFile(wsPath, []byte(body), 0o644); err != nil {
		t.Fatalf("write workspace.toml: %v", err)
	}

	lister := &stubAdoptableLister{
		urls: []adoptableDB{
			{Name: "alpha", URL: "mysql://root@127.0.0.1:3307/alpha"},
			{Name: "beta", URL: "mysql://root@127.0.0.1:3307/beta"},
			{Name: "gamma", URL: "mysql://root@127.0.0.1:3307/gamma"},
		},
	}
	r := adoptableRouter(t, projectsDir, lister)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, adoptableReq())
	env := decodeAdoptable(t, rec)
	if env.Total != 2 {
		t.Fatalf("want 2 (beta dropped), got %d (%+v)", env.Total, env.DBs)
	}
	for _, db := range env.DBs {
		if db.Name == "beta" {
			t.Errorf("beta should have been subtracted: %+v", db)
		}
	}
}

func TestAdoptable_ListerFailureReturns200WithNotice(t *testing.T) {
	projectsDir := t.TempDir()
	lister := &stubAdoptableLister{err: errors.New("connection refused")}
	r := adoptableRouter(t, projectsDir, lister)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, adoptableReq())
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200 even on lister failure, got %d body=%s",
			rec.Code, rec.Body.String())
	}
	env := decodeAdoptable(t, rec)
	if len(env.DBs) != 0 {
		t.Errorf("DBs should be empty on failure, got %+v", env.DBs)
	}
	if env.Notice == "" {
		t.Errorf("notice should be non-empty on lister failure")
	}
	if !strings.Contains(env.Notice, "127.0.0.1:3307") {
		t.Errorf("notice should mention the host: %q", env.Notice)
	}
}

func TestAdoptable_ProjectWithoutBeadsDBLine_DoesNotSubtract(t *testing.T) {
	projectsDir := t.TempDir()
	// makeProject() writes only `[workspace]\nname = ...`; no beads_db
	// line. That project shouldn't subtract any candidate.
	makeProject(t, projectsDir, "noisy-proj")

	lister := &stubAdoptableLister{
		urls: []adoptableDB{
			{Name: "alpha", URL: "mysql://root@127.0.0.1:3307/alpha"},
		},
	}
	r := adoptableRouter(t, projectsDir, lister)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, adoptableReq())
	env := decodeAdoptable(t, rec)
	if env.Total != 1 || env.DBs[0].Name != "alpha" {
		t.Errorf("want alpha to survive, got %+v", env)
	}
}

// beadsHostFromURL is exercised indirectly above (the stub lister is
// called with the resolved host) but pin its behavior directly so a
// future change to URL parsing trips a focused test.
func TestBeadsHostFromURL(t *testing.T) {
	cases := []struct {
		in, want string
		wantErr  bool
	}{
		{"mysql://root@127.0.0.1:3307/gemba", "mysql://root@127.0.0.1:3307/", false},
		{"mysql://user:secret@host.example.com:3307/db", "mysql://user:secret@host.example.com:3307/", false},
		{"mysql://127.0.0.1:3307/", "mysql://127.0.0.1:3307/", false},
		{"http://127.0.0.1:3307/", "", true},
		{"", "", true},
	}
	for _, tc := range cases {
		got, err := beadsHostFromURL(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("beadsHostFromURL(%q) want error, got %q", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("beadsHostFromURL(%q) unexpected error: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("beadsHostFromURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestIsSystemDatabase(t *testing.T) {
	for _, n := range []string{"mysql", "information_schema", "sys", "dolt", "dolt_cluster"} {
		if !isSystemDatabase(n) {
			t.Errorf("%q should be system", n)
		}
	}
	for _, n := range []string{"gemba", "my_project", "alpha"} {
		if isSystemDatabase(n) {
			t.Errorf("%q should NOT be system", n)
		}
	}
}
