// work_items.go: typed-client convenience methods for the WorkItem
// CRUD surface used by `gemba bead {create,list,show,update,close,note}`
// (gm-o9t8.1.3.2).
//
// The generated client at gen/client.gen.go exposes only the read +
// dispatch endpoints (List, Get, Delete, CascadeDispatch) because the
// OpenAPI document hadn't yet pinned the mutation surface when the
// typed client was first generated (gm-o9t8.1.1.4). Rather than gate
// CRUD on a fresh codegen pass, this file rides the same auth /
// confirm / user-agent pipeline by issuing manual JSON requests
// through c.httpDoer with the three RequestEditor functions applied.
//
// When the OpenAPI spec is refreshed and the typed client is
// regenerated against it, these helpers SHOULD be deleted in favor of
// the generated CreateWorkItem / UpdateWorkItem / etc. methods.

package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/GembaCore/gemba-core/core"
)

// CreateWorkItemInput is the boundary shape the server accepts on
// POST /api/work-items. Mirrors transport.CreateWorkItemRequest with a
// slim, hand-curated subset of WorkItem fields that operator-facing
// CLI users actually set on create.
type CreateWorkItemInput struct {
	Title         string   `json:"title"`
	Kind          string   `json:"kind"`
	Status        string   `json:"status"`
	StateCategory string   `json:"state_category"`
	Description   string   `json:"description,omitempty"`
	Labels        []string `json:"labels,omitempty"`
	Priority      int      `json:"priority,omitempty"`
	ParentID      string   `json:"parent_id,omitempty"`
}

// createBody is the wire envelope the server expects on POST.
type createBody struct {
	Item map[string]any `json:"item"`
}

// CreateWorkItem POSTs a new work item and returns the materialized
// core.WorkItem. Body is wrapped in `{"item": {...}}` per
// transport.DecodeCreateWorkItem.
func (c *Client) CreateWorkItem(ctx context.Context, in CreateWorkItemInput) (*core.WorkItem, error) {
	if strings.TrimSpace(in.Title) == "" {
		return nil, fmt.Errorf("create work item: title is required")
	}
	if strings.TrimSpace(in.Kind) == "" {
		in.Kind = "task"
	}
	if strings.TrimSpace(in.Status) == "" {
		in.Status = "todo"
	}
	if strings.TrimSpace(in.StateCategory) == "" {
		in.StateCategory = "unstarted"
	}
	item := map[string]any{
		"title":          in.Title,
		"kind":           in.Kind,
		"status":         in.Status,
		"state_category": in.StateCategory,
	}
	if in.Description != "" {
		item["description"] = in.Description
	}
	if len(in.Labels) > 0 {
		item["labels"] = in.Labels
	}
	if in.Priority != 0 {
		item["priority"] = in.Priority
	}
	if in.ParentID != "" {
		item["parent_id"] = in.ParentID
	}
	body, err := json.Marshal(createBody{Item: item})
	if err != nil {
		return nil, fmt.Errorf("create work item: marshal: %w", err)
	}
	resp, raw, err := c.doJSON(ctx, http.MethodPost, "/api/work-items", body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, mapErr(resp, raw)
	}
	var out core.WorkItem
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("create work item: decode response: %w", err)
	}
	return &out, nil
}

// ListWorkItemsFilter is the typed filter struct for ListWorkItems.
// Fields default to "no filter" when zero.
type ListWorkItemsFilter struct {
	Status string
	Limit  int
}

// ListWorkItems is a typed wrapper over GET /api/work-items.
func (c *Client) ListWorkItems(ctx context.Context, f ListWorkItemsFilter) ([]core.WorkItem, error) {
	q := url.Values{}
	if f.Status != "" {
		q.Set("status", f.Status)
	}
	if f.Limit > 0 {
		q.Set("limit", strconv.Itoa(f.Limit))
	}
	path := "/api/work-items"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	resp, raw, err := c.doJSON(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, mapErr(resp, raw)
	}
	// Server returns either an envelope `{"items": [...]}` or a raw
	// array depending on the adaptor. Accept both.
	var env struct {
		Items []core.WorkItem `json:"items"`
	}
	if err := json.Unmarshal(raw, &env); err == nil && env.Items != nil {
		return env.Items, nil
	}
	var arr []core.WorkItem
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil, fmt.Errorf("list work items: decode response: %w", err)
	}
	return arr, nil
}

// GetWorkItem fetches a single work item by id.
func (c *Client) GetWorkItem(ctx context.Context, id string) (*core.WorkItem, error) {
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("get work item: id is required")
	}
	path := "/api/work-items/" + url.PathEscape(id)
	resp, raw, err := c.doJSON(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, mapErr(resp, raw)
	}
	var out core.WorkItem
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("get work item: decode response: %w", err)
	}
	return &out, nil
}

