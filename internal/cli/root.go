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

See https://github.com/MikeBengtson/gemba for documentation.`,
		SilenceUsage: true,
	}

	root.AddCommand(
		newServeCmd(),
		newDoctorCmd(),
		newVersionCmd(b),
		newAdaptorCmd(),
		newAuthCmd(),
	)

	return root
}
