// Pool config (gm-s47n.12). Loads the [pool] table from a TOML file
// and resolves per-pool overrides against rig-level defaults.
//
// Phase 0 zero-delta is the default: an unconfigured server (no
// [pool.<rig>.<persona>] block with size > 0) constructs no daemons.
// Behavior identical to today's main.
//
// MaxParallel clamp:
//
//	effective_size = min(declared_size, MaxParallel - reserved_for_manual)
//
// reserved_for_manual defaults to 1 so a saturated pool never starves
// manual operator drag. The clamp runs ONCE at config load; when it
// activates a WARN log line names declared/cap/effective sizes. The
// SPA's /api/pools endpoint surfaces both `size_target_declared` and
// `size_target_effective` so the clamp stays observable post-startup.
//
// Pool sizing and MaxParallel cross-reference (spec §3.3 documentation
// requirement): MaxParallel is the orchestration manifest's per-host
// cap on concurrent agent panes. Pool sizing is best-effort against
// it. Operators tuning pool size MUST also size MaxParallel — a pool
// of 5 against MaxParallel=2 silently clamps to 1 (MaxParallel -
// reserved_for_manual = 2 - 1).

package config

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/BurntSushi/toml"
)

// DefaultReservedForManual is the default number of pane slots held
// back from the pool so a manual operator drag is never starved by a
// saturated pool. Operators tune via [pool] reserved_for_manual = N.
const DefaultReservedForManual = 1

// DefaultAutoDispatchFloor is the rig-level default minimum Layer 5
// Selection score (spec §8.1). Per-pool overrides via
// [pool.<rig>.<persona>] floor = N.
const DefaultAutoDispatchFloor = 0.5

// DefaultRecycleAfterBeads is the safety-belt recycle counter. 0
// disables; 5 is the suggested default (spec §4.4).
const DefaultRecycleAfterBeads = 5

// DefaultIdleCeilingMinutes is the default reaper threshold. Pool
// members idle past this long are reaped (spec §4.4).
const DefaultIdleCeilingMinutes = 30

// PoolConfig captures the [pool] table from gemba.toml — both the
// rig-level defaults and the per-pool overrides. Loaded by
// LoadPoolConfig; the cascade is applied at daemon-construction time
// via Resolve.
//
// The TOML shape (all keys optional; an unconfigured PoolConfig is
// the Phase 0 zero-delta default — no daemons constructed):
//
//	[pool]
//	default_size = 0           # opt-in; 0 = no pools
//	default_persona = ""       # server-level persona fallback
//	default_floor = 0.5        # auto-dispatch score floor
//	reserved_for_manual = 1    # pool slots held back for manual drag
//
//	[pool.routing]
//	# Three-layer persona routing cascade (spec §3.2):
//	# bead extras `persona` field (highest)
//	#   > [pool.routing.<kind>] (this table)
//	#   > [pool] default_persona (lowest)
//	epic = "pm-claude"
//	bug = "engineer-claude"
//	decision = "pm-claude"
//
//	[pool.gemba.engineer-claude]
//	# Per-pool overrides. Pool key = (rig, persona). Rig is implicit
//	# when running a single gemba server; the daemon constructs one
//	# instance per (rig, persona) with size > 0.
//	size = 3                   # target pool members (clamped by
//	                           # MaxParallel - reserved_for_manual)
//	floor = 0.4                # overrides default_floor for THIS pool
//	recycle_after_beads = 5    # 0 disables
//	idle_ceiling_minutes = 30  # reaper threshold
type PoolConfig struct {
	// DefaultSize is the rig-level fallback pool size when a
	// per-pool block doesn't override. 0 means "no pool" — Phase 0
	// zero-delta. Operators opt in by setting per-pool size > 0;
	// the rig default is rarely useful (each persona's warm
	// context is distinct, see spec §3.2) but is supported for
	// uniform sizing across personas.
	DefaultSize int `toml:"default_size"`

	// DefaultPersona is the server-level persona fallback for the
	// three-layer routing cascade (spec §3.2 / §12 q8). Used when
	// a bead has no extras `persona` field AND no
	// [pool.routing.<kind>] entry covers its kind.
	DefaultPersona string `toml:"default_persona"`

	// DefaultFloor is the rig-level auto-dispatch score floor
	// (spec §8.1). 0 falls back to DefaultAutoDispatchFloor.
	DefaultFloor float64 `toml:"default_floor"`

	// ReservedForManual is the count of pane slots held back from
	// the pool. The clamp computes
	//
	//	effective_size = min(declared_size, MaxParallel - ReservedForManual)
	//
	// 0 falls back to DefaultReservedForManual (1).
	ReservedForManual int `toml:"reserved_for_manual"`

	// Routing maps bead kind → persona id for the middle layer of
	// the persona routing cascade (spec §3.2). When neither the
	// bead's extras nor this map resolve a persona, DefaultPersona
	// is used. When that's also empty, the daemon refuses
	// autodispatch and waits for manual drag.
	Routing map[string]string `toml:"routing"`

	// Pools is the per-(rig, persona) override map. Indexed by rig
	// name first (e.g. "gemba") then by persona id (e.g.
	// "engineer-claude"). Daemons are constructed for every entry
	// with effective size > 0 after the cascade resolves.
	Pools map[string]map[string]PoolEntry `toml:"-"`
}

