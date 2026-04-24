// Command gemba is the Gemba CLI: a standalone sidecar that serves a
// browser-based Atlassian-style UI for a Gas Town workspace today, and —
// via a thin adapter flip — for a Gas City workspace when Gas City
// reaches GA. Gas Town v1.0 is the stable runtime; Gas City is in alpha.
//
// See ../../README.md for the architectural charter, locked decisions,
// and the stability posture.
package main

import (
	"os"

	"github.com/MikeBengtson/gemba/internal/cli"
	// Side-effect: registers the bundled coaching + manager skills FS
	// with internal/adapter/native/install so `gemba install-bridge`
	// can copy them into a fresh worktree (gm-native.18).
	_ "github.com/MikeBengtson/gemba/cmd/gemba-bridge/skills"
)

// Injected at build time via -ldflags. See Makefile.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	os.Exit(cli.Execute(cli.BuildInfo{
		Version: version,
		Commit:  commit,
		Date:    date,
	}))
}
