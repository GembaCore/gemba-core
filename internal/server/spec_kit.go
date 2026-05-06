// /api/spec-kit/* exposes Spec Kit planning artifacts as a first-class
// refinement source and syncs approved artifacts into Beads.
package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/server/httperr"
	"github.com/GembaCore/gemba-core/internal/speckit"
)

func (r *Router) getSpecKitWorkspace(w http.ResponseWriter, req *http.Request) {
	workspace, err := speckit.NewScanner(r.specKitRoot()).Workspace(req.Context())
	if err != nil {
		httperr.Write(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, workspace)
}

func (r *Router) listSpecKitFeatures(w http.ResponseWriter, req *http.Request) {
	result, err := speckit.NewScanner(r.specKitRoot()).List(req.Context())
	if err != nil {
		httperr.Write(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (r *Router) getSpecKitFeature(w http.ResponseWriter, req *http.Request) {
	id := strings.TrimSpace(chi.URLParam(req, "id"))
	if id == "" {
		httperr.Write(w, http.StatusBadRequest, "validation", "missing Spec Kit feature id")
		return
	}
	feature, err := speckit.NewScanner(r.specKitRoot()).Load(req.Context(), id)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			httperr.Write(w, http.StatusNotFound, "not_found", "Spec Kit feature not found")
			return
		}
		httperr.Write(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, feature)
}

func (r *Router) getSpecKitFile(w http.ResponseWriter, req *http.Request) {
	path := strings.TrimSpace(req.URL.Query().Get("path"))
	if path == "" {
		httperr.Write(w, http.StatusBadRequest, "validation", "missing Spec Kit file path")
		return
	}
	file, err := speckit.NewScanner(r.specKitRoot()).ReadWorkspaceFile(req.Context(), path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			httperr.Write(w, http.StatusNotFound, "not_found", "Spec Kit file not found")
			return
		}
		httperr.Write(w, http.StatusBadRequest, "validation", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, file)
}

type specKitFileWriteRequest struct {
	Content string `json:"content"`
}

func (r *Router) putSpecKitFile(w http.ResponseWriter, req *http.Request) {
	path := strings.TrimSpace(req.URL.Query().Get("path"))
	if path == "" {
		httperr.Write(w, http.StatusBadRequest, "validation", "missing Spec Kit file path")
		return
	}
	var body specKitFileWriteRequest
	dec := json.NewDecoder(req.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		httperr.Write(w, http.StatusBadRequest, "validation", "invalid Spec Kit file body: "+err.Error())
		return
	}
	file, err := speckit.NewScanner(r.specKitRoot()).WriteWorkspaceFile(req.Context(), path, body.Content)
	if err != nil {
		httperr.Write(w, http.StatusBadRequest, "validation", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, file)
}

func (r *Router) initializeSpecKitScaffold(w http.ResponseWriter, req *http.Request) {
	workspace, err := speckit.NewScanner(r.specKitRoot()).EnsureScaffold(req.Context())
	if err != nil {
		httperr.Write(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, workspace)
}

type specKitFeatureCreateRequest struct {
	ID    string `json:"id,omitempty"`
	Title string `json:"title"`
}

func (r *Router) createSpecKitFeature(w http.ResponseWriter, req *http.Request) {
	var body specKitFeatureCreateRequest
	dec := json.NewDecoder(req.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		httperr.Write(w, http.StatusBadRequest, "validation", "invalid Spec Kit feature body: "+err.Error())
		return
	}
	if strings.TrimSpace(body.ID) == "" && strings.TrimSpace(body.Title) == "" {
		httperr.Write(w, http.StatusBadRequest, "validation", "Spec Kit feature requires a title or id")
		return
	}
	feature, err := speckit.NewScanner(r.specKitRoot()).CreateFeature(req.Context(), speckit.NewFeatureOptions{
		ID:    body.ID,
		Title: body.Title,
	})
	if err != nil {
		httperr.Write(w, http.StatusBadRequest, "validation", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, feature)
}

func (r *Router) planSpecKitFeatureSync(w http.ResponseWriter, req *http.Request) {
	if r.host == nil || r.host.WorkPlane() == nil {
		httperr.Write(w, http.StatusServiceUnavailable,
			"adaptor_not_configured", "no WorkPlane adaptor registered")
		return
	}
	id := strings.TrimSpace(chi.URLParam(req, "id"))
	if id == "" {
		httperr.Write(w, http.StatusBadRequest, "validation", "missing Spec Kit feature id")
		return
	}
	feature, err := speckit.NewScanner(r.specKitRoot()).Load(req.Context(), id)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			httperr.Write(w, http.StatusNotFound, "not_found", "Spec Kit feature not found")
			return
		}
		httperr.Write(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	plan, err := speckit.PlanFeature(req.Context(), r.host.WorkPlane(), feature)
	if err != nil {
		httperr.WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

func (r *Router) draftSpecKitFeatureSync(w http.ResponseWriter, req *http.Request) {
	if r.host == nil || r.host.WorkPlane() == nil {
		httperr.Write(w, http.StatusServiceUnavailable,
			"adaptor_not_configured", "no WorkPlane adaptor registered")
		return
	}
	id := strings.TrimSpace(chi.URLParam(req, "id"))
	if id == "" {
		httperr.Write(w, http.StatusBadRequest, "validation", "missing Spec Kit feature id")
		return
	}
	feature, err := speckit.NewScanner(r.specKitRoot()).Load(req.Context(), id)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			httperr.Write(w, http.StatusNotFound, "not_found", "Spec Kit feature not found")
			return
		}
		httperr.Write(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	draft, err := speckit.DraftFeature(req.Context(), r.host.WorkPlane(), feature)
	if err != nil {
		httperr.WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, draft)
}

type specKitSyncRequest struct {
	PlanHash     string          `json:"plan_hash"`
	AllowDeletes bool            `json:"allow_deletes"`
	Items        []core.WorkItem `json:"items,omitempty"`
}

func (r *Router) syncSpecKitFeature(w http.ResponseWriter, req *http.Request) {
	if r.rejectBeadsReadOnly(w) {
		return
	}
	if r.host == nil || r.host.WorkPlane() == nil {
		httperr.Write(w, http.StatusServiceUnavailable,
			"adaptor_not_configured", "no WorkPlane adaptor registered")
		return
	}
	id := strings.TrimSpace(chi.URLParam(req, "id"))
	if id == "" {
		httperr.Write(w, http.StatusBadRequest, "validation", "missing Spec Kit feature id")
		return
	}
	var body specKitSyncRequest
	if req.Body == nil {
		httperr.Write(w, http.StatusBadRequest, "validation", "Spec Kit sync requires an approved plan hash")
		return
	}
	dec := json.NewDecoder(req.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		httperr.Write(w, http.StatusBadRequest, "validation", "invalid Spec Kit sync body: "+err.Error())
		return
	}
	if strings.TrimSpace(body.PlanHash) == "" {
		httperr.Write(w, http.StatusBadRequest, "validation", "Spec Kit sync requires an approved plan hash")
		return
	}
	scanner := speckit.NewScanner(r.specKitRoot())
	feature, err := scanner.Load(req.Context(), id)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			httperr.Write(w, http.StatusNotFound, "not_found", "Spec Kit feature not found")
			return
		}
		httperr.Write(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	result, err := speckit.SyncFeatureWithOptions(req.Context(), r.host.WorkPlane(), feature, speckit.SyncOptions{
		ExpectedHash: strings.TrimSpace(body.PlanHash),
		AllowDeletes: body.AllowDeletes,
		Items:        body.Items,
	})
	if err != nil {
		httperr.WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (r *Router) specKitRoot() string {
	if root := strings.TrimSpace(r.cfg.ProjectRoot()); root != "" {
		return root
	}
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}
