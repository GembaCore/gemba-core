// Pool config (gm-s47n.12, scope-renamed in gm-s47n.16). Loads the
// [pool] table from a TOML file and resolves per-pool overrides
// against scope-level defaults.
//
// Phase 0 zero-delta is the default: an unconfigured server (no
// [pool.<scope>.<persona>] block with size > 0) constructs no daemons.
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
// Naming (gm-s47n.16, spec §2): the first axis of the pool key is now
// `scope` rather than `rig`. The TOML decoder accepts both
// `[pool.<scope>.<persona>]` and the legacy `[pool.<rig>.<persona>]`
// for one release; the latter logs a WARN once per process. Phase 3
// of the migration drops the alias.

package config

import (
	"fmt"
	"log/slog"
	"os"
	"sync"

	"github.com/BurntSushi/toml"
)

// DefaultReservedForManual is the default number of pane slots held
// back from the pool so a manual operator drag is never starved by a
// saturated pool. Operators tune via [pool] reserved_for_manual = N.
const DefaultReservedForManual = 1

// DefaultAutoDispatchFloor is the scope-level default minimum Layer 5
// Selection score (spec §8.1). Per-pool overrides via
// [pool.<scope>.<persona>] floor = N.
const DefaultAutoDispatchFloor = 0.5

// DefaultRecycleAfterBeads is the safety-belt recycle counter. 0
// disables; 5 is the suggested default (spec §4.4).
const DefaultRecycleAfterBeads = 5

// DefaultIdleCeilingMinutes is the default reaper threshold. Pool
// members idle past this long are reaped (spec §4.4).
const DefaultIdleCeilingMinutes = 30

// rigDeprecationOnce ensures the "rig is now scope" deprecation WARN
// fires at most once per process — multiple file loads (server +
// editor reload) shouldn't spam the log.
var rigDeprecationOnce sync.Once

// PoolConfig captures the [pool] table from gemba.toml — both the
// scope-level defaults and the per-pool overrides. Loaded by
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
//	# Per-pool overrides. Pool key = (scope, persona). Scope is implicit
//	# when running a single gemba server (native adaptor); for the gt
//	# adaptor it's the rig name. The legacy
//	# [pool.<rig>.<persona>] form remains accepted with a WARN.
//	size = 3                   # target pool members (clamped by
//	                           # MaxParallel - reserved_for_manual)
//	floor = 0.4                # overrides default_floor for THIS pool
//	recycle_after_beads = 5    # 0 disables
//	idle_ceiling_minutes = 30  # reaper threshold
type PoolConfig struct {
	// DefaultSize is the scope-level fallback pool size when a
	// per-pool block doesn't override. 0 means "no pool" — Phase 0
	// zero-delta. Operators opt in by setting per-pool size > 0.
	DefaultSize int `toml:"default_size"`

	// DefaultPersona is the server-level persona fallback for the
	// three-layer routing cascade (spec §3.2 / §12 q8). Used when
	// a bead has no extras `persona` field AND no
	// [pool.routing.<kind>] entry covers its kind.
	DefaultPersona string `toml:"default_persona"`

	// DefaultFloor is the scope-level auto-dispatch score floor
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

	// Pools is the per-(scope, persona) override map. Indexed by
	// scope name first (e.g. "gemba" — for the native adaptor a
	// singleton; for gt the rig name) then by persona id.
	Pools map[string]map[string]PoolEntry `toml:"-"`
}

