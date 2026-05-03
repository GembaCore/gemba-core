// Mutation handlers for /api/work-items (gm-root.8 slice 1, renamed
// in gm-root.9).
//
// Mutating routes: POST /api/work-items, PATCH /api/work-items/{id},
// and DELETE /api/work-items/{id}. Closing remains a PATCH to
// state_category=completed; DELETE is a destructive Beads-management
// operation for modes that expose raw bead administration.

package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/server/httperr"
	"github.com/GembaCore/gemba-core/internal/transport/api"
)

type workItemDeleter interface {
	DeleteWorkItem(ctx context.Context, id core.WorkItemID) (core.WorkItem, error)
}

// createWorkItem is POST /api/work-items. Body MUST be the boundary
// shape accepted by transport.DecodeCreateWorkItem: `{"item": {...}}`
// with title, kind, status, state_category all non-empty and id +
// timestamps unset. Server-side fields land on the response.
//
// Response: 201 Created + the materialized core.WorkItem. SPA drops
// it straight into the react-query cache.
//
// Adaptor errors flow through the shared httperr mapper with the same
// kind→status mapping patchWorkItem uses.
func (r *Router) createWorkItem(w http.ResponseWriter, req *http.Request) {
	if r.host == nil {
		httperr.Write(w, http.StatusServiceUnavailable,
			"adaptor_not_configured", "no WorkPlane adaptor registered")
		return
	}
	wp := r.host.WorkPlane()
	if wp == nil {
		httperr.Write(w, http.StatusServiceUnavailable,
			"adaptor_not_configured", "no WorkPlane adaptor registered")
		return
	}

	wi, err := api.DecodeCreateWorkItem(req)
	if err != nil {
		httperr.WriteError(w, err)
		return
	}

	// gm-lw6h: auto-prefix milestone titles with `M<n>` (per-project
	// monotonic numbering) unless the operator already supplied one.
	if wi.Kind == core.KindMilestone {
		if !milestonePrefixRe.MatchString(strings.TrimSpace(wi.Title)) {
			n := nextMilestoneNumber(existingMilestoneTitles(req.Context(), wp))
			wi.Title = applyMilestonePrefix(wi.Title, n)
		}
	}

	out, err := wp.CreateWorkItem(req.Context(), wi)
	if err != nil {
		httperr.WriteError(w, err)
		return
	}
	if err := r.appendBeadsHistory(beadsHistoryEvent{
		Action:  createAction(out.Kind),
		Entity:  workItemEntity(out),
		After:   map[string]any{"state_category": out.StateCategory, "status": out.Status, "labels": out.Labels},
		Summary: createSummary(out),
	}); err != nil {
		httperr.Write(w, http.StatusInternalServerError, "manifest_write_failed", err.Error())
		return
	}
	if !r.cfg.BeadsOnly {
		// A session can file follow-up beads beneath an active epic or
		// milestone. Treat that as new work entering the active cascade:
		// wrapper rollups should update immediately, and propulsion should
		// stage/dispatch the child if dependencies allow.
		r.continueActiveCascades(req.Context(), wp, out)
		r.reconcileWrapperAncestors(req.Context(), wp, out)
	}
	if refreshed, err := wp.GetWorkItem(req.Context(), out.ID); err == nil {
		out = refreshed
	}
	writeJSON(w, http.StatusCreated, out)
}

