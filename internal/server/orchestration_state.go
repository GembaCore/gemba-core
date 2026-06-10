// /api/orchestration/state handler (gm-s47n.16, spec §7).
//
// Adaptor-aware projection of OrchestrationPlane.ListAgents +
// ListGroups for the pool editor. The native adaptor returns a single
// implicit scope; the gt adaptor returns one scope per rig with its
// polecats enumerated.
//
// Returns 503 when no orchestration plane is bound — the SPA's
// editor branches on that and renders the "no adaptor" placeholder.

package server

import (
	"net/http"
	"sort"
	"strings"

	"github.com/GembaCore/gemba-core/core"
)

// OrchestrationState is the wire shape /api/orchestration/state
// returns. Mirrors web/src/api/orchestrationState.ts.
type OrchestrationState struct {
	AdaptorID  string            `json:"adaptor_id"`
	Scopes     []OrchestrationSc `json:"scopes"`
	AgentTypes []string          `json:"agent_types"`
	Polecats   []OrchestrationPC `json:"polecats,omitempty"`
}

// OrchestrationSc — one scope row. For native this is the single
// implicit local scope ("local"). For gt this is one rig.
type OrchestrationSc struct {
	ID       string   `json:"id"`
	Kind     string   `json:"kind"` // "rig" | "local"
	Personas []string `json:"personas"`
}

// OrchestrationPC — one polecat row, gt-only. Empty on native.
type OrchestrationPC struct {
	Scope   string `json:"scope"`
	Persona string `json:"persona"`
	Name    string `json:"name"`
	State   string `json:"state,omitempty"`
}

func (r *Router) orchestrationState(w http.ResponseWriter, req *http.Request) {
	if r.host == nil || r.host.OrchestrationPlane() == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error":   "adaptor_not_configured",
			"message": "no orchestration plane bound",
		})
		return
	}
	op := r.host.OrchestrationPlane()
	adaptorID := op.Describe().AdaptorID

	state := OrchestrationState{
		AdaptorID: adaptorID,
		Scopes:    []OrchestrationSc{},
		Polecats:  []OrchestrationPC{},
	}

	// agents drives both the per-scope persona enumeration and the
	// polecat list. AgentTypes is left unset here — the SPA falls
	// back to /api/agents (the WorkPlane's registry, which is the
	// canonical source for agent runtime types like "claude" /
	// "codex"). AgentRef.Role is orchestration-role, not agent-type.
	agents, agentsErr := op.ListAgents(req.Context(), core.AgentFilter{})

	if groups, err := op.ListGroups(req.Context()); err == nil && agentsErr == nil {
		state.Scopes = projectScopes(adaptorID, groups, agents)
	}

	switch adaptorID {
	case "gastown":
		state.Polecats = projectPolecats(agents)
	case "native", "":
		// Synthesize one implicit local scope so the editor's
		// scope-axis-hidden path always has a target to write into.
		state.Scopes = []OrchestrationSc{{
			ID:       "local",
			Kind:     "local",
			Personas: []string{},
		}}
	}

	writeJSON(w, http.StatusOK, state)
}

// projectScopes turns gt rigs into scope rows. Each scope's
// `personas` array enumerates polecats (which carry the persona
// identity in their Name string).
func projectScopes(adaptorID string, groups []core.AgentGroup, agents []core.AgentRef) []OrchestrationSc {
	if adaptorID != "gastown" {
		return nil
	}
	rigs := map[string]struct{}{}
	for _, g := range groups {
		if rig, ok := g.Extension["rig"].(string); ok && rig != "" {
			rigs[rig] = struct{}{}
		}
	}
	// Personas per rig: derive from polecats (role == "polecats").
	personasByRig := map[string]map[string]struct{}{}
	for _, a := range agents {
		if a.Role != "polecats" || a.Workspace == "" {
			continue
		}
		// Polecat name carries persona; ListAgents formats as
		// "<rig>/polecats/<name>". Use the suffix.
		persona := strings.TrimPrefix(string(a.Name), a.Workspace+"/polecats/")
		if persona == "" {
			continue
		}
		if personasByRig[a.Workspace] == nil {
			personasByRig[a.Workspace] = map[string]struct{}{}
		}
		personasByRig[a.Workspace][persona] = struct{}{}
	}
	out := make([]OrchestrationSc, 0, len(rigs))
	for rig := range rigs {
		ps := []string{}
		for p := range personasByRig[rig] {
			ps = append(ps, p)
		}
		sort.Strings(ps)
		out = append(out, OrchestrationSc{
			ID:       rig,
			Kind:     "rig",
			Personas: ps,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func projectPolecats(agents []core.AgentRef) []OrchestrationPC {
	out := []OrchestrationPC{}
	for _, a := range agents {
		if a.Role != "polecats" {
			continue
		}
		name := strings.TrimPrefix(string(a.Name), a.Workspace+"/polecats/")
		out = append(out, OrchestrationPC{
			Scope:   a.Workspace,
			Persona: name,
			Name:    name,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Scope != out[j].Scope {
			return out[i].Scope < out[j].Scope
		}
		return out[i].Name < out[j].Name
	})
	return out
}