// PoolEntry holds the per-(rig, persona) overrides. Zero values
// cascade to the rig-level defaults.
type PoolEntry struct {
	// Size is the declared target count of pool members. Subject
	// to the MaxParallel clamp (see EffectiveSize). 0 means "fall
	// back to PoolConfig.DefaultSize"; -1 means "explicitly disable
	// this pool" (rare, but lets an operator set a default and turn
	// off one specific pool).
	Size int `toml:"size"`

	// AgentType names the agents.toml entry the daemon dispatches
	// against (e.g. "claude", "shell-only"). Threaded through to
	// SessionPrompt as `gemba:agent_type` so the native adaptor's
	// pane-reuse path can find a slot. Empty defaults to "claude".
	AgentType string `toml:"agent_type"`

	// Floor overrides PoolConfig.DefaultFloor for this pool. Zero
	// cascades to DefaultFloor; DefaultFloor zero in turn cascades
	// to DefaultAutoDispatchFloor (0.5).
	Floor float64 `toml:"floor"`

	// RecycleAfterBeads bounds profile staleness. The daemon
	// recycles a session after this many beads even without
	// health-trip. 0 cascades to DefaultRecycleAfterBeads (5).
	// Negative disables the safety belt.
	RecycleAfterBeads int `toml:"recycle_after_beads"`

	// IdleCeilingMinutes is the reaper threshold for this pool's
	// members. 0 cascades to DefaultIdleCeilingMinutes (30).
	// Negative disables the reaper for this pool.
	IdleCeilingMinutes int `toml:"idle_ceiling_minutes"`

	// MinIntervalPerSession is the per-session rate limit (spec
	// §11; planner.DispatchPolicy). 0 cascades to the planner's
	// DefaultMinIntervalPerSession (5m).
	MinIntervalPerSession int `toml:"min_interval_per_session_seconds"`

	// MaxConcurrent is the auto-dispatch concurrency cap for this
	// pool (planner.DispatchPolicy). 0 cascades to
	// planner.DefaultMaxConcurrent.
	MaxConcurrent int `toml:"max_concurrent"`
}

// LoadPoolConfig reads + parses path. Returns a zero-value PoolConfig
// (Phase 0 zero-delta) when path is empty or the file does not exist.
// Other I/O / parse errors propagate so the operator sees a
// misconfigured file as an actionable startup failure.
func LoadPoolConfig(path string) (PoolConfig, error) {
	if path == "" {
		return PoolConfig{}, nil
	}
	body, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return PoolConfig{}, nil
		}
		return PoolConfig{}, fmt.Errorf("pool config %s: %w", path, err)
	}
	return DecodePoolConfig(body)
}

