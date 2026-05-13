// gm-o9t8.1.5.5 — `gemba agent dispatch` CLI.
// gm-o9t8.1.11    — migrated to the typed client at internal/client.
//
// Fire-and-forget cascade dispatch. POSTs the bead's id to
// /api/work-items/{id}/cascade-dispatch via the typed client (which
// auto-injects Authorization + X-GEMBA-Confirm + User-Agent) and prints
// the resulting run id (the cascade wrapper id) to stdout. Operators
// chain this with `gemba logs -f <run_id>` when they want to attach to
// the live event stream after the fact.
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
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	gembaclient "github.com/GembaCore/gemba-core/internal/client"
)

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
			server, err := resolveServerFlag(cmd, cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			agentType, _ := cmd.Flags().GetString("agent-type")
			limit, _ := cmd.Flags().GetInt("limit")

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			client, err := runClientFactoryFn(server)
			if err != nil {
				return err
			}
			resp, err := client.CascadeDispatchWorkItem(ctx, args[0], gembaclient.CascadeDispatchInput{
				AgentType: agentType,
				Limit:     limit,
			})
			if err != nil {
				return mapDispatchErr(err)
			}
			runID := resp.WrapperID
			if runID == "" {
				runID = args[0]
			}
			fmt.Fprintln(cmd.OutOrStdout(), runID)
			return nil
		},
	}
	addServerFlags(cmd)
	cmd.Flags().String("agent-type", "", "agent_type to pass to cascade-dispatch (optional)")
	cmd.Flags().Int("limit", 0, "limit on dispatched leaves (0 = server default)")
	return cmd
}

// mapDispatchErr surfaces well-known sentinels with the same exit-code
// contract as `gemba bead` (4=unauthorized, 5=not found). For other
// failures the error is returned verbatim so cobra prints it.
func mapDispatchErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, gembaclient.ErrUnauthorized):
		return &exitError{code: 4, err: err}
	case errors.Is(err, gembaclient.ErrNotFound):
		return &exitError{code: 5, err: err}
	}
	return err
}
