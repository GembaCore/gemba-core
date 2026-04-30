// /api/pools handler (gm-s47n.12, spec §10.1).
//
// Read-only endpoint exposing the daemon-managed pool state to the
// SPA. Returns one entry per (rig, persona) pool that the operator
// configured, with both the declared and effective sizes (so the
// MaxParallel clamp is observable post-startup) plus per-member rows
// projecting the OrchestrationPlane's session inventory.
//
// No nonce gate — read-only.

package server

import (
	"net/http"
	"sync"
	"time"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/config"
)

// PoolMember is the wire shape of one pool slot's row.
type PoolMember struct {
	SessionID           string     `json:"session_id"`
	PaneID              string     `json:"pane_id,omitempty"`
	Status              string     `json:"status"`
	LastBead            string     `json:"last_bead,omitempty"`
	BeadsDoneThisMember int        `json:"beads_done_this_member,omitempty"`
	LastRecycleAt       *time.Time `json:"last_recycle_at,omitempty"`
}

// PoolStateEntry is one (rig, persona) pool's exposed state.
type PoolStateEntry struct {
	Rig                 string       `json:"rig"`
	Persona             string       `json:"persona"`
	SizeTargetDeclared  int          `json:"size_target_declared"`
	SizeTargetEffective int          `json:"size_target_effective"`
	SizeActual          int          `json:"size_actual"`
	Idle                int          `json:"idle"`
	Working             int          `json:"working"`
	Members             []PoolMember `json:"members"`
}

// PoolState is the response envelope for GET /api/pools.
type PoolState struct {
	Pools      []PoolStateEntry `json:"pools"`
	CapturedAt time.Time        `json:"captured_at"`
}

// poolsRegistry holds the resolved pool config. Populated at startup
// by AttachPools so the read endpoint sees declared vs effective
// sizes. Routes return an empty list when the registry is unbound
// (Phase 0 zero-delta — no pools configured).
type poolsRegistry struct {
	mu       sync.RWMutex
	resolved []config.ResolvedPool
}

// AttachPools binds the resolved pool list to the router. cmd/gemba
// serve calls this once at startup after Resolve runs. Calling with
// a zero-length slice is the "no pools configured" path — the
// endpoint then returns an empty list.
func (r *Router) AttachPools(resolved []config.ResolvedPool) {
	if r.pools == nil {
		r.pools = &poolsRegistry{}
	}
	r.pools.mu.Lock()
	r.pools.resolved = append([]config.ResolvedPool(nil), resolved...)
	r.pools.mu.Unlock()
}

// listPools handles GET /api/pools. Joins the resolved pool config
// with the OrchestrationPlane's live session inventory.
func (r *Router) listPools(w http.ResponseWriter, req *http.Request) {
	now := time.Now().UTC()
	out := PoolState{Pools: []PoolStateEntry{}, CapturedAt: now}

	if r.pools == nil {
		writeJSON(w, http.StatusOK, out)
		return
	}
	r.pools.mu.RLock()
	resolved := r.pools.resolved
	r.pools.mu.RUnlock()
	if len(resolved) == 0 {
		writeJSON(w, http.StatusOK, out)
		return
	}

	// Pull every session once; partition by persona.
	byPersona := map[string][]core.Session{}
	if r.host != nil && r.host.OrchestrationPlane() != nil {
		op := r.host.OrchestrationPlane()
		sessions, err := op.ListSessions(req.Context(), core.SessionFilter{})
		if err == nil {
			for _, s := range sessions {
				byPersona[s.Persona] = append(byPersona[s.Persona], s)
			}
		}
	}

	for _, p := range resolved {
		entry := PoolStateEntry{
			Rig:                 p.Rig,
			Persona:             p.Persona,
			SizeTargetDeclared:  p.SizeDeclared,
			SizeTargetEffective: p.SizeEffective,
			Members:             []PoolMember{},
		}
		for _, s := range byPersona[p.Persona] {
			// Skip terminal sessions for the actual-size count.
			if s.Status == core.SessionCompleted || s.Status == core.SessionFailed {
				continue
			}
			entry.SizeActual++
			switch s.Status {
			case core.SessionReady:
				entry.Idle++
			case core.SessionWorking, core.SessionPrompting:
				entry.Working++
			}
			pm := PoolMember{
				SessionID: s.ID,
				Status:    string(s.Status),
			}
			if s.ProviderMetadata != nil {
				if v, ok := s.ProviderMetadata["pane_id"].(string); ok {
					pm.PaneID = v
				}
				if v, ok := s.ProviderMetadata["bead_id"].(string); ok {
					pm.LastBead = v
				} else if v, ok := s.ProviderMetadata["prior_bead_id"].(string); ok {
					pm.LastBead = v
				}
				if v, ok := s.ProviderMetadata["recycled_at"].(string); ok {
					if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
						pm.LastRecycleAt = &t
					}
				}
			}
			entry.Members = append(entry.Members, pm)
		}
		out.Pools = append(out.Pools, entry)
	}
	writeJSON(w, http.StatusOK, out)
}