// PatchWorkItem PATCHes a work item with the supplied core.WorkItemPatch.
func (c *Client) PatchWorkItem(ctx context.Context, id string, patch core.WorkItemPatch) (*core.WorkItem, error) {
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("patch work item: id is required")
	}
	body, err := json.Marshal(patch)
	if err != nil {
		return nil, fmt.Errorf("patch work item: marshal: %w", err)
	}
	path := "/api/work-items/" + url.PathEscape(id)
	resp, raw, err := c.doJSON(ctx, http.MethodPatch, path, body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, mapErr(resp, raw)
	}
	var out core.WorkItem
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("patch work item: decode response: %w", err)
	}
	return &out, nil
}

// CascadeDispatchResponse mirrors the subset of the server's
// cascadeDispatchResponse envelope that the CLI consumes
// (gm-o9t8.1.5.5). Decoupled from the server-side type so the wire
// client stays independent of internal/server.
type CascadeDispatchResponse struct {
	WrapperID  string                  `json:"wrapper_id"`
	Dispatched []CascadeDispatchedItem `json:"dispatched"`
	Blocked    []string                `json:"blocked,omitempty"`
	Skipped    []string                `json:"skipped,omitempty"`
	Errors     []CascadeDispatchError  `json:"errors,omitempty"`
}

// CascadeDispatchedItem is one entry in the Dispatched slice.
type CascadeDispatchedItem struct {
	WorkItemID string `json:"work_item_id"`
	SessionID  string `json:"session_id,omitempty"`
}

// CascadeDispatchError is one entry in the Errors slice.
type CascadeDispatchError struct {
	WorkItemID string `json:"work_item_id"`
	Message    string `json:"message"`
}

// CascadeDispatchInput is the optional body POSTed to
// /api/work-items/{id}/cascade-dispatch. Fields default to "no
// override" when zero.
type CascadeDispatchInput struct {
	AgentType string `json:"agent_type,omitempty"`
	Limit     int    `json:"limit,omitempty"`
}

// CascadeDispatchWorkItem POSTs to /api/work-items/{id}/cascade-dispatch
// and decodes the wrapper response. The wrapper_id is the cascade run
// id (used by `gemba logs -f <run_id>`). X-GEMBA-Confirm is
// auto-injected by the wrapper's request editors.
func (c *Client) CascadeDispatchWorkItem(ctx context.Context, id string, in CascadeDispatchInput) (*CascadeDispatchResponse, error) {
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("cascade dispatch: id is required")
	}
	body := map[string]any{}
	if in.AgentType != "" {
		body["agent_type"] = in.AgentType
	}
	if in.Limit > 0 {
		body["limit"] = in.Limit
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("cascade dispatch: marshal: %w", err)
	}
	path := "/api/work-items/" + url.PathEscape(id) + "/cascade-dispatch"
	resp, raw, err := c.doJSON(ctx, http.MethodPost, path, buf)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, mapErr(resp, raw)
	}
	var out CascadeDispatchResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("cascade dispatch: decode response: %w", err)
	}
	return &out, nil
}

// NoteWorkItem appends a free-form operator note to a work item.
//
// Backed by POST /api/work-items/{id}/notes which is currently a 501
// stub (added in gm-o9t8.1.3.2 alongside this client). The CLI wires
// the verb so the surface is discoverable; the server-side semantics
// land in a follow-up bead.
func (c *Client) NoteWorkItem(ctx context.Context, id, note string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("note: id is required")
	}
	if strings.TrimSpace(note) == "" {
		return fmt.Errorf("note: text is required")
	}
	body, err := json.Marshal(map[string]string{"note": note})
	if err != nil {
		return fmt.Errorf("note: marshal: %w", err)
	}
	path := "/api/work-items/" + url.PathEscape(id) + "/notes"
	resp, raw, err := c.doJSON(ctx, http.MethodPost, path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return mapErr(resp, raw)
	}
	return nil
}

// doJSON issues a JSON request through c.httpDoer applying the same
// editors the generated client uses. Returns the raw response (without
// the body still attached) and the read-and-buffered body bytes so
// callers can decode + branch on status.
func (c *Client) doJSON(ctx context.Context, method, path string, body []byte) (*http.Response, []byte, error) {
	full := strings.TrimRight(c.server, "/") + path
	var reqBody io.Reader
	if body != nil {
		reqBody = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, full, reqBody)
	if err != nil {
		return nil, nil, fmt.Errorf("build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if err := c.addAuth(ctx, req); err != nil {
		return nil, nil, err
	}
	if err := c.addUserAgent(ctx, req); err != nil {
		return nil, nil, err
	}
	if err := c.addConfirm(ctx, req); err != nil {
		return nil, nil, err
	}
	resp, err := c.httpDoer.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp, nil, fmt.Errorf("read body: %w", err)
	}
	return resp, raw, nil
}
