package config

import (
	"testing"
)

func TestDecodePoolConfig_Empty(t *testing.T) {
	cfg, err := DecodePoolConfig([]byte(``))
	if err != nil {
		t.Fatalf("decode empty: %v", err)
	}
	if cfg.DefaultSize != 0 {
		t.Errorf("default_size = %d, want 0", cfg.DefaultSize)
	}
	if len(cfg.Pools) != 0 {
		t.Errorf("expected no pools, got %d", len(cfg.Pools))
	}
}

func TestDecodePoolConfig_RigLevelDefaults(t *testing.T) {
	body := []byte(`
[pool]
default_size = 0
default_persona = "engineer-claude"
default_floor = 0.6
reserved_for_manual = 2

[pool.routing]
epic = "pm-claude"
bug = "engineer-claude"
`)
	cfg, err := DecodePoolConfig(body)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if cfg.DefaultPersona != "engineer-claude" {
		t.Errorf("default_persona = %q", cfg.DefaultPersona)
	}
	if cfg.DefaultFloor != 0.6 {
		t.Errorf("default_floor = %v", cfg.DefaultFloor)
	}
	if cfg.ReservedForManual != 2 {
		t.Errorf("reserved_for_manual = %d", cfg.ReservedForManual)
	}
	if cfg.Routing["epic"] != "pm-claude" {
		t.Errorf("routing.epic = %q", cfg.Routing["epic"])
	}
}

func TestDecodePoolConfig_PerPoolBlocks(t *testing.T) {
	body := []byte(`
[pool]
default_floor = 0.5

[pool.gemba.engineer-claude]
size = 3
floor = 0.4
recycle_after_beads = 7
idle_ceiling_minutes = 45

[pool.gemba.pm-claude]
size = 1
`)
	cfg, err := DecodePoolConfig(body)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	rig, ok := cfg.Pools["gemba"]
	if !ok {
		t.Fatalf("missing pool.gemba")
	}
	eng, ok := rig["engineer-claude"]
	if !ok {
		t.Fatalf("missing engineer-claude entry")
	}
	if eng.Size != 3 {
		t.Errorf("engineer size = %d, want 3", eng.Size)
	}
	if eng.Floor != 0.4 {
		t.Errorf("engineer floor = %v", eng.Floor)
	}
	if eng.RecycleAfterBeads != 7 {
		t.Errorf("recycle_after_beads = %d", eng.RecycleAfterBeads)
	}
	if eng.IdleCeilingMinutes != 45 {
		t.Errorf("idle_ceiling = %d", eng.IdleCeilingMinutes)
	}
	pm := rig["pm-claude"]
	if pm.Size != 1 {
		t.Errorf("pm size = %d", pm.Size)
	}
}

func TestResolve_AppliesCascade(t *testing.T) {
	cfg := PoolConfig{
		DefaultFloor:      0.5,
		ReservedForManual: 1,
		Pools: map[string]map[string]PoolEntry{
			"gemba": {
				"engineer-claude": {Size: 3, Floor: 0.4},
				"pm-claude":       {Size: 1},
			},
		},
	}
	resolved := cfg.Resolve(10)
	if len(resolved) != 2 {
		t.Fatalf("want 2 resolved pools, got %d", len(resolved))
	}
	for _, r := range resolved {
		if r.ClampActivated {
			t.Errorf("unexpected clamp on %s/%s", r.Scope, r.Persona)
		}
	}
	// Expect deterministic order: engineer-claude before pm-claude.
	if resolved[0].Persona != "engineer-claude" {
		t.Errorf("expected engineer-claude first, got %s", resolved[0].Persona)
	}
	if resolved[0].Floor != 0.4 {
		t.Errorf("engineer floor = %v, want 0.4 (override)", resolved[0].Floor)
	}
	if resolved[1].Floor != 0.5 {
		t.Errorf("pm floor = %v, want 0.5 (cascade)", resolved[1].Floor)
	}
	if resolved[1].RecycleAfterBeads != DefaultRecycleAfterBeads {
		t.Errorf("pm recycle = %d, want default", resolved[1].RecycleAfterBeads)
	}
}

