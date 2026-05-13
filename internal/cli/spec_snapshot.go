package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/GembaCore/gemba-core/internal/spec/snapshot"
)

// newSpecSnapshotCmd wires `gemba spec snapshot <spec.md>`.
//
// Pre-apply safety net: freezes the current spec.md into
// <spec_dir>/.snapshots/<spec_id>-<nonce-prefix>-<unix>.md before the
// reconciler is allowed to mutate beads. Prints the snapshot path on stdout.
func newSpecSnapshotCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "snapshot <spec.md>",
		Short: "Freeze a content-addressed copy of the spec under .snapshots/",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out, err := snapshot.Take(args[0])
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), out)
			return nil
		},
	}
}