// DecodePoolConfig parses a TOML body. Exposed for testing without a
// real file on disk.
func DecodePoolConfig(body []byte) (PoolConfig, error) {
	// First pass — pull the flat [pool] keys.
	var top struct {
		Pool struct {
			DefaultSize       int               `toml:"default_size"`
			DefaultPersona    string            `toml:"default_persona"`
			DefaultFloor      float64           `toml:"default_floor"`
			ReservedForManual int               `toml:"reserved_for_manual"`
			Routing           map[string]string `toml:"routing"`
		} `toml:"pool"`
	}
	if _, err := toml.Decode(string(body), &top); err != nil {
		return PoolConfig{}, fmt.Errorf("pool config: decode top: %w", err)
	}
	cfg := PoolConfig{
		DefaultSize:       top.Pool.DefaultSize,
		DefaultPersona:    top.Pool.DefaultPersona,
		DefaultFloor:      top.Pool.DefaultFloor,
		ReservedForManual: top.Pool.ReservedForManual,
		Routing:           top.Pool.Routing,
		Pools:             map[string]map[string]PoolEntry{},
	}

	// Second pass — pull the nested [pool.<rig>.<persona>] blocks.
	// We decode into a generic map[string]any then walk it because
	// BurntSushi's toml package doesn't have a "rest of table" tag.
	var raw map[string]any
	if _, err := toml.Decode(string(body), &raw); err != nil {
		return PoolConfig{}, fmt.Errorf("pool config: decode raw: %w", err)
	}
	poolRaw, ok := raw["pool"].(map[string]any)
	if !ok {
		return cfg, nil // no [pool] table at all → zero-delta
	}
	// Reserved keys at [pool] top level — anything else is a rig name.
	reserved := map[string]bool{
		"default_size":        true,
		"default_persona":     true,
		"default_floor":       true,
		"reserved_for_manual": true,
		"routing":             true,
	}
	for rig, val := range poolRaw {
		if reserved[rig] {
			continue
		}
		rigMap, ok := val.(map[string]any)
		if !ok {
			continue
		}
		cfg.Pools[rig] = map[string]PoolEntry{}
		for personaID, personaVal := range rigMap {
			personaMap, ok := personaVal.(map[string]any)
			if !ok {
				continue
			}
			entry := decodePoolEntry(personaMap)
			cfg.Pools[rig][personaID] = entry
		}
	}
	return cfg, nil
}

func decodePoolEntry(m map[string]any) PoolEntry {
	var e PoolEntry
	if v, ok := m["size"].(int64); ok {
		e.Size = int(v)
	}
	if v, ok := m["floor"].(float64); ok {
		e.Floor = v
	}
	if v, ok := m["recycle_after_beads"].(int64); ok {
		e.RecycleAfterBeads = int(v)
	}
	if v, ok := m["idle_ceiling_minutes"].(int64); ok {
		e.IdleCeilingMinutes = int(v)
	}
	if v, ok := m["min_interval_per_session_seconds"].(int64); ok {
		e.MinIntervalPerSession = int(v)
	}
	if v, ok := m["max_concurrent"].(int64); ok {
		e.MaxConcurrent = int(v)
	}
	if v, ok := m["agent_type"].(string); ok {
		e.AgentType = v
	}
	return e
}

// ResolvedPool is one daemon's worth of effective config — the
// per-pool overrides cascaded against the rig-level defaults and (for
// Size) clamped against MaxParallel.
type ResolvedPool struct {
	Rig                string
	Persona            string
	AgentType          string  // "claude" default
	SizeDeclared       int     // before clamp
	SizeEffective      int     // after clamp (min with cap)
	Floor              float64 // auto-dispatch score floor
	RecycleAfterBeads  int
	IdleCeilingMinutes int
	MinIntervalSeconds int
	MaxConcurrent      int
	// ClampActivated is true when SizeEffective < SizeDeclared
	// because the MaxParallel-reserved_for_manual cap fired. The
	// caller logs a WARN and surfaces it via /api/pools.
	ClampActivated bool
}