// PoolEntry holds the per-(scope, persona) overrides. Zero values
// cascade to the scope-level defaults.
type PoolEntry struct {
	// Size is the declared target count of pool members. Subject
	// to the MaxParallel clamp (see EffectiveSize). 0 means "fall
	// back to PoolConfig.DefaultSize"; -1 means "explicitly disable
	// this pool".
	Size int `toml:"size"`

	// AgentType names the agents.toml entry the daemon dispatches
	// against (e.g. "claude", "shell-only"). Threaded through to
	// SessionPrompt as `gemba:agent_type` so the native adaptor's
	// pane-reuse path can find a slot. Empty defaults to "claude".
	AgentType string `toml:"agent_type"`

	// Floor overrides PoolConfig.DefaultFloor for this pool.
	Floor float64 `toml:"floor"`

	// RecycleAfterBeads bounds profile staleness.
	RecycleAfterBeads int `toml:"recycle_after_beads"`

	// IdleCeilingMinutes is the reaper threshold for this pool's
	// members.
	IdleCeilingMinutes int `toml:"idle_ceiling_minutes"`

	// MinIntervalPerSession is the per-session rate limit.
	MinIntervalPerSession int `toml:"min_interval_per_session_seconds"`

	// MaxConcurrent is the auto-dispatch concurrency cap for this
	// pool.
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

	// Second pass — pull the nested [pool.<scope>.<persona>] blocks.
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
	// Reserved keys at [pool] top level — anything else is a scope name.
	reserved := map[string]bool{
		"default_size":        true,
		"default_persona":     true,
		"default_floor":       true,
		"reserved_for_manual": true,
		"routing":             true,
	}
	// Track whether the file used the legacy `<rig>` shape for the
	// deprecation WARN. We can't distinguish `<rig>` from `<scope>`
	// from the keys alone — both are arbitrary strings — so the
	// heuristic is: if the file has scope blocks AND no top-level
	// migration-marker, emit the WARN once. Spec §10.3: drop alias
	// in Phase 3. Until then, every non-empty Pools triggers the
	// soft notice; operators editing via the SPA will see it once
	// then save with the new field name.
	if len(poolRaw) > len(reserved) {
		// At least one non-reserved key — i.e. a scope block exists.
		// We don't know the editor source, so assume legacy until
		// a future commit threads a `[pool.format = "scope"]` marker.
		warnRigDeprecationIfNeeded(poolRaw, reserved)
	}
	for scope, val := range poolRaw {
		if reserved[scope] {
			continue
		}
		scopeMap, ok := val.(map[string]any)
		if !ok {
			continue
		}
		cfg.Pools[scope] = map[string]PoolEntry{}
		for personaID, personaVal := range scopeMap {
			personaMap, ok := personaVal.(map[string]any)
			if !ok {
				continue
			}
			entry := decodePoolEntry(personaMap)
			cfg.Pools[scope][personaID] = entry
		}
	}
	return cfg, nil
}

