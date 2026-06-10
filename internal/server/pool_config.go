// /api/pool-config handler (gm-s47n.16, spec §7).
//
// GET returns the current pool.toml body + parsed PoolConfig +
// max_parallel + reserved_for_manual so the SPA editor can render its
// initial state. PUT writes a JSON-encoded PoolConfig back to disk via
// EncodePoolConfig.
//
// Both routes degrade cleanly when no path is configured:
//   - GET returns {path:"", body:"", parsed:{}} so the editor renders
//     a fresh-config form.
//   - PUT returns 400 path_not_configured so the operator knows to
//     set --pool-config first.

package server

import (
	"encoding/json"
	"net/http"
	"os"

	"github.com/GembaCore/gemba-core/internal/config"
)

// PoolConfigEnvelope is the wire shape both GET and PUT return.
// Mirrors web/src/api/poolConfig.ts.
type PoolConfigEnvelope struct {
	Path              string         `json:"path"`
	Body              string         `json:"body"`
	Parsed            poolConfigJSON `json:"parsed"`
	MaxParallel       int            `json:"max_parallel"`
	ReservedForManual int            `json:"reserved_for_manual"`
}

// poolConfigJSON is the JSON projection of config.PoolConfig. The
// underlying Go struct uses `toml:"-"` on Pools because BurntSushi
// can't round-trip nested-map shapes; for JSON we want them visible,
// so this view re-projects.
type poolConfigJSON struct {
	DefaultSize       int                                `json:"default_size"`
	DefaultPersona    string                             `json:"default_persona,omitempty"`
	DefaultFloor      float64                            `json:"default_floor"`
	ReservedForManual int                                `json:"reserved_for_manual"`
	Routing           map[string]string                  `json:"routing,omitempty"`
	Pools             map[string]map[string]poolEntryRow `json:"pools,omitempty"`
}

type poolEntryRow struct {
	Size                  int     `json:"size"`
	AgentType             string  `json:"agent_type,omitempty"`
	Floor                 float64 `json:"floor"`
	RecycleAfterBeads     int     `json:"recycle_after_beads"`
	IdleCeilingMinutes    int     `json:"idle_ceiling_minutes"`
	MinIntervalPerSession int     `json:"min_interval_per_session_seconds"`
	MaxConcurrent         int     `json:"max_concurrent"`
}

// poolConfigRegistry holds the routing-time editor state. cmd/gemba
// serve attaches both the file path and the host-level MaxParallel via
// AttachPoolConfig once at boot. Read-modify-write through the editor
// re-decodes the file on each GET — that's intentional, the editor is
// the source of truth and we don't cache.
type poolConfigRegistry struct {
	path              string
	maxParallel       int
	reservedForManual int
}

// AttachPoolConfig binds the editor's load/save target. path is the
// pool.toml filesystem path; maxParallel is the host-wide concurrent
// pane cap (the same value [pool] clamp arithmetic uses);
// reservedForManual is the [pool] reserved_for_manual default after
// the cascade. cmd/gemba serve calls this once at boot.
func (r *Router) AttachPoolConfig(path string, maxParallel, reservedForManual int) {
	r.poolConfig = &poolConfigRegistry{
		path:              path,
		maxParallel:       maxParallel,
		reservedForManual: reservedForManual,
	}
}

// getPoolConfig handles GET /api/pool-config.
func (r *Router) getPoolConfig(w http.ResponseWriter, _ *http.Request) {
	pc := r.poolConfig
	if pc == nil {
		writeJSON(w, http.StatusOK, PoolConfigEnvelope{Parsed: poolConfigJSON{}})
		return
	}

	env := PoolConfigEnvelope{
		Path:              pc.path,
		MaxParallel:       pc.maxParallel,
		ReservedForManual: pc.reservedForManual,
	}

	if pc.path != "" {
		body, err := os.ReadFile(pc.path)
		switch {
		case err == nil:
			env.Body = string(body)
		case os.IsNotExist(err):
			env.Body = ""
		default:
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"error":   "read_failed",
				"message": err.Error(),
			})
			return
		}
	}
	cfg, err := config.DecodePoolConfig([]byte(env.Body))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error":   "decode_failed",
			"message": err.Error(),
		})
		return
	}
	env.Parsed = projectPoolConfig(cfg)
	writeJSON(w, http.StatusOK, env)
}

// putPoolConfig handles PUT /api/pool-config (nonce-gated).
func (r *Router) putPoolConfig(w http.ResponseWriter, req *http.Request) {
	pc := r.poolConfig
	if pc == nil || pc.path == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "path_not_configured",
			"message": "server was started without --pool-config; cannot write",
		})
		return
	}
	var body poolConfigJSON
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "invalid_body",
			"message": "request body must be a PoolConfig JSON object",
		})
		return
	}
	cfg := unprojectPoolConfig(body)
	rendered, err := config.EncodePoolConfig(cfg)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error":   "encode_failed",
			"message": err.Error(),
		})
		return
	}
	if err := os.WriteFile(pc.path, rendered, 0o600); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error":   "write_failed",
			"message": err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, PoolConfigEnvelope{
		Path:              pc.path,
		Body:              string(rendered),
		Parsed:            projectPoolConfig(cfg),
		MaxParallel:       pc.maxParallel,
		ReservedForManual: pc.reservedForManual,
	})
}

func projectPoolConfig(c config.PoolConfig) poolConfigJSON {
	out := poolConfigJSON{
		DefaultSize:       c.DefaultSize,
		DefaultPersona:    c.DefaultPersona,
		DefaultFloor:      c.DefaultFloor,
		ReservedForManual: c.ReservedForManual,
		Routing:           c.Routing,
	}
	if len(c.Pools) > 0 {
		out.Pools = map[string]map[string]poolEntryRow{}
		for scope, byPersona := range c.Pools {
			row := map[string]poolEntryRow{}
			for persona, e := range byPersona {
				row[persona] = poolEntryRow{
					Size:                  e.Size,
					AgentType:             e.AgentType,
					Floor:                 e.Floor,
					RecycleAfterBeads:     e.RecycleAfterBeads,
					IdleCeilingMinutes:    e.IdleCeilingMinutes,
					MinIntervalPerSession: e.MinIntervalPerSession,
					MaxConcurrent:         e.MaxConcurrent,
				}
			}
			out.Pools[scope] = row
		}
	}
	return out
}

func unprojectPoolConfig(j poolConfigJSON) config.PoolConfig {
	out := config.PoolConfig{
		DefaultSize:       j.DefaultSize,
		DefaultPersona:    j.DefaultPersona,
		DefaultFloor:      j.DefaultFloor,
		ReservedForManual: j.ReservedForManual,
		Routing:           j.Routing,
		Pools:             map[string]map[string]config.PoolEntry{},
	}
	for scope, byPersona := range j.Pools {
		out.Pools[scope] = map[string]config.PoolEntry{}
		for persona, e := range byPersona {
			out.Pools[scope][persona] = config.PoolEntry{
				Size:                  e.Size,
				AgentType:             e.AgentType,
				Floor:                 e.Floor,
				RecycleAfterBeads:     e.RecycleAfterBeads,
				IdleCeilingMinutes:    e.IdleCeilingMinutes,
				MinIntervalPerSession: e.MinIntervalPerSession,
				MaxConcurrent:         e.MaxConcurrent,
			}
		}
	}
	return out
}
