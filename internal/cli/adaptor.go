package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/GembaCore/gemba-core/core"
)

// newAdaptorCmd is the parent for `gemba adaptor …` subcommands. Today it
// exposes `register`; `test` lands with the conformance harness (gm-e3.5).
func newAdaptorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "adaptor",
		Short: "Operate on adaptors (register, test)",
		Long: `Adaptor operations.

Gemba requires exactly one WorkPlane + one OrchestrationPlane adaptor bound
per process (gm-root DD-1). 'adaptor register' binds an out-of-process
adaptor to a transport host; 'adaptor test' (gm-e3.5) runs the
conformance harness against one.`,
	}
	cmd.AddCommand(newAdaptorRegisterCmd())
	cmd.AddCommand(newAdaptorTestCmd())
	return cmd
}

type registerFlags struct {
	transport string
	target    string
	plane     string
	jsonOut   bool
}

func newAdaptorRegisterCmd() *cobra.Command {
	var f registerFlags

	cmd := &cobra.Command{
		Use:   "register",
		Short: "Register an adaptor with a transport host and negotiate versions",
		Long: `Register an out-of-process adaptor with a transport host.

The command dials the adaptor at --target over --transport, pulls its
capability manifest, and checks it against the core's protocol version
(core_version=` + core.ProtocolVersion + `). A mismatch exits non-zero with
an actionable message so CI gates can detect a bad pin.

The --plane flag selects which of the two planes the adaptor satisfies
(work|orchestration). Gemba requires exactly one of each at serve time.

Supported transports: api, jsonl, mcp (gm-root DD-12).

NOTE: The three transport hosts (HTTP handlers, JSONL pump, MCP server)
land incrementally in gm-e3.5 / gm-e4.x. This command validates the
command-line surface and runs version negotiation when a transport wire
is available. Until then it emits a structured not-yet-implemented error
per transport so operators get a consistent message from CI.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAdaptorRegister(cmd, f)
		},
	}

	cmd.Flags().StringVar(&f.transport, "transport", "",
		"transport kind: api | jsonl | mcp (required)")
	cmd.Flags().StringVar(&f.target, "target", "",
		"adaptor target (transport-specific: URL for api/mcp, command or socket for jsonl)")
	cmd.Flags().StringVar(&f.plane, "plane", "work",
		"which plane the adaptor satisfies: work | orchestration")
	cmd.Flags().BoolVar(&f.jsonOut, "json", false,
		"emit the registration result as JSON")

	_ = cmd.MarkFlagRequired("transport")
	_ = cmd.MarkFlagRequired("target")

	return cmd
}

func runAdaptorRegister(cmd *cobra.Command, f registerFlags) error {
	out := cmd.OutOrStdout()

	transport := core.Transport(f.transport)
	if !transport.Valid() {
		return fmt.Errorf(
			"unknown --transport %q: must be one of api, jsonl, mcp (gm-root DD-12)",
			f.transport,
		)
	}
	switch f.plane {
	case "work", "orchestration":
	default:
		return fmt.Errorf(
			"unknown --plane %q: must be work or orchestration (gm-root DD-1)",
			f.plane,
		)
	}
	if f.target == "" {
		return fmt.Errorf("--target is required")
	}

	// Transport-specific dial + describe + negotiate. Each transport's wire
	// client lands with gm-e3.5 (conformance harness needs it). Until then
	// this command documents the surface and returns a structured
	// not-yet-implemented so operators in CI see a consistent message.
	result := map[string]any{
		"core_version": core.ProtocolVersion,
		"transport":    string(transport),
		"plane":        f.plane,
		"target":       f.target,
		"status":       "not-yet-implemented",
		"next":         "transport wire client lands in gm-e3.5 (conformance harness)",
	}

	if f.jsonOut {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(result); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(out, "core_version: %s\n", core.ProtocolVersion)
		fmt.Fprintf(out, "transport:    %s\n", transport)
		fmt.Fprintf(out, "plane:        %s\n", f.plane)
		fmt.Fprintf(out, "target:       %s\n", f.target)
		fmt.Fprintln(out, "status:       not-yet-implemented")
		fmt.Fprintln(out, "note:         wire client for this transport lands with gm-e3.5")
	}
	return nil
}