// patchWorkItem is PATCH /api/work-items/{id}. Body MUST be a
// core.WorkItemPatch JSON object; an empty object is a no-op (the
// underlying adaptor still returns the current item, useful for
// round-tripping a state probe).
//
// Response shape: 200 + the materialized core.WorkItem (so the SPA
// can drop the result straight into its react-query cache).
//
// Adaptor errors flow through the shared httperr mapper:
//
//	core.KindReadOnly        → 405 method_not_allowed   (dolt-url adaptor)
//	core.KindValidation      → 400 validation
//	core.KindAdaptorDegraded → 503 adaptor_degraded
//	core.KindSessionNotFound → 404 session_not_found
//	(untagged)               → 500 internal             (Conformance Group F violation)
func (r *Router) patchWorkItem(w http.ResponseWriter, req *http.Request) {
	raw := chi.URLParam(req, "id")
	if raw == "" {
		httperr.Write(w, http.StatusBadRequest, "bad_request", "missing work item id")
		return
	}
	id, err := url.PathUnescape(raw)
	if err != nil {
		httperr.Write(w, http.StatusBadRequest, "bad_request",
			"malformed work item id: "+err.Error())
		return
	}

	if r.host == nil {
		httperr.Write(w, http.StatusServiceUnavailable,
			"adaptor_not_configured", "no WorkPlane adaptor registered")
		return
	}
	wp := r.host.WorkPlane()
	if wp == nil {
		httperr.Write(w, http.StatusServiceUnavailable,
			"adaptor_not_configured", "no WorkPlane adaptor registered")
		return
	}

	var patch core.WorkItemPatch
	if err := decodePatchBody(req, &patch); err != nil {
		httperr.Write(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}

	var before core.WorkItem
	haveBefore := false
	if r.cfg.BeadsOnly {
		if current, getErr := wp.GetWorkItem(req.Context(), core.WorkItemID(id)); getErr == nil {
			before = current
			haveBefore = true
		}
	}

	out, err := wp.UpdateWorkItem(req.Context(), core.WorkItemID(id), patch)
	if err != nil {
		// Tag bare ErrNotFound the same way the GET handler does so the
		// envelope is self-describing.
		if errors.Is(err, core.ErrNotFound) && core.AsAdaptorError(err) == nil {
			err = core.NewAdaptorError(core.KindSessionNotFound, "work item %s not found", id)
		}
		httperr.WriteError(w, err)
		return
	}
	if r.cfg.BeadsOnly && haveBefore {
		beforeMap, afterMap := patchBeforeAfter(before, out, patch)
		action := "work_item.edited"
		if patch.StateCategory != nil && before.StateCategory != out.StateCategory {
			action = "work_item.state_changed"
		}
		if err := r.appendBeadsHistory(beadsHistoryEvent{
			Action:  action,
			Entity:  workItemEntity(out),
			Before:  beforeMap,
			After:   afterMap,
			Summary: patchSummary(before, out, patch),
		}); err != nil {
			httperr.Write(w, http.StatusInternalServerError, "manifest_write_failed", err.Error())
			return
		}
	}
	// gm-1o9n: wrapper beads (epics/milestones) derive their board
	// state from descendant runnable leaves. Best-effort — see
	// milestone_autoclose.go.
	if !r.cfg.BeadsOnly {
		r.reconcileWrapperAncestors(req.Context(), wp, out)
		r.continueActiveCascades(req.Context(), wp, out)
	}
	writeJSON(w, http.StatusOK, out)
}

// deleteWorkItem is DELETE /api/work-items/{id}. It is intentionally
// modeled as an optional WorkPlane extension: Beads can hard-delete
// records through the public `bd delete` CLI, while read-only or
// execution-oriented adaptors can reject the operation without growing
// the core WorkPlane contract prematurely.
func (r *Router) deleteWorkItem(w http.ResponseWriter, req *http.Request) {
	raw := chi.URLParam(req, "id")
	if raw == "" {
		httperr.Write(w, http.StatusBadRequest, "bad_request", "missing work item id")
		return
	}
	id, err := url.PathUnescape(raw)
	if err != nil {
		httperr.Write(w, http.StatusBadRequest, "bad_request",
			"malformed work item id: "+err.Error())
		return
	}

	if r.host == nil {
		httperr.Write(w, http.StatusServiceUnavailable,
			"adaptor_not_configured", "no WorkPlane adaptor registered")
		return
	}
	wp := r.host.WorkPlane()
	if wp == nil {
		httperr.Write(w, http.StatusServiceUnavailable,
			"adaptor_not_configured", "no WorkPlane adaptor registered")
		return
	}
	deleter, ok := wp.(workItemDeleter)
	if !ok {
		httperr.WriteError(w, core.NewAdaptorError(core.KindUnsupported,
			"work item delete is not supported by this adaptor"))
		return
	}
	deleted, err := deleter.DeleteWorkItem(req.Context(), core.WorkItemID(id))
	if err != nil {
		if errors.Is(err, core.ErrNotFound) && core.AsAdaptorError(err) == nil {
			err = core.NewAdaptorError(core.KindSessionNotFound, "work item %s not found", id)
		}
		httperr.WriteError(w, err)
		return
	}
	if r.cfg.BeadsOnly {
		if err := r.appendBeadsHistory(beadsHistoryEvent{
			Action: "work_item.deleted",
			Entity: workItemEntity(deleted),
			Before: map[string]any{
				"state_category": deleted.StateCategory,
				"status":         deleted.Status,
				"labels":         deleted.Labels,
			},
			Summary: "Deleted \"" + deleted.Title + "\".",
		}); err != nil {
			httperr.Write(w, http.StatusInternalServerError, "manifest_write_failed", err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, deleted)
}

// decodePatchBody reads the JSON body into patch. An empty body is
// treated as "no fields to update" rather than an error — pairs nicely
// with the nonce idempotency contract (a retry with no body is a probe
// that returns the current item).
func decodePatchBody(req *http.Request, patch *core.WorkItemPatch) error {
	if req.Body == nil || req.ContentLength == 0 {
		return nil
	}
	dec := json.NewDecoder(req.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(patch); err != nil {
		return err
	}
	return nil
}
