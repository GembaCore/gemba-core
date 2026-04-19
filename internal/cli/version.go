package cli

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime/debug"
	"strings"

	"github.com/spf13/cobra"
)

func newVersionCmd(b BuildInfo) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print build info",
		RunE: func(cmd *cobra.Command, args []string) error {
			b = enrichBuildInfo(b, debug.ReadBuildInfo, gitProbe)
			info := map[string]string{
				"version": b.Version,
				"commit":  b.Commit,
				"date":    b.Date,
			}
			if asJSON {
				out, _ := json.MarshalIndent(info, "", "  ")
				fmt.Println(string(out))
				return nil
			}
			fmt.Printf("gemba %s (commit %s, built %s)\n",
				info["version"], info["commit"], info["date"])
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON")
	return cmd
}

// gitInfo carries the values discovered by probing a git working tree.
type gitInfo struct {
	Revision string // short sha
	Modified bool   // dirty worktree
	Time     string // RFC3339-ish commit time, may be empty
}

// enrichBuildInfo fills placeholder BuildInfo fields from two sources:
// (1) runtime/debug.ReadBuildInfo — populated by `go build` / `go install`
// from a VCS tree, and (2) a `git` probe — needed for `go run`, which Go
// intentionally does not VCS-stamp. Release binaries built via the Makefile
// with -ldflags take precedence and skip both fallbacks.
func enrichBuildInfo(
	b BuildInfo,
	read func() (*debug.BuildInfo, bool),
	probe func() gitInfo,
) BuildInfo {
	if !placeholder(b) {
		return b
	}
	if info, ok := read(); ok {
		applyDebugInfo(&b, info)
	}
	if placeholder(b) {
		applyGitInfo(&b, probe())
	}
	return b
}

func placeholder(b BuildInfo) bool {
	return b.Version == "" || b.Version == "dev" ||
		b.Commit == "" || b.Commit == "none" ||
		b.Date == "" || b.Date == "unknown"
}

func applyDebugInfo(b *BuildInfo, info *debug.BuildInfo) {
	var rev, modified, when string
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			modified = s.Value
		case "vcs.time":
			when = s.Value
		}
	}
	if (b.Version == "" || b.Version == "dev") &&
		info.Main.Version != "" && info.Main.Version != "(devel)" {
		b.Version = info.Main.Version
	}
	if (b.Commit == "" || b.Commit == "none") && rev != "" {
		short := rev
		if len(short) > 7 {
			short = short[:7]
		}
		if modified == "true" {
			short += "-dirty"
		}
		b.Commit = short
	}
	if (b.Date == "" || b.Date == "unknown") && when != "" {
		b.Date = when
	}
}

func applyGitInfo(b *BuildInfo, g gitInfo) {
	if (b.Commit == "" || b.Commit == "none") && g.Revision != "" {
		short := g.Revision
		if g.Modified {
			short += "-dirty"
		}
		b.Commit = short
	}
	if (b.Date == "" || b.Date == "unknown") && g.Time != "" {
		b.Date = g.Time
	}
}

// gitProbe shells out to git in the current working directory. Failures are
// silent: the caller just sees zero values, and the original placeholders
// survive. Used only when ldflags + debug.BuildInfo both miss (the `go run`
// case).
func gitProbe() gitInfo {
	var g gitInfo
	if out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output(); err == nil {
		g.Revision = strings.TrimSpace(string(out))
	} else {
		return g
	}
	if out, err := exec.Command("git", "status", "--porcelain").Output(); err == nil {
		g.Modified = len(strings.TrimSpace(string(out))) > 0
	}
	if out, err := exec.Command("git", "log", "-1", "--format=%cI").Output(); err == nil {
		g.Time = strings.TrimSpace(string(out))
	}
	return g
}
