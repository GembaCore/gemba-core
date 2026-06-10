package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/server/httperr"
)

// notifyRequest is the body shape of POST /api/workitems/notify
// (gm-e4.3.2). The endpoint exists so out-of-process bd writers
// (terminal `bd update`, the post-commit git hook from gm-e4.3.3)
// can flush a workitem.* GembaEvent into the in-process events.Hub
// — closing the half of gm-e4.3.1 that the in-process emit can't
// reach.
type notifyRequest struct {
	// WorkItemID is the canonical id of the bead that was mutated.
	// Required. The handler does NOT trust any other caller-supplied
	// state — Gemba re-reads the WorkItem from the bound WorkPlane.
	WorkItemID string `json:"work_item_id"`

	// Kind is an advisory hint reserved for forward compat. Today the
	// server always derives the kind from the persisted StateCategory
	// (mirroring UpdateWorkItem's terminal-state detection); this
	// field is accepted but ignored.
	Kind string `json:"kind,omitempty"`

	// Source is an audit hint ("bd-git-hook", "ops-runbook"). Echoed
	// onto the emitted event's payload as `notify_source` so SSE
	// subscribers can route on origin.
	Source string `json:"source,omitempty"`
}

// notifyResponse is the body shape returned on a successful publish.
// Echoes back the canonical id and the kind the server emitted so a
// caller (e.g. a git hook) can log what landed. When the post is a
// duplicate of the most-recent notify for the same id (same
// UpdatedAt), Skipped is true and Kind is empty.
type notifyResponse struct {
	WorkItemID string `json:"work_item_id"`
	Kind       string `json:"kind,omitempty"`
	Skipped    bool   `json:"skipped,omitempty"`
}

// notifyDeduper is the per-Router idempotency cache (gm-e4.3.2 DoD:
// "same notify twice should not double-publish"). Keyed by
// WorkItemID with the last published UpdatedAt as the value; a re-
// post arriving with an unchanged UpdatedAt is suppressed.
//
// Bounded by entry count; oldest entry evicted FIFO on insert when
// full. A re-fire of an evicted id simply re-emits — that's a false
// negative on the dedup, never a false positive (an event for state
// the operator has already seen).
type notifyDeduper struct {
	mu    sync.Mutex
	last  map[string]string // work_item_id → last-published UpdatedAt RFC3339Nano
	order []string
	cap   int
}

const defaultNotifyDedupCap = 1024

func newNotifyDeduper(cap int) *notifyDeduper {
	if cap <= 0 {
		cap = defaultNotifyDedupCap
	}
	return &notifyDeduper{
		last:  make(map[string]string, cap),
		order: make([]string, 0, cap),
		cap:   cap,
	}
}

// shouldEmit reports whether (id, updatedAt) has changed since the
// last published notify for id, recording the pair for next time.
// Returns true on first sighting AND on a state change; false when
// the (id, updatedAt) pair is identical to the last record.
func (d *notifyDeduper) shouldEmit(id, updatedAt string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if prev, ok := d.last[id]; ok {
		if prev == updatedAt {
			return false
		}
		d.last[id] = updatedAt
		return true
	}
	if len(d.order) >= d.cap {
		evict := d.order[0]
		d.order = d.order[1:]
		delete(d.last, evict)
	}
	d.order = append(d.order, id)
	d.last[id] = updatedAt
	return true
}

// notifyWorkItem handles POST /api/workitems/notify. Dumb trigger:
// re-read through the bound WorkPlane, publish a WorkPlaneEvent
// through the same emitter the in-process mutation path uses. The
// /events SSE hub picks it up automatically via the existing
// AttachWorkPlaneStream pump.
//
// Auth: mounted under /api/* so the existing api.Use(apiAuth) covers
// this endpoint when --auth=token is on. No special MCP/agent path —
// this is server-internal plumbing.
func (r *Router) notifyWorkItem(w http.ResponseWriter, req *http.Request) {
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

	var body notifyRequest
	dec := json.NewDecoder(req.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		httperr.Write(w, http.StatusBadRequest, "bad_request",
			"malformed JSON: "+err.Error())
		return
	}
	if strings.TrimSpace(body.WorkItemID) == "" {
		httperr.Write(w, http.StatusBadRequest, "bad_request",
			"work_item_id is required")
		return
	}

	notifier, ok := wp.(core.WorkItemNotifier)
	if !ok {
		// Dolt read-only adaptor, noop adaptor, etc. — no mutation
		// path means nothing to notify about. Mirror the Subscribe()
		// KindUnsupported convention from gm-e4.3.1.
		httperr.Write(w, http.StatusConflict, "capability_denied",
			"bound WorkPlane adaptor does not support notify (read-only or non-emitting)")
		return
	}

	id := core.WorkItemID(body.WorkItemID)

	// Re-read through the WorkPlane to get the canonical state.
	// Idempotency check happens against the persisted UpdatedAt — no
	// trust of caller state. This costs one extra `bd show` on a
	// duplicate post, but the path is rare; the consistency win is
	// worth the cost.
	wi, err := wp.GetWorkItem(req.Context(), id)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) && core.AsAdaptorError(err) == nil {
			err = core.NewAdaptorError(core.KindSessionNotFound,
				"work item %s not found", body.WorkItemID)
		}
		httperr.WriteError(w, err)
		return
	}
	updatedAt := wi.UpdatedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00")
	if !r.notifyDeduper.shouldEmit(body.WorkItemID, updatedAt) {
		writeJSON(w, http.StatusOK, notifyResponse{
			WorkItemID: body.WorkItemID,
			Skipped:    true,
		})
		return
	}

	// NotifyExternal re-reads AND publishes — the re-read above is
	// for dedup only. The double-read is intentional: the dedup
	// check needs the canonical UpdatedAt, and NotifyExternal owns
	// the full read+publish path so the in-process emit semantics
	// stay unified.
	wi, kind, err := notifier.NotifyExternal(req.Context(), id, body.Source)
	if err != nil {
		// Rare — we just successfully read the same id seconds ago.
		// A failure here usually means an adaptor-degraded scenario.
		httperr.WriteError(w, err)
		return
	}
	r.reconcileWrapperAncestors(req.Context(), wp, wi)
	r.continueActiveCascades(req.Context(), wp, wi)

	writeJSON(w, http.StatusOK, notifyResponse{
		WorkItemID: body.WorkItemID,
		Kind:       kind,
	})
}