func TestResolve_ClampActivates(t *testing.T) {
	cfg := PoolConfig{
		ReservedForManual: 1,
		Pools: map[string]map[string]PoolEntry{
			"gemba": {
				"engineer-claude": {Size: 5},
			},
		},
	}
	// MaxParallel=3, reserved=1 → cap=2, but declared=5 → effective=2.
	resolved := cfg.Resolve(3)
	if len(resolved) != 1 {
		t.Fatalf("want 1 resolved pool, got %d", len(resolved))
	}
	r := resolved[0]
	if !r.ClampActivated {
		t.Errorf("clamp should have activated")
	}
	if r.SizeDeclared != 5 {
		t.Errorf("declared = %d, want 5", r.SizeDeclared)
	}
	if r.SizeEffective != 2 {
		t.Errorf("effective = %d, want 2 (3 - 1)", r.SizeEffective)
	}
}

func TestResolve_ClampToZeroSkipsPool(t *testing.T) {
	cfg := PoolConfig{
		ReservedForManual: 1,
		Pools: map[string]map[string]PoolEntry{
			"gemba": {
				"engineer-claude": {Size: 5},
			},
		},
	}
	// MaxParallel=1 → cap=0 → effective=0 → drop pool entirely.
	resolved := cfg.Resolve(1)
	if len(resolved) != 0 {
		t.Errorf("want 0 resolved pools (clamp to zero), got %d", len(resolved))
	}
}

func TestResolve_ZeroSizeSkipped(t *testing.T) {
	cfg := PoolConfig{
		Pools: map[string]map[string]PoolEntry{
			"gemba": {
				"engineer-claude": {Size: 0}, // no rig default; skip
			},
		},
	}
	resolved := cfg.Resolve(10)
	if len(resolved) != 0 {
		t.Errorf("want 0 resolved pools (size=0), got %d", len(resolved))
	}
}

func TestResolve_NoMaxParallelDisablesClamp(t *testing.T) {
	cfg := PoolConfig{
		Pools: map[string]map[string]PoolEntry{
			"gemba": {
				"engineer-claude": {Size: 100},
			},
		},
	}
	// maxParallel = 0 → no clamp.
	resolved := cfg.Resolve(0)
	if len(resolved) != 1 {
		t.Fatalf("want 1 resolved pool, got %d", len(resolved))
	}
	if resolved[0].ClampActivated {
		t.Errorf("clamp should not activate when maxParallel=0")
	}
	if resolved[0].SizeEffective != 100 {
		t.Errorf("effective = %d, want 100", resolved[0].SizeEffective)
	}
}

func TestResolvePersona_BeadOverrideWins(t *testing.T) {
	cfg := PoolConfig{
		DefaultPersona: "engineer-claude",
		Routing:        map[string]string{"epic": "pm-claude"},
	}
	got, ok := cfg.ResolvePersona("override-claude", "epic")
	if !ok || got != "override-claude" {
		t.Errorf("bead override should win: got=%q ok=%v", got, ok)
	}
}

func TestResolvePersona_KindRoutingMiddle(t *testing.T) {
	cfg := PoolConfig{
		DefaultPersona: "engineer-claude",
		Routing:        map[string]string{"epic": "pm-claude"},
	}
	got, ok := cfg.ResolvePersona("", "epic")
	if !ok || got != "pm-claude" {
		t.Errorf("kind routing should match: got=%q ok=%v", got, ok)
	}
}

func TestResolvePersona_DefaultFallback(t *testing.T) {
	cfg := PoolConfig{
		DefaultPersona: "engineer-claude",
		Routing:        map[string]string{"epic": "pm-claude"},
	}
	got, ok := cfg.ResolvePersona("", "bug")
	if !ok || got != "engineer-claude" {
		t.Errorf("default should match: got=%q ok=%v", got, ok)
	}
}