// warnRigDeprecationIfNeeded fires the rig→scope deprecation WARN
// once per process. Wired via sync.Once so multiple PoolConfig loads
// (the editor's read-modify-write cycle) don't double-log.
//
// The signature accepts the parsed table so a future enhancement can
// inspect a [pool.format] marker to suppress the warning when the
// file was written by the SPA editor (which already uses the scope
// vocabulary). Today there's no such marker so the warning fires
// whenever any per-pool block is present.
func warnRigDeprecationIfNeeded(_ map[string]any, _ map[string]bool) {
	rigDeprecationOnce.Do(func() {
		slog.Warn("config: pool keys: 'rig' is now 'scope' — both forms accepted for one release; new writes use 'scope'",
			"see", "docs/design/pool-editor.md §2",
		)
	})
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
// per-pool overrides cascaded against the scope-level defaults and
// (for Size) clamped against MaxParallel.
type ResolvedPool struct {
	// Scope is the first axis of the pool key. For native it's the
	// implicit local server scope (e.g. "gemba"); for gt it's a rig
	// name. Renamed from `Rig` in gm-s47n.16 (spec §2). For one
	// release the Rig() method below returns the same string so
	// existing callers can migrate without an immediate flag day.
	Scope              string
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
	// because the MaxParallel-reserved_for_manual cap fired.
	ClampActivated bool
}

// Resolve walks every (scope, persona) entry, applies the scope-level
// default cascade, and clamps Size against MaxParallel -
// ReservedForManual. Pools with effective size <= 0 are dropped.
// Returns a deterministic-order slice (scope asc, persona asc).
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

	scopes := sortedKeys(c.Pools)
	for _, scope := range scopes {
		personas := sortedKeys(c.Pools[scope])
		for _, persona := range personas {
			entry := c.Pools[scope][persona]
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
				Scope:              scope,
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
// reserved_for_manual cap.
func LogClampWarnings(resolved []ResolvedPool, maxParallel, reserved int) {
	if reserved <= 0 {
		reserved = DefaultReservedForManual
	}
	for _, r := range resolved {
		if !r.ClampActivated {
			continue
		}
		slog.Warn("pool: size clamped by MaxParallel cap",
			"scope", r.Scope,
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
// Returns ("", false) when no layer resolves.
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

// EncodePoolConfig serialises a PoolConfig back to TOML in the editor's
// canonical scope-key shape. Used by the PUT /api/pool-config handler
// so the on-disk file always reflects the operator's last save in a
// stable, human-readable form.
//
// Key order: top-level [pool] table → [pool.routing] → [pool.<scope>.<persona>]
// blocks in scope-asc, persona-asc order. Deterministic so a no-op
// save produces an unchanged file.
func EncodePoolConfig(c PoolConfig) ([]byte, error) {
	// BurntSushi/toml.Marshal can't round-trip the nested
	// [pool.<scope>.<persona>] shape from our flat-by-design Go
	// struct (Pools is excluded from struct tags). Hand-render to
	// keep deterministic key order.
	var b bufBuilder
	b.WriteString("[pool]\n")
	if c.DefaultSize != 0 {
		b.WriteFmt("default_size = %d\n", c.DefaultSize)
	}
	if c.DefaultPersona != "" {
		b.WriteFmt("default_persona = %q\n", c.DefaultPersona)
	}
	if c.DefaultFloor != 0 {
		b.WriteFmt("default_floor = %v\n", c.DefaultFloor)
	}
	if c.ReservedForManual != 0 {
		b.WriteFmt("reserved_for_manual = %d\n", c.ReservedForManual)
	}
	if len(c.Routing) > 0 {
		b.WriteString("\n[pool.routing]\n")
		for _, k := range sortedKeys(c.Routing) {
			b.WriteFmt("%s = %q\n", k, c.Routing[k])
		}
	}
	for _, scope := range sortedKeys(c.Pools) {
		for _, persona := range sortedKeys(c.Pools[scope]) {
			e := c.Pools[scope][persona]
			b.WriteFmt("\n[pool.%s.%s]\n", scope, persona)
			if e.Size != 0 {
				b.WriteFmt("size = %d\n", e.Size)
			}
			if e.AgentType != "" {
				b.WriteFmt("agent_type = %q\n", e.AgentType)
			}
			if e.Floor != 0 {
				b.WriteFmt("floor = %v\n", e.Floor)
			}
			if e.RecycleAfterBeads != 0 {
				b.WriteFmt("recycle_after_beads = %d\n", e.RecycleAfterBeads)
			}
			if e.IdleCeilingMinutes != 0 {
				b.WriteFmt("idle_ceiling_minutes = %d\n", e.IdleCeilingMinutes)
			}
			if e.MinIntervalPerSession != 0 {
				b.WriteFmt("min_interval_per_session_seconds = %d\n", e.MinIntervalPerSession)
			}
			if e.MaxConcurrent != 0 {
				b.WriteFmt("max_concurrent = %d\n", e.MaxConcurrent)
			}
		}
	}
	return []byte(b.String()), nil
}

// bufBuilder is a tiny wrapper around strings.Builder that adds a
// printf-style helper. Local-only — keeps EncodePoolConfig terse.
type bufBuilder struct {
	parts []string
}

func (b *bufBuilder) WriteString(s string) { b.parts = append(b.parts, s) }
func (b *bufBuilder) WriteFmt(format string, args ...any) {
	b.parts = append(b.parts, fmt.Sprintf(format, args...))
}
func (b *bufBuilder) String() string {
	out := ""
	for _, p := range b.parts {
		out += p
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}