// Resolve walks every (rig, persona) entry, applies the rig-level
// default cascade, and clamps Size against MaxParallel -
// ReservedForManual. Pools with effective size <= 0 are dropped.
// Returns a deterministic-order slice (rig asc, persona asc) so the
// caller's startup log is reproducible.
//
// maxParallel is the host-wide concurrent-pane cap (e.g. derived
// from the agents.toml `max_parallel` for the pool's agent_type, or
// from the orchestration manifest). 0 disables the clamp — the
// operator opted out of host-level parallelism control. The
// resulting effective_size = min(declared_size, maxParallel -
// reserved_for_manual). When the clamp activates, ResolvedPool.
// ClampActivated is true and LogClampWarnings emits a WARN.
func (c PoolConfig) Resolve(maxParallel int) []ResolvedPool {
	out := []ResolvedPool{}
	reserved := c.ReservedForManual
	if reserved <= 0 {
		reserved = DefaultReservedForManual
	}
	cap := -1
	if maxParallel > 0 {
		cap = maxParallel - reserved
		if cap < 0 {
			cap = 0
		}
	}

	rigs := sortedKeys(c.Pools)
	for _, rig := range rigs {
		personas := sortedKeys(c.Pools[rig])
		for _, persona := range personas {
			entry := c.Pools[rig][persona]
			declared := entry.Size
			if declared == 0 {
				declared = c.DefaultSize
			}
			if declared <= 0 {
				continue // disabled
			}
			effective := declared
			clamp := false
			if cap >= 0 && effective > cap {
				effective = cap
				clamp = true
			}
			if effective <= 0 {
				// Clamp pushed it below 1; skip — there's no
				// pane budget. The WARN at config load already
				// names the pool.
				continue
			}
			floor := entry.Floor
			if floor == 0 {
				floor = c.DefaultFloor
			}
			if floor == 0 {
				floor = DefaultAutoDispatchFloor
			}
			recycleAfter := entry.RecycleAfterBeads
			if recycleAfter == 0 {
				recycleAfter = DefaultRecycleAfterBeads
			}
			idleCeiling := entry.IdleCeilingMinutes
			if idleCeiling == 0 {
				idleCeiling = DefaultIdleCeilingMinutes
			}
			agentType := entry.AgentType
			if agentType == "" {
				agentType = "claude"
			}
			out = append(out, ResolvedPool{
				Rig:                rig,
				Persona:            persona,
				AgentType:          agentType,
				SizeDeclared:       declared,
				SizeEffective:      effective,
				Floor:              floor,
				RecycleAfterBeads:  recycleAfter,
				IdleCeilingMinutes: idleCeiling,
				MinIntervalSeconds: entry.MinIntervalPerSession,
				MaxConcurrent:      entry.MaxConcurrent,
				ClampActivated:     clamp,
			})
		}
	}
	return out
}

// LogClampWarnings emits a WARN slog line for every pool whose
// effective size was clamped down by the MaxParallel -
// reserved_for_manual cap. Spec §3.3: when the clamp activates,
// gemba MUST log a WARN naming the declared, cap, and effective
// sizes so the operator sees the difference at startup.
func LogClampWarnings(resolved []ResolvedPool, maxParallel, reserved int) {
	if reserved <= 0 {
		reserved = DefaultReservedForManual
	}
	for _, r := range resolved {
		if !r.ClampActivated {
			continue
		}
		slog.Warn("pool: size clamped by MaxParallel cap",
			"rig", r.Rig,
			"persona", r.Persona,
			"declared", r.SizeDeclared,
			"effective", r.SizeEffective,
			"max_parallel", maxParallel,
			"reserved_for_manual", reserved,
			"hint", "raise [agents].max_parallel or lower pool.size to silence")
	}
}

// ResolvePersona implements the three-layer persona routing cascade
// (spec §3.2). Highest precedence first:
//
//  1. Bead extras `persona` field (passed as beadPersona)
//  2. [pool.routing.<kind>] map for the bead's kind
//  3. [pool] default_persona
//
// Returns ("", false) when no layer resolves — the daemon then
// declines to autodispatch the bead and the manual drag is the only
// path forward. This is the documented OutcomeNoPersona path; logged
// so operators can find unrouted beads.
func (c PoolConfig) ResolvePersona(beadPersona, beadKind string) (string, bool) {
	if beadPersona != "" {
		return beadPersona, true
	}
	if c.Routing != nil {
		if v, ok := c.Routing[beadKind]; ok && v != "" {
			return v, true
		}
	}
	if c.DefaultPersona != "" {
		return c.DefaultPersona, true
	}
	return "", false
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	// Simple insertion sort to avoid pulling in sort for trivial sizes.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}
