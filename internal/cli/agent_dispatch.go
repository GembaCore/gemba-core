// gm-o9t8.1.5.5 — `gemba agent dispatch` CLI.
//
// Fire-and-forget cascade dispatch. POSTs the bead's id to
// /api/work-items/{id}/cascade-dispatch and prints the resulting run
// id (the cascade wrapper id) to stdout. Operators chain this with
// `gemba logs -f <run_id>` when they want to attach to the live event
// stream after the fact.
//
// Sibling commands:
//
//	gemba run <bead>      — same dispatch, then auto-attaches to /events
//	gemba logs -f <run>   — re-attach to /events for a known run
//
// Note: this is unrelated to the existing `gemba dispatch` command
// (internal/cli/dispatch.go), which drives the auto-dispatch policy
// daemon. The two share neither flags nor code paths.

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/spf13/cobra"

	"github.com/GembaCore/gemba-core/internal/cli/serverconfig"
)

// cascadeDispatchRespEnvelope is the subset of cascadeDispatchResponse
// (internal/server/work_items_cascade.go) we consume on the CLI side.
// We deliberately don't import the server struct — the CLI is a wire
// client and decouples from server-internal types.
type cascadeDispatchRespEnvelope struct {
	WrapperID  string `json:"wrapper_id"`
	Dispatched []struct {
		WorkItemID string `json:"work_item_id"`
		SessionID  string `json:"session_id,omitempty"`
	} `json:"dispatched"`
	Blocked []string `json:"blocked,omitempty"`
	Skipped []string `json:"skipped,omitempty"`
	Errors  []struct {
		WorkItemID string `json:"work_item_id"`
		Message    string `json:"message"`
	} `json:"errors,omitempty"`
}

func newAgentDispatchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dispatch <bead-id>",
		Short: "Fire-and-forget cascade dispatch; prints the run id",
		Long: `Dispatch the runnable leaves beneath <bead-id> by POSTing to
/api/work-items/{id}/cascade-dispatch. Prints the cascade wrapper id
(treat as run id) to stdout on success.

Resume the live event stream later with:

  gemba logs -f <run-id>`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			baseURL, _ := cmd.Flags().GetString("base-url")
			resolved, err := serverconfig.Resolve(baseURL)
			if err != nil {
				return err
			}
			baseURL = resolved
			agentType, _ := cmd.Flags().GetString("agent-type")
			limit, _ := cmd.Flags().GetInt("limit")

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			resp, err := postCascadeDispatch(ctx, baseURL, args[0], agentType, limit)
			if err != nil {
				return err
			}
			runID := resp.WrapperID
			if runID == "" {
				runID = args[0]
			}
			fmt.Fprintln(cmd.OutOrStdout(), runID)
			return nil
		},
	}
	cmd.Flags().String("base-url", "", "base URL of a running gemba serve (default http://localhost:7666)")
	cmd.Flags().String("agent-type", "", "agent_type to pass to cascade-dispatch (optional)")
	cmd.Flags().Int("limit", 0, "limit on dispatched leaves (0 = server default)")
	return cmd
}

// postCascadeDispatch issues POST /api/work-items/{id}/cascade-dispatch
// and decodes the wrapper response. Returns the decoded envelope or a
// wrapped error on transport / status failure.
func postCascadeDispatch(ctx context.Context, baseURL, beadID, agentType string, limit int) (*cascadeDispatchRespEnvelope, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse base-url: %w", err)
	}
	u.Path = "/api/work-items/" + url.PathEscape(beadID) + "/cascade-dispatch"

	body := map[string]any{}
	if agentType != "" {
		body["agent_type"] = agentType
	}
	if limit > 0 {
		body["limit"] = limit
	}
	buf, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	httpResp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("POST %s: %w", u.String(), err)
	}
	defer httpResp.Body.Close()
	if httpResp.StatusCode/100 != 2 {
		raw, _ := io.ReadAll(httpResp.Body)
		return nil, fmt.Errorf("POST %s: %s: %s", u.String(), httpResp.Status, string(raw))
	}
	var out cascadeDispatchRespEnvelope
	if err := json.NewDecoder(httpResp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode cascade-dispatch response: %w", err)
	}
	return &out, nil
}
