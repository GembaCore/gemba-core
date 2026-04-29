package native

import (
	"sort"
	"time"

	"github.com/GembaCore/gemba-core/core"
)

// SessionDispatchInfo summarizes a live session's state for the
// server-side dispatcher policy (gm-root.16.4). PaneID, InFlight, and
// MaxParallel are the inputs the policy needs to decide whether to
// route a new bead onto an existing session or spawn a new pane.
//
// AgentType is the registry name (e.g. "claude"), separate from
// AgentID (per-instance identifier on the core.Session).
type SessionDispatchInfo struct {
	SessionID   string
	PaneID      string
	AgentType   string
	Status      core.SessionStatus
	StartedAt   time.Time
	InFlight    int
	MaxParallel int
}

// SessionsByAgentType returns dispatch info for every live session of
// the given agent type that can accept a new bead. Sessions in
// terminal or initializing states are excluded — only Ready, Working,
// Prompting, and Stalled qualify (the dispatcher should still route
// to a Stalled session because the operator may unstick it; the
// alternative is spawning a fresh pane that competes for resources).
//
// Sorted by StartedAt ascending so the dispatcher's tiebreak ("oldest
// first") is stable across calls.
func (o *OrchestrationPlane) SessionsByAgentType(agentType string) []SessionDispatchInfo {
	o.mu.Lock()
	// Resolve the cap once so every candidate carries the same value;
	// drift between agents.toml reload and live dispatch is the kind
	// of subtle bug that's worth structurally avoiding.
	maxParallel := 1
	if a, ok := o.cfg.Registry.Get(agentType); ok {
		maxParallel = a.ResolvedMaxParallel()
	}
	out := make([]SessionDispatchInfo, 0)
	for _, s := range o.sessions {
		at, _ := s.ProviderMetadata["agent_type"].(string)
		if at != agentType {
			continue
		}
		if !canAcceptDispatch(s.Status) {
			continue
		}
		paneID, _ := s.ProviderMetadata["pane_id"].(string)
		out = append(out, SessionDispatchInfo{
			SessionID:   s.ID,
			PaneID:      paneID,
			AgentType:   at,
			Status:      s.Status,
			StartedAt:   s.StartedAt,
			InFlight:    o.paneInFlightLocked(paneID),
			MaxParallel: maxParallel,
		})
	}
	o.mu.Unlock()

	sort.Slice(out, func(i, j int) bool {
		return out[i].StartedAt.Before(out[j].StartedAt)
	})
	return out
}

// canAcceptDispatch reports whether a session in this status can be
// handed a new bead. Initializing sessions have a half-attached pane
// (preamble still in flight) — sending a second bead before the first
// turn is a recipe for the agent's Ink TUI swallowing input.
func canAcceptDispatch(s core.SessionStatus) bool {
	switch s {
	case core.SessionReady, core.SessionWorking, core.SessionPrompting, core.SessionStalled:
		return true
	}
	return false
}
