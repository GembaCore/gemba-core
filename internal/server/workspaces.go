// /api/v1/workspaces handler (gm-e4.1).
//
// Surfaces the union of workspace_kinds declared by the bound
// WorkPlane + OrchestrationPlane manifests, plus the per-kind
// isolation matrix. The SPA reads this to gate workspace-related
// chrome (e.g. "show worktree picker only when at least one bound
// adaptor declares WorkspaceWorktree").
//
// Read-only; no nonce. 503 with adaptor_not_configured when no
// orchestration plane is bound — the workspace_kinds primitive
// lives on the orchestration manifest today.

package server

import (
	"net/http"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/server/httperr"
)

// workspaceKindEntry is one row of /api/v1/workspaces. Kind is the
// canonical WorkspaceKind name; Default flips true for the
// adaptor's DefaultWorkspaceKind. Isolation surfaces the
// per-kind isolation matrix the manifest declared (FSScoped,
// NetIsolated, ProcIsolated — exact fields per
// core.IsolationCapabilities).
type workspaceKindEntry struct {
	Kind      core.WorkspaceKind         `json:"kind"`
	Default   bool                       `json:"default,omitempty"`
	Isolation core.IsolationCapabilities `json:"isolation,omitempty"`
}

// workspaceKindsEnvelope is the response body of GET /api/v1/workspaces.
// Shape mirrors the Phase-12 response convention (capabilities, sprints,
// etc.): list under a named key + total count.
type workspaceKindsEnvelope struct {
	WorkspaceKinds []workspaceKindEntry `json:"workspace_kinds"`
	Total          int                  `json:"total"`
}

// listWorkspaces returns the orchestration plane's declared
// workspace kinds. Read-only. When no orchestration plane is
// bound the handler emits 503 adaptor_not_configured — same
// shape /api/escalations uses.
func (r *Router) listWorkspaces(w http.ResponseWriter, _ *http.Request) {
	if r.host == nil {
		emptyWorkspaces(w)
		return
	}
	op := r.host.OrchestrationPlane()
	if op == nil {
		emptyWorkspaces(w)
		return
	}
	m := op.Describe()
	out := make([]workspaceKindEntry, 0, len(m.WorkspaceKinds))
	for _, k := range m.WorkspaceKinds {
		out = append(out, workspaceKindEntry{
			Kind:      k,
			Default:   k == m.DefaultWorkspaceKind,
			Isolation: m.PerKindIsolation[k],
		})
	}
	writeJSON(w, http.StatusOK, workspaceKindsEnvelope{
		WorkspaceKinds: out,
		Total:          len(out),
	})
}

// emptyWorkspaces returns a 200 with an empty list rather than a
// 503. Workspace kinds are advisory metadata — the SPA's gating
// already treats an empty list as "no workspace surface to render."
// Matches /api/escalations + /api/agents conventions for unbound
// adaptors (they return empty + 200, not 503; this preserves the
// convention).
func emptyWorkspaces(w http.ResponseWriter) {
	writeJSON(w, http.StatusOK, workspaceKindsEnvelope{
		WorkspaceKinds: []workspaceKindEntry{},
		Total:          0,
	})
}

// _unused keeps the import live until httperr is used in this file
// (a future iteration may surface adaptor errors via the shared
// envelope; the current implementation degrades to empty).
var _ = httperr.Write
