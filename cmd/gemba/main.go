// Command bc is the Gemba CLI: a standalone sidecar that serves a
// browser-based Atlassian-style UI for a Gas Town workspace today, and —
// via a thin adapter flip — for a Gas City workspace when Gas City
// reaches GA. Gas Town v1.0 is the stable runtime; Gas City is in alpha.
//
// See ../../README.md for the architectural charter, locked decisions,
// and the stability posture.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// These are injected at build time via -ldflags. See Makefile.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
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

See https://github.com/YOUR_ORG/gemba for documentation.`,
		SilenceUsage: true,
	}

	root.AddCommand(
		newServeCmd(),
		newDoctorCmd(),
		newVersionCmd(),
	)

	if err := root.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "bc: %v\n", err)
		os.Exit(1)
	}
}
