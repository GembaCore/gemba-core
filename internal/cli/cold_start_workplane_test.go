package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/GembaCore/gemba-core/internal/config"
	"github.com/GembaCore/gemba-core/internal/server"
)

// TestColdStartShouldSkipBind_BuiltInDefaultNoProjects locks in the
// gm-ygwe gate: when applyBeadsURLDefault tagged the URL as "default"
// and the default_dir contains no projects, the WorkPlane MUST NOT be
// bound. This is the cold-start case the bug report describes.
func TestColdStartShouldSkipBind_BuiltInDefaultNoProjects(t *testing.T) {
	defaultDir := t.TempDir()
	configPath := writeTempConfig(t, defaultDir)
	cfg := config.ServeConfig{ConfigPath: configPath}

	// Simulate the run-time flow: applyBeadsURLDefault first, then the
	// gate. The first call records BeadsURLSource="default".
	if err := applyBeadsURLDefault(&cfg); err != nil {
		t.Fatalf("applyBeadsURLDefault: %v", err)
	}
	if cfg.BeadsURLSource != "default" {
		t.Fatalf("BeadsURLSource = %q, want %q", cfg.BeadsURLSource, "default")
	}
	skip, err := coldStartShouldSkipBind(cfg)
	if err != nil {
		t.Fatalf("coldStartShouldSkipBind: %v", err)
	}
	if !skip {
		t.Fatal("want skip=true (default URL + no projects = cold start); got false")
	}
}

// TestColdStartShouldSkipBind_DefaultWithProjectsBinds verifies that an
// existing project under the default_dir UNLOCKS the bind even when the
// URL came from the built-in default — the operator clearly has at
// least one project on this machine and presumably wants gemba to talk
// to it.
func TestColdStartShouldSkipBind_DefaultWithProjectsBinds(t *testing.T) {
	defaultDir := t.TempDir()
	makeProjectT(t, defaultDir, "my-project")
	configPath := writeTempConfig(t, defaultDir)
	cfg := config.ServeConfig{ConfigPath: configPath}

	if err := applyBeadsURLDefault(&cfg); err != nil {
		t.Fatalf("applyBeadsURLDefault: %v", err)
	}
	skip, err := coldStartShouldSkipBind(cfg)
	if err != nil {
		t.Fatalf("coldStartShouldSkipBind: %v", err)
	}
	if skip {
		t.Fatal("want skip=false (default URL but a project exists); got true")
	}
}

// TestColdStartShouldSkipBind_ExplicitCLIBindsRegardless guarantees that
// an operator who passes --dolt-url is honored even on a fresh machine
// with no projects. The CLI flag is the override.
func TestColdStartShouldSkipBind_ExplicitCLIBindsRegardless(t *testing.T) {
	defaultDir := t.TempDir() // empty
	configPath := writeTempConfig(t, defaultDir)
	cfg := config.ServeConfig{
		ConfigPath: configPath,
		DoltURL:    "mysql://root@127.0.0.1:3307/explicit",
	}
	if err := applyBeadsURLDefault(&cfg); err != nil {
		t.Fatalf("applyBeadsURLDefault: %v", err)
	}
	if cfg.BeadsURLSource != "cli" {
		t.Fatalf("BeadsURLSource = %q, want %q", cfg.BeadsURLSource, "cli")
	}
	skip, err := coldStartShouldSkipBind(cfg)
	if err != nil {
		t.Fatalf("coldStartShouldSkipBind: %v", err)
	}
	if skip {
		t.Fatal("want skip=false (explicit --dolt-url overrides); got true")
	}
}

// TestColdStartShouldSkipBind_OperatorConfigOverrideBinds locks in that
// a [beads].url operator override in ~/.gemba/config.toml is honored
// even with no projects on disk — the operator has stated their intent.
func TestColdStartShouldSkipBind_OperatorConfigOverrideBinds(t *testing.T) {
	defaultDir := t.TempDir() // empty
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	content := "[projects]\ndefault_dir = \"" + defaultDir + "\"\n" +
		"[beads]\nurl = \"mysql://root@10.0.0.1:3307/operator-pick\"\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.ServeConfig{ConfigPath: cfgPath}
	if err := applyBeadsURLDefault(&cfg); err != nil {
		t.Fatalf("applyBeadsURLDefault: %v", err)
	}
	if cfg.BeadsURLSource != "config" {
		t.Fatalf("BeadsURLSource = %q, want %q", cfg.BeadsURLSource, "config")
	}
	skip, err := coldStartShouldSkipBind(cfg)
	if err != nil {
		t.Fatalf("coldStartShouldSkipBind: %v", err)
	}
	if skip {
		t.Fatal("want skip=false ([beads].url operator override); got true")
	}
}

