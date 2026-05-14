// status.go: typed-client methods for the `gemba status` dashboard
// (gm-o9t8.1.17).
//
// These wrap two endpoints the CLI hits on every status read:
//
//   - GET /api/v1/workspaces/{wsid}/status — primary path; returns the
//     unified workspace envelope (mode + bead counts + repo + agents).
//     Sentinels: ErrUnauthorized, ErrNotFound, ErrNotImplemented.
//   - GET /api/work-items?status=<status> — legacy fallback the dashboard
//     uses when the primary endpoint returns 501.
//
// Both methods ride the same auth / user-agent / confirm pipeline the
// rest of the client uses, so a 401 from an auth-enabled server surfaces
// as ErrUnauthorized instead of a silent miss.

package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/GembaCore/gemba-core/core"
)

// WorkspaceRepoStatus mirrors the server's repo stripe.
type WorkspaceRepoStatus struct {
	Head   string `json:"head"`
	Dirty  bool   `json:"dirty"`
	Branch string `json:"branch"`
}

// WorkspaceBeadCounts mirrors the bead counts the dashboard renders.
type WorkspaceBeadCounts struct {
	Ready       int `json:"ready"`
	InFlight    int `json:"in_flight"`
	Blocked     int `json:"blocked"`
	OpenTotal   int `json:"open_total"`
	ClosedToday int `json:"closed_today"`
}

// WorkspaceAgentStatus mirrors the agents stripe.
type WorkspaceAgentStatus struct {
	Active         int    `json:"active"`
	LastRunID      string `json:"last_run_id,omitempty"`
	LastRunStatus  string `json:"last_run_status,omitempty"`
	LastRunSummary string `json:"last_run_summary,omitempty"`
}

// EmbeddedDoltStatus mirrors the supervisor stripe in the status
// response (gm-o9t8.1.9). Populated when the server has --embedded-dolt
// enabled; null/omitted when using an external Dolt via --dolt-url.
type EmbeddedDoltStatus struct {
	Enabled      bool   `json:"enabled"`
	State        string `json:"state"`
	Port         int    `json:"port"`
	DataDir      string `json:"data_dir"`
	RestartCount int64  `json:"restart_count"`
	LastError    string `json:"last_error,omitempty"`
}

// WorkspaceStatusResponse is the unified envelope the workspace status
// endpoint returns on 200. Field-for-field mirror of
// internal/server.WorkspaceStatusResponse; kept here so the client
// package stays independent of the server package.
type WorkspaceStatusResponse struct {
	WorkspaceID  string                `json:"workspace_id"`
	Mode         string                `json:"mode"`
	Repo         *WorkspaceRepoStatus  `json:"repo"`
	Beads        WorkspaceBeadCounts   `json:"beads"`
	Agents       *WorkspaceAgentStatus `json:"agents"`
	EmbeddedDolt *EmbeddedDoltStatus   `json:"embedded_dolt,omitempty"`
}

// WorkspaceStatus GETs /api/v1/workspaces/{wsid}/status. Pass "_" as
// wsid to ask the server to resolve the active workspace for the
// authenticated user.
//
// Sentinels mapped via mapErr: ErrUnauthorized (401), ErrNotFound (404),
// ErrNotImplemented (501). Other 4xx/5xx return *ServerError.
func (c *Client) WorkspaceStatus(ctx context.Context, wsid string) (*WorkspaceStatusResponse, error) {
	if strings.TrimSpace(wsid) == "" {
		return nil, fmt.Errorf("workspace status: wsid is required")
	}
	path := "/api/v1/workspaces/" + url.PathEscape(wsid) + "/status"
	resp, raw, err := c.doJSON(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, mapErr(resp, raw)
	}
	var out WorkspaceStatusResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("workspace status: decode response: %w", err)
	}
	return &out, nil
}

// ListWorkItemsByStatus is a convenience wrapper over ListWorkItems
// that filters by a single status. Used by the dashboard's fallback
// path when the primary status endpoint returns 501.
func (c *Client) ListWorkItemsByStatus(ctx context.Context, status string) ([]core.WorkItem, error) {
	return c.ListWorkItems(ctx, ListWorkItemsFilter{Status: status})
}
