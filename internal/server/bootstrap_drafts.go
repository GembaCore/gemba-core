package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/server/httperr"
)

type bootstrapDraftApplyRequest struct {
	TargetDatabase string          `json:"target_database,omitempty"`
	Items          []core.WorkItem `json:"items"`
}

type bootstrapDraftApplyResponse struct {
	TargetDatabase string   `json:"target_database"`
	Created        []string `json:"created"`
	Count          int      `json:"count"`
}

func (r *Router) applyBootstrapDraft(w http.ResponseWriter, req *http.Request) {
	if r.rejectBeadsReadOnly(w) {
		return
	}
	if r.host == nil || r.host.WorkPlane() == nil {
		httperr.Write(w, http.StatusServiceUnavailable,
			"adaptor_not_configured", "no WorkPlane adaptor registered")
		return
	}
	var body bootstrapDraftApplyRequest
	dec := json.NewDecoder(req.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		httperr.Write(w, http.StatusBadRequest, "validation", "invalid bootstrap draft body: "+err.Error())
		return
	}
	target := strings.TrimSpace(body.TargetDatabase)
	if target == "" {
		target = "active"
	}
	if target != "active" {
		httperr.Write(w, http.StatusBadRequest, "validation", "only the active Beads database target is available")
		return
	}
	if len(body.Items) == 0 {
		httperr.Write(w, http.StatusBadRequest, "validation", "bootstrap draft requires at least one item")
		return
	}
	created, err := createDraftItems(req.Context(), r.host.WorkPlane(), body.Items)
	if err != nil {
		httperr.WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, bootstrapDraftApplyResponse{
		TargetDatabase: target,
		Created:        created,
		Count:          len(created),
	})
}

func createDraftItems(ctx context.Context, wp core.WorkPlane, items []core.WorkItem) ([]string, error) {
	pending := map[core.WorkItemID]core.WorkItem{}
	for _, item := range items {
		item.ID = core.WorkItemID(strings.TrimSpace(string(item.ID)))
		item.Title = strings.TrimSpace(item.Title)
		if item.ID == "" {
			return nil, core.NewAdaptorError(core.KindValidation, "bootstrap draft item id is required")
		}
		if item.Title == "" {
			return nil, core.NewAdaptorError(core.KindValidation, "bootstrap draft item %s title is required", item.ID)
		}
		pending[item.ID] = item
	}
	createdByDraftID := map[core.WorkItemID]core.WorkItemID{}
	var created []string
	for len(pending) > 0 {
		progress := false
		for draftID, item := range pending {
			parent := parentForDraftApply(item.Relationships)
			if parent != "" {
				mapped := createdByDraftID[parent]
				if mapped == "" {
					if _, parentPending := pending[parent]; parentPending {
						continue
					}
				} else {
					item.Relationships = []core.Relationship{{
						Kind: core.RelParentChild,
						From: mapped,
					}}
				}
			}
			out, err := wp.CreateWorkItem(ctx, item)
			if err != nil {
				return nil, err
			}
			createdByDraftID[draftID] = out.ID
			created = append(created, string(out.ID))
			delete(pending, draftID)
			progress = true
		}
		if !progress {
			return nil, core.NewAdaptorError(core.KindValidation, "bootstrap draft parent relationships contain a cycle or missing root")
		}
	}
	return created, nil
}

func parentForDraftApply(rels []core.Relationship) core.WorkItemID {
	for _, rel := range rels {
		if rel.Kind == core.RelParentChild && rel.From != "" {
			return rel.From
		}
	}
	return ""
}