// TestRegisterWorkPlane_ColdStartLeavesHostUnbound is the unit-level
// integration check (gm-ygwe DoD): with an empty default_dir AND the
// resolved URL coming from the built-in default, registerWorkPlane
// returns a Host with NO WorkPlane bound and a "cold-start" mode tag
// so the banner / SPA can render the empty state.
func TestRegisterWorkPlane_ColdStartLeavesHostUnbound(t *testing.T) {
	defaultDir := t.TempDir() // empty: no projects
	configPath := writeTempConfig(t, defaultDir)
	cfg := config.ServeConfig{ConfigPath: configPath}
	if err := applyBeadsURLDefault(&cfg); err != nil {
		t.Fatalf("applyBeadsURLDefault: %v", err)
	}

	reg, err := registerWorkPlane(context.Background(), cfg)
	if err != nil {
		t.Fatalf("registerWorkPlane: %v", err)
	}
	if reg == nil || reg.Host == nil {
		t.Fatal("want non-nil reg + reg.Host even in cold-start mode")
	}
	if got := reg.Host.WorkPlane(); got != nil {
		t.Errorf("want WorkPlane=nil in cold-start mode; got %T", got)
	}
	if reg.Mode != "cold-start" {
		t.Errorf("Mode = %q, want %q", reg.Mode, "cold-start")
	}
}

// TestColdStart_HTTPRoutesReturn503 wires the cold-start Host into the
// real Router and asserts that every project-data route returns the
// expected 503 adaptor_not_configured envelope. Mirrors the bug report:
// a fresh operator with a stray Dolt "gemba" DB must NOT see stale
// work surfaced — every read must fail closed.
func TestColdStart_HTTPRoutesReturn503(t *testing.T) {
	defaultDir := t.TempDir() // empty
	configPath := writeTempConfig(t, defaultDir)
	cfg := config.ServeConfig{ConfigPath: configPath, Listen: "127.0.0.1", Port: 0}
	if err := applyBeadsURLDefault(&cfg); err != nil {
		t.Fatalf("applyBeadsURLDefault: %v", err)
	}
	reg, err := registerWorkPlane(context.Background(), cfg)
	if err != nil {
		t.Fatalf("registerWorkPlane: %v", err)
	}
	router := server.NewRouter(cfg, nil, reg.Host)
	defer router.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()

	cases := []struct {
		path        string
		wantStatus  int
		wantPanic   bool
		wantErrCode string
	}{
		{path: "/api/work-items", wantStatus: http.StatusServiceUnavailable, wantErrCode: "adaptor_not_configured"},
		{path: "/api/sprints", wantStatus: http.StatusServiceUnavailable, wantErrCode: "adaptor_not_configured"},
		{path: "/api/v1/workitems", wantStatus: http.StatusServiceUnavailable, wantErrCode: "adaptor_not_configured"},
		{path: "/api/capabilities", wantStatus: http.StatusServiceUnavailable, wantErrCode: "adaptor_not_configured"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			resp, err := http.Get(srv.URL + tc.path)
			if err != nil {
				t.Fatalf("GET %s: %v", tc.path, err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.wantStatus {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("GET %s: status=%d, want=%d (body=%s)",
					tc.path, resp.StatusCode, tc.wantStatus, body)
			}
			var env map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
				t.Fatalf("decode envelope: %v", err)
			}
			if got, _ := env["error"].(string); got != tc.wantErrCode {
				t.Errorf("error code = %q, want %q (full=%v)", got, tc.wantErrCode, env)
			}
		})
	}
}

// TestColdStart_NonProjectRoutesStillWork verifies non-project routes
// (health, openapi, the new-project flow surface) keep responding even
// when the WorkPlane is unbound — cold-start mode is "no project bound,"
// not "server broken."
func TestColdStart_NonProjectRoutesStillWork(t *testing.T) {
	defaultDir := t.TempDir() // empty
	configPath := writeTempConfig(t, defaultDir)
	cfg := config.ServeConfig{ConfigPath: configPath, Listen: "127.0.0.1", Port: 0}
	if err := applyBeadsURLDefault(&cfg); err != nil {
		t.Fatalf("applyBeadsURLDefault: %v", err)
	}
	reg, err := registerWorkPlane(context.Background(), cfg)
	if err != nil {
		t.Fatalf("registerWorkPlane: %v", err)
	}
	router := server.NewRouter(cfg, nil, reg.Host)
	defer router.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/health")
	if err != nil {
		t.Fatalf("GET /api/health: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/api/health: status=%d, want=200", resp.StatusCode)
	}
}
