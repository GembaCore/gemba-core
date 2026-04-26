package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/spf13/cobra"

	"github.com/MikeBengtson/gemba/internal/adapter/registry"
	eventlog "github.com/MikeBengtson/gemba/internal/events/log"

	// Registers the v1 adaptors via init() side effects. Doctor relies on
	// these to enumerate what's available; new adaptors land by adding a
	// blank import here.
	_ "github.com/MikeBengtson/gemba/internal/adapter/bd"
	_ "github.com/MikeBengtson/gemba/internal/adapter/gc"
	_ "github.com/MikeBengtson/gemba/internal/adapter/gt"
)

type doctorFlags struct {
	eventLogDir string
}

func newDoctorCmd() *cobra.Command {
	var flags doctorFlags
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Report which WorkPlane + OrchestrationPlane adaptors the current workspace satisfies",
		Long: `Enumerates every adaptor compiled into this gemba binary, probes the
current workspace, and prints which can be satisfied.

Gemba requires exactly one WorkPlane adaptor (work tracker) paired with
exactly one OrchestrationPlane adaptor (agent runtime) to serve. If no
such pair is detected, doctor exits non-zero so it can be used in
preflight scripts.

Also reports the event log footprint (size, rotated file count, oldest
retained event) when --eventlog-dir is supplied or ~/.gemba/events exists.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDoctor(cmd, flags)
		},
	}

	cmd.Flags().StringVar(&flags.eventLogDir, "eventlog-dir", "",
		"directory containing the event log (default: ~/.gemba/events if present)")

	return cmd
}

func runDoctor(cmd *cobra.Command, flags doctorFlags) error {
	out := cmd.OutOrStdout()
	adaptors := registry.List()

	byPlane := map[registry.Plane][]registry.Adaptor{}
	for _, a := range adaptors {
		byPlane[a.Plane] = append(byPlane[a.Plane], a)
	}

	order := []registry.Plane{registry.WorkPlane, registry.OrchestrationPlane}
	titles := map[registry.Plane]string{
		registry.WorkPlane:          "WorkPlane adaptors",
		registry.OrchestrationPlane: "OrchestrationPlane adaptors",
	}
	detectedPerPlane := map[registry.Plane]int{}

	for _, plane := range order {
		as := byPlane[plane]
		sort.Slice(as, func(i, j int) bool { return as[i].Name < as[j].Name })
		fmt.Fprintf(out, "%s:\n", titles[plane])
		if len(as) == 0 {
			fmt.Fprintln(out, "  (none registered)")
			continue
		}
		for _, a := range as {
			res := registry.DetectResult{Reason: "detect not implemented"}
			if a.Detect != nil {
				res = a.Detect()
			}
			if res.Ok {
				fmt.Fprintf(out, "  ✓ %s\n", a.Name)
				detectedPerPlane[plane]++
			} else {
				fmt.Fprintf(out, "  ✗ %s — %s\n", a.Name, res.Reason)
			}
		}
	}

	if detectedPerPlane[registry.WorkPlane] == 0 ||
		detectedPerPlane[registry.OrchestrationPlane] == 0 {
		return fmt.Errorf(
			"no WorkPlane + OrchestrationPlane pair detected " +
				"(need at least one of each); gemba serve would refuse to start")
	}

	// Event log footprint is informational — never gates the exit code,
	// since the log may not exist yet on a fresh install.
	reportEventLog(out, flags.eventLogDir)

	fmt.Fprintln(out, "\npair detected — gemba serve will start")
	return nil
}

// reportEventLog prints the event log footprint. Resolution order:
//  1. --eventlog-dir flag (if non-empty)
//  2. ~/.gemba/events if it exists
//
// If neither resolves to a real directory, the section is skipped so fresh
// installs don't see spurious warnings.
func reportEventLog(out interface{ Write(p []byte) (int, error) }, flag string) {
	dir := flag
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return
		}
		candidate := filepath.Join(home, ".gemba", "events")
		if _, err := os.Stat(candidate); err != nil {
			return
		}
		dir = candidate
	}

	stats, err := eventlog.ScanDir(dir, eventlog.DefaultBaseName)
	if err != nil {
		fmt.Fprintf(out, "\nEvent log: error scanning %s: %v\n", dir, err)
		return
	}

	fmt.Fprintf(out, "\nEvent log (%s):\n", dir)
	fmt.Fprintf(out, "  total size:        %s\n", humanBytes(stats.TotalSizeBytes))
	fmt.Fprintf(out, "  active file:       %s\n", humanBytes(stats.ActiveSizeBytes))
	fmt.Fprintf(out, "  rotated files:     %d\n", stats.RotatedFileCount)
	if stats.OldestRetainedAt.IsZero() {
		fmt.Fprintln(out, "  oldest retained:   (none)")
	} else {
		age := time.Since(stats.OldestRetainedAt).Round(time.Second)
		fmt.Fprintf(out, "  oldest retained:   %s (%s ago)\n",
			stats.OldestRetainedAt.UTC().Format(time.RFC3339), age)
	}
}

// humanBytes formats a byte count with a binary suffix. Keeps the doctor
// output readable without pulling in a sizing library.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
