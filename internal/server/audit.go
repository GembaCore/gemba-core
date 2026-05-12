// Audit log HTTP surface (gm-o9t8.3.8, Workstream B).
//
// Two endpoints are mounted on the api block:
//
//	GET /api/v1/audit       — paged JSON list filtered by since/type/tenant
//	GET /api/v1/audit/tail  — SSE live stream of new events
//
// Both require a valid bearer (the apiAuth middleware already gates the
// /api block). For v1 every authenticated caller is treated as admin
// (refine later — the decision explicitly tags this as a v1 simplification).
// A non-admin tenant filter still applies if the request supplies one.
//
// The Auditor is lazy-attached via Router.AttachAuditor. When nil, every
// endpoint returns 503 adaptor_not_configured — matching the project's
// convention for unwired surfaces (see AttachWorkflowClient).

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/GembaCore/gemba-core/internal/audit"
	"github.com/GembaCore/gemba-core/internal/server/httperr"
)

// AttachAuditor binds the audit.Auditor the /api/v1/audit and
// /api/v1/audit/tail endpoints dispatch to (gm-o9t8.3.8). Until called,
// every audit handler returns 503 adaptor_not_configured. cmd/gemba
// serve calls this once at boot when --audit-dir resolves to a usable
// path; tests inject a stub.
func (r *Router) AttachAuditor(a audit.Auditor) { r.auditor = a }

// Auditor returns the bound Auditor (nil when AttachAuditor has not
// been called). Producer handlers use this to emit events; see
// emitAudit in work_items_write.go for the create-bead wiring.
func (r *Router) Auditor() audit.Auditor { return r.auditor }

// listAudit handles GET /api/v1/audit?since=<ts>&type=<t>&tenant=<t>.
// Returns {"events": [...]} with up to defaultAuditLimit events.
func (r *Router) listAudit(w http.ResponseWriter, req *http.Request) {
	if r.auditor == nil {
		httperr.Write(w, http.StatusServiceUnavailable,
			"adaptor_not_configured", "no Auditor attached")
		return
	}
	q := req.URL.Query()
	var query audit.Query
	if v := strings.TrimSpace(q.Get("since")); v != "" {
		t, err := parseAuditTimestamp(v)
		if err != nil {
			httperr.Write(w, http.StatusBadRequest, "bad_request",
				"since must be RFC3339 or YYYY-MM-DD")
			return
		}
		query.Since = t
	}
	if v := strings.TrimSpace(q.Get("until")); v != "" {
		t, err := parseAuditTimestamp(v)
		if err != nil {
			httperr.Write(w, http.StatusBadRequest, "bad_request",
				"until must be RFC3339 or YYYY-MM-DD")
			return
		}
		query.Until = t
	}
	query.Type = strings.TrimSpace(q.Get("type"))
	query.Tenant = strings.TrimSpace(q.Get("tenant"))
	query.Workspace = strings.TrimSpace(q.Get("wsid"))
	if v := strings.TrimSpace(q.Get("limit")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			httperr.Write(w, http.StatusBadRequest, "bad_request",
				"limit must be a non-negative integer")
			return
		}
		query.Limit = n
	} else {
		query.Limit = defaultAuditLimit
	}
	events, err := r.auditor.Query(req.Context(), query)
	if err != nil {
		httperr.Write(w, http.StatusInternalServerError, "audit_query_failed", err.Error())
		return
	}
	if events == nil {
		events = []audit.Event{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"events": events,
		"total":  len(events),
	})
}

// tailAudit handles GET /api/v1/audit/tail. Streams every event as it
// is appended, encoded as Server-Sent Events.
func (r *Router) tailAudit(w http.ResponseWriter, req *http.Request) {
	if r.auditor == nil {
		httperr.Write(w, http.StatusServiceUnavailable,
			"adaptor_not_configured", "no Auditor attached")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		httperr.Write(w, http.StatusInternalServerError, "streaming_unsupported",
			"response writer does not support flushing")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ctx := req.Context()
	ch, err := r.auditor.Tail(ctx)
	if err != nil {
		fmt.Fprintf(w, "event: error\ndata: %s\n\n", err.Error())
		flusher.Flush()
		return
	}
	tenant := strings.TrimSpace(req.URL.Query().Get("tenant"))
	for {
		select {
		case <-ctx.Done():
			return
		case e, ok := <-ch:
			if !ok {
				return
			}
			if tenant != "" && e.Tenant != tenant {
				continue
			}
			line, mErr := encodeSSEEvent(e)
			if mErr != nil {
				continue
			}
			if _, err := w.Write(line); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// parseAuditTimestamp accepts RFC3339 (with or without nanos / TZ) or
// the YYYY-MM-DD shorthand and returns a UTC time.
func parseAuditTimestamp(v string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse("2006-01-02", v); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("unparseable timestamp: %q", v)
}

// encodeSSEEvent renders one Event as an SSE data frame.
func encodeSSEEvent(e audit.Event) ([]byte, error) {
	body, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	return []byte("event: audit\ndata: " + string(body) + "\n\n"), nil
}

// defaultAuditLimit is the cap applied when the caller doesn't pass
// ?limit=. Matches the work-items default.
const defaultAuditLimit = 1000