func TestResolvePersona_NoMatch(t *testing.T) {
	cfg := PoolConfig{}
	got, ok := cfg.ResolvePersona("", "bug")
	if ok || got != "" {
		t.Errorf("no match: got=%q ok=%v", got, ok)
	}
}

func TestLoadPoolConfig_MissingFileNoError(t *testing.T) {
	cfg, err := LoadPoolConfig("/definitely/does/not/exist.toml")
	if err != nil {
		t.Errorf("missing file should be non-error (zero-delta), got %v", err)
	}
	if len(cfg.Pools) != 0 {
		t.Errorf("missing file should yield empty Pools, got %d", len(cfg.Pools))
	}
}

func TestLoadPoolConfig_EmptyPathNoError(t *testing.T) {
	cfg, err := LoadPoolConfig("")
	if err != nil {
		t.Errorf("empty path should be non-error, got %v", err)
	}
	if len(cfg.Pools) != 0 {
		t.Errorf("empty path should yield empty Pools, got %d", len(cfg.Pools))
	}
}

// TestDecodePoolConfig_AcceptsBothScopeAndRigKeys verifies the
// gm-s47n.16 deprecation alias: TOML files using either
// [pool.<scope>.<persona>] or the legacy [pool.<rig>.<persona>] decode
// to the same Go shape. The decoder doesn't tag which axis-name the
// file used — Pools is just map[string]map[string]PoolEntry — but
// both files MUST yield equivalent in-memory state.
func TestDecodePoolConfig_AcceptsBothScopeAndRigKeys(t *testing.T) {
	// Same content, different operator vocabulary.
	rigForm := []byte(`
[pool.gemba.engineer-claude]
size = 2
`)
	scopeForm := []byte(`
[pool.gemba.engineer-claude]
size = 2
`)
	cfgRig, err := DecodePoolConfig(rigForm)
	if err != nil {
		t.Fatalf("decode rig form: %v", err)
	}
	cfgScope, err := DecodePoolConfig(scopeForm)
	if err != nil {
		t.Fatalf("decode scope form: %v", err)
	}
	if cfgRig.Pools["gemba"]["engineer-claude"].Size != 2 {
		t.Errorf("rig form: size = %d want 2", cfgRig.Pools["gemba"]["engineer-claude"].Size)
	}
	if cfgScope.Pools["gemba"]["engineer-claude"].Size != 2 {
		t.Errorf("scope form: size = %d want 2", cfgScope.Pools["gemba"]["engineer-claude"].Size)
	}
}

func TestEncodePoolConfig_RoundTrips(t *testing.T) {
	src := PoolConfig{
		DefaultSize:       0,
		DefaultPersona:    "engineer-claude",
		DefaultFloor:      0.5,
		ReservedForManual: 1,
		Routing:           map[string]string{"epic": "pm-claude"},
		Pools: map[string]map[string]PoolEntry{
			"gemba": {
				"engineer-claude": {
					Size:               3,
					AgentType:          "claude",
					Floor:              0.4,
					RecycleAfterBeads:  5,
					IdleCeilingMinutes: 30,
				},
			},
		},
	}
	body, err := EncodePoolConfig(src)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := DecodePoolConfig(body)
	if err != nil {
		t.Fatalf("decode round-trip: %v", err)
	}
	if got.DefaultPersona != "engineer-claude" {
		t.Errorf("default_persona = %q after round-trip", got.DefaultPersona)
	}
	if got.Pools["gemba"]["engineer-claude"].Size != 3 {
		t.Errorf("pool size = %d after round-trip", got.Pools["gemba"]["engineer-claude"].Size)
	}
	if got.Pools["gemba"]["engineer-claude"].Floor != 0.4 {
		t.Errorf("pool floor = %v after round-trip", got.Pools["gemba"]["engineer-claude"].Floor)
	}
	if got.Routing["epic"] != "pm-claude" {
		t.Errorf("routing.epic = %q after round-trip", got.Routing["epic"])
	}
}
