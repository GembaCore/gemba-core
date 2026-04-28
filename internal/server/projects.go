// projects.go — /api/v1/projects endpoints (gm-root.18).
//
// Two endpoints:
//
//   GET  /api/v1/projects          — enumerate projects under default_dir
//   POST /api/v1/projects/switch   — switch the active workspace
//
// Project discovery uses config.ListProjectsUnder (shared with the
// gm-root.17.4 cold-start gate so the scanning logic is not duplicated).
//
// "Switch workspace" in v1 is a lightweight server-side ack: it records
// which project the operator selected and returns the project entry so
// the SPA can update its route + topbar label. Full server-side workspace
// isolation (re-binding the WorkPlane adaptor) is out of scope for this
// bead — the SPA navigates to the selected project and the operator
// re-launches gemba serve against the new directory for isolated backends.

package server

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/MikeBengtson/gemba/internal/config"
	"github.com/MikeBengtson/gemba/internal/server/httperr"
)

// projectEntry is one row in GET /api/v1/projects. Name is the directory
// basename; Path is the absolute disk path. Active is true for the
// project the operator most recently switched to (persisted in-process;
// resets on server restart).
type projectEntry struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Active bool   `json:"active,omitempty"`
}

// projectsEnvelope is the response body of GET /api/v1/projects.
type projectsEnvelope struct {
	Projects []projectEntry `json:"projects"`
	Total    int            `json:"total"`
}

// switchProjectRequest is the request body of POST /api/v1/projects/switch.
// The caller must supply either Name or Path; Name is preferred and is
// matched against the project's directory basename.
type switchProjectRequest struct {
	// Name is the project directory basename (preferred).
	Name string `json:"name,omitempty"`
	// Path is the absolute project root. Honored when Name is empty.
	Path string `json:"path,omitempty"`
}

// switchProjectResponse is the response body of POST /api/v1/projects/switch.
type switchProjectResponse struct {
	Active projectEntry `json:"active"`
}

// activeProjectMu guards activeProjectName. The field is in-process
// state only — a server restart resets it to "" (no active project).
// gm-root.17.7 (start-planning handoff) and future beads that need the
// active-workspace field read it via Router.ActiveProject().
var (
	activeProjectMu   sync.RWMutex
	activeProjectName string
)

// listProjects handles GET /api/v1/projects.
//
// Shared with gm-root.17.4 (cold-start gate) via config.ListProjectsUnder.
// gm-root.17.7 (start-planning handoff) can reach the same project list
// through this endpoint.
func (r *Router) listProjects(w http.ResponseWriter, _ *http.Request) {
	ucfg, err := config.LoadUserConfig(r.cfg.ConfigPath)
	if err != nil {
		httperr.Write(w, http.StatusInternalServerError,
			"config_load_failed", "failed to load user config: "+err.Error())
		return
	}
	defaultDir, err := config.ResolveDefaultDir(ucfg)
	if err != nil {
		httperr.Write(w, http.StatusInternalServerError,
			"config_resolve_failed", "failed to resolve default_dir: "+err.Error())
		return
	}
	found, err := config.ListProjectsUnder(defaultDir)
	if err != nil {
		httperr.Write(w, http.StatusInternalServerError,
			"projects_scan_failed", "failed to enumerate projects: "+err.Error())
		return
	}

	activeProjectMu.RLock()
	active := activeProjectName
	activeProjectMu.RUnlock()

	out := make([]projectEntry, 0, len(found))
	for _, p := range found {
		out = append(out, projectEntry{
			Name:   p.Name,
			Path:   p.Path,
			Active: active != "" && p.Name == active,
		})
	}
	writeJSON(w, http.StatusOK, projectsEnvelope{
		Projects: out,
		Total:    len(out),
	})
}

// switchProject handles POST /api/v1/projects/switch.
//
// Records the selected project as active (in-process) and returns it so
// the SPA can update its route and topbar label. gm-root.17.7 (start-
// planning handoff) calls this endpoint after ratification to switch the
// workspace to the newly-created project.
func (r *Router) switchProject(w http.ResponseWriter, req *http.Request) {
	var body switchProjectRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		httperr.Write(w, http.StatusBadRequest,
			"validation", "invalid request body: "+err.Error())
		return
	}
	if body.Name == "" && body.Path == "" {
		httperr.Write(w, http.StatusBadRequest,
			"validation", "name or path is required")
		return
	}

	ucfg, err := config.LoadUserConfig(r.cfg.ConfigPath)
	if err != nil {
		httperr.Write(w, http.StatusInternalServerError,
			"config_load_failed", "failed to load user config: "+err.Error())
		return
	}
	defaultDir, err := config.ResolveDefaultDir(ucfg)
	if err != nil {
		httperr.Write(w, http.StatusInternalServerError,
			"config_resolve_failed", "failed to resolve default_dir: "+err.Error())
		return
	}
	found, err := config.ListProjectsUnder(defaultDir)
	if err != nil {
		httperr.Write(w, http.StatusInternalServerError,
			"projects_scan_failed", "failed to enumerate projects: "+err.Error())
		return
	}

	// Find the requested project.
	var target *config.ProjectEntry
	for i := range found {
		p := &found[i]
		if body.Name != "" && p.Name == body.Name {
			target = p
			break
		}
		if body.Path != "" && p.Path == body.Path {
			target = p
			break
		}
	}
	if target == nil {
		httperr.Write(w, http.StatusNotFound,
			"project_not_found", "no project with that name or path was found")
		return
	}

	activeProjectMu.Lock()
	activeProjectName = target.Name
	activeProjectMu.Unlock()

	writeJSON(w, http.StatusOK, switchProjectResponse{
		Active: projectEntry{
			Name:   target.Name,
			Path:   target.Path,
			Active: true,
		},
	})
}

// ActiveProject returns the project name the operator most recently
// switched to, or an empty string if no switch has occurred this session.
// Exposed for gm-root.17.7 (start-planning handoff) so it can read the
// just-created project without re-scanning the disk.
func (r *Router) ActiveProject() string {
	activeProjectMu.RLock()
	defer activeProjectMu.RUnlock()
	return activeProjectName
}
