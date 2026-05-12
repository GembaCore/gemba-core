package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// BuildInfo carries -ldflags-injected build metadata from cmd/gemba.
type BuildInfo struct {
	Version string
	Commit  string
	Date    string
}

// Execute builds the root command and runs it. Returns the process exit code.
func Execute(b BuildInfo) int {
	root := newRootCmd(b)
	if err := root.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "gemba: %v\n", err)
		return 1
	}
	return 0
}

func newRootCmd(b BuildInfo) *cobra.Command {
	// statusCmd is registered as a subcommand AND used as the no-args
	// default. Per the locked verb taxonomy (gm-o9t8.5), `gemba` with
	// no arguments is equivalent to `gemba status`. We forward to the
	// status RunE here so both surfaces share one implementation.
	statusCmd := newStatusCmd()

	root := &cobra.Command{
		Use:   "gemba",
		Short: "Gemba — Atlassian-style UI for Gas Town v1 (Gas City-ready)",
		Long: `Gemba is a standalone sidecar binary that serves a browser-based
control surface for multi-agent orchestration.

  * v1 runtime: Gas Town 1.0 (stable). Talks via the gt and bd CLIs,
    reads ~/gt/ state for low-latency views.
  * Future runtime: Gas City (in alpha). Architecture and adapters are
    staged so the primary runtime flips from gt to gc via configuration,
    not code surgery, when Gas City reaches GA.

See https://github.com/GembaCore/gemba-core for documentation.`,
		SilenceUsage: true,
		// No-args dispatches to `gemba status`. cobra already
		// intercepts --help / --version on its own, so this only
		// fires for a truly empty argv. We propagate flags by
		// re-using the status flag set on the status subcommand.
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				// Unknown subcommand — let cobra's default handler
				// surface its usage error.
				return fmt.Errorf("unknown command %q for %q", args[0], cmd.CommandPath())
			}
			return statusCmd.RunE(statusCmd, args)
		},
	}

	root.AddCommand(
		statusCmd,
		newServeCmd(b),
		newDoctorCmd(),
		newVersionCmd(b),
		newAdaptorCmd(),
		newAuthCmd(),
		newLoginCmd(),
		newWhoamiCmd(),
		newInstallBridgeCmd(),
		newConflictsCmd(),
		newAffinityCmd(),
		newSessionHealthCmd(),
		newSessionStatusCmd(),
		newOperationalContextCmd(),
		newDispatchCmd(),
		newRetroCmd(),
		newConceptsCmd(),
		newBeadCmd(),
		newSessionCmd(),
		newAgentCmd(),
		newSizeCalibrationCmd(),
		newNewProjectCmd(),
		newRunCmd(),
		newLogsCmd(),
		newConfigCmd(),
		newDiffCmd(),
	)

	return root
}
