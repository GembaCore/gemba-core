package server

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/MikeBengtson/gemba/internal/core"
	"github.com/MikeBengtson/gemba/internal/server/httperr"
	"github.com/go-chi/chi/v5"
)

// listWorkItems is the GET /api/work-items handler. It calls the
// registered WorkPlane's ListWorkItems with a zero-valued filter (no
// narrowing — filtering and pagination are deferred to later
// milestones) and returns the envelope `{items: []WorkItem, total: N}`.
// The SPA's useWorkItems() hook drives off this shape.
//
// Error envelopes follow the shared httperr contract (gm-root.1.1):
//
//	503 {"error": "adaptor_not_configured", ...} — no host/WorkPlane wired
//	503 {"error": "adaptor_degraded", ...}       — tagged AdaptorError
//	500 {"error": "internal", ...}               — untagged adaptor error
//
// An empty-but-healthy adaptor surfaces as 200 with `{items: [], total: 0}`;
// the handler normalises nil slices so the wire shape is a JSON array, not null.
func (r *Router) listWorkItems(w http.ResponseWriter, req *http.Request) {
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

	filter, err := parseWorkItemFilter(req.URL.Query())
	if err != nil {
		httperr.Write(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}

	items, err := wp.ListWorkItems(req.Context(), filter)
	if err != nil {
		httperr.WriteError(w, err)
		return
	}
	if items == nil {
		items = []core.WorkItem{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"total": len(items),
	})
}

// parseWorkItemFilter turns the request query string into a
// core.WorkItemFilter (gm-e12.9.1). Multi-value fields accept either
// repeated params (?state_category=backlog&state_category=unstarted)
// or comma-separated lists (?state_category=backlog,unstarted) — pick
// whichever reads better at the call site.
//
// Invalid state_category / kind values return an error so typos
// surface as 400 rather than silently returning an empty list.
func parseWorkItemFilter(q url.Values) (core.WorkItemFilter, error) {
	var f core.WorkItemFilter
	f.Kinds = csvList(q["kind"])
	f.Statuses = csvList(q["status"])
	f.Labels = csvList(q["label"])
	f.IDs = toWorkItemIDs(csvList(q["id"]))

	for _, raw := range csvList(q["state_category"]) {
		sc := core.StateCategory(raw)
		if !sc.Valid() {
			return core.WorkItemFilter{}, &badRequestError{
				msg: "unknown state_category: " + raw,
			}
		}
		f.StateCategory = append(f.StateCategory, sc)
	}

	if v := strings.TrimSpace(q.Get("assignee_id")); v != "" {
		id := core.AgentID(v)
		f.AssigneeID = &id
	}
	if v := strings.TrimSpace(q.Get("sprint_id")); v != "" {
		s := v
		f.SprintID = &s
	}
	if v := strings.TrimSpace(q.Get("limit")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return core.WorkItemFilter{}, &badRequestError{
				msg: "limit must be a non-negative integer",
			}
		}
		f.Limit = n
	} else {
		// Default to a high cap when the caller doesn't specify (gm-nr67).
		// The bd CLI's own default is 50, which silently hides newer /
		// lower-priority items from the SPA's Board + Grid (which call
		// without a limit). 1000 covers every realistic workspace today
		// without unbounded blast radius. Proper pagination ships in a
		// later milestone; this is the interim cap.
		f.Limit = defaultListLimit
	}
	return f, nil
}

// defaultListLimit is the cap parseWorkItemFilter applies when the
// caller didn't specify ?limit=. Picked to be larger than any realistic
// workspace a single-tenant gemba instance manages today (cf. gm-e12.3
// virtualised grid is 10k rows; we sit one order of magnitude under
// that so a missing limit can't tank the server).
const defaultListLimit = 1000

// csvList flattens a slice of query values where each element may
// itself be a comma-separated list. Empty entries are dropped.
func csvList(in []string) []string {
	var out []string
	for _, raw := range in {
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

func toWorkItemIDs(in []string) []core.WorkItemID {
	if len(in) == 0 {
		return nil
	}
	out := make([]core.WorkItemID, 0, len(in))
	for _, s := range in {
		out = append(out, core.WorkItemID(s))
	}
	return out
}

// badRequestError is a typed wrapper so parseWorkItemFilter returns a
// single error shape the handler maps to 400.
type badRequestError struct{ msg string }

func (e *badRequestError) Error() string { return e.msg }

// getWorkItem is the GET /api/work-items/{id} handler. Returns a single
// WorkItem with its full Relationship graph (blocks / parent_child /
// relates_to) plus any extension edges the adaptor surfaces on
// Custom["beads:dependencies"] (or whatever the active adaptor names
// its native-edge custom key). Drives the SPA drill-in drawer.
//
// Error envelopes go through the shared httperr package so every data
// handler emits the same wire shape (gm-root.1.1):
//
//	400 {"error": "bad_request", "message": "missing work item id"}
//	404 {"error": "session_not_found", "message": "work item gm-foo not found"}
//	503 {"error": "adaptor_not_configured" | "adaptor_degraded", ...}
//	500 {"error": "internal", "message": "..."}
//
// The handler itself does not walk the relationship graph — the bound
// WorkPlane adaptor is responsible for populating Relationships,
// Evidence, and Custom fields before returning.
func (r *Router) getWorkItem(w http.ResponseWriter, req *http.Request) {
	raw := chi.URLParam(req, "id")
	if raw == "" {
		httperr.Write(w, http.StatusBadRequest, "bad_request", "missing work item id")
		return
	}
	// chi returns the path param verbatim; adaptors that prefix ids with
	// a workspace segment (the dolt adaptor's "gemba/gemba/" prefix) mean
	// the SPA sends %2F-encoded slashes. Decode here so the adaptor
	// receives the canonical id and not the still-encoded wire form.
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

	item, err := wp.GetWorkItem(req.Context(), core.WorkItemID(id))
	if err != nil {
		// Tag the bare sentinel with the work item id so the 404 envelope
		// carries a self-describing message. Adaptors that already
		// return a *core.AdaptorError flow through to httperr.WriteError
		// unchanged.
		if errors.Is(err, core.ErrNotFound) && core.AsAdaptorError(err) == nil {
			err = core.NewAdaptorError(core.KindSessionNotFound, "work item %s not found", id)
		}
		httperr.WriteError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, item)
}
