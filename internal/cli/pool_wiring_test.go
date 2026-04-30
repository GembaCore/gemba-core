package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/GembaCore/gemba-core/internal/config"
)

// TestLoadAndResolvePools_ZeroDelta confirms the Phase 0 contract:
// no pool config file → no resolved pools → no daemons. Existing
// serve tests rely on this — gm-s47n.12 spec §11.1.
func TestLoadAndResolvePools_ZeroDelta(t *testing.T) {
	cfg := config.ServeConfig{}
	resolved, _, err := loadAndResolvePools(cfg)
	if err != nil {
		t.Fatalf("zero-delta load: %v", err)
	}
	if len(resolved) != 0 {
		t.Errorf("zero-delta should yield no pools; got %+v", resolved)
	}
}

func TestLoadAndResolvePools_WithFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pool.toml")
	body := []byte(`
[pool]
default_floor = 0.6

[pool.gemba.engineer-claude]
size = 2
agent_type = "claude"
`)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.ServeConfig{PoolConfigPath: path}
	resolved, poolCfg, err := loadAndResolvePools(cfg)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(resolved) != 1 {
		t.Fatalf("expected 1 resolved pool, got %d", len(resolved))
	}
	if resolved[0].SizeEffective != 2 {
		t.Errorf("size = %d, want 2", resolved[0].SizeEffective)
	}
	if resolved[0].AgentType != "claude" {
		t.Errorf("agent_type = %q", resolved[0].AgentType)
	}
	if resolved[0].Floor != 0.6 {
		t.Errorf("floor = %v (cascade)", resolved[0].Floor)
	}
	if poolCfg.DefaultFloor != 0.6 {
		t.Errorf("returned poolCfg.default_floor = %v", poolCfg.DefaultFloor)
	}
}

func TestLoadAndResolvePools_MissingFileNoError(t *testing.T) {
	cfg := config.ServeConfig{PoolConfigPath: "/definitely/not/here.toml"}
	resolved, _, err := loadAndResolvePools(cfg)
	if err != nil {
		t.Errorf("missing file should not error; got %v", err)
	}
	if len(resolved) != 0 {
		t.Errorf("missing file should yield no pools; got %+v", resolved)
	}
}
