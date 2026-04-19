package cli

import (
	"fmt"
	"sort"

	"github.com/MikeBengtson/gemba/internal/adapter/registry"
	"github.com/spf13/cobra"

	// Registers the v1 adaptors via init() side effects. Doctor relies on
	// these to enumerate what's available; new adaptors land by adding a
	// blank import here.
	_ "github.com/MikeBengtson/gemba/internal/adapter/bd"
	_ "github.com/MikeBengtson/gemba/internal/adapter/gc"
	_ "github.com/MikeBengtson/gemba/internal/adapter/gt"
)

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Report which WorkPlane + OrchestrationPlane adaptors the current workspace satisfies",
		Long: `Enumerates every adaptor compiled into this gemba binary, probes the
current workspace, and prints which can be satisfied.

Gemba requires exactly one WorkPlane adaptor (work tracker) paired with
exactly one OrchestrationPlane adaptor (agent runtime) to serve. If no
such pair is detected, doctor exits non-zero so it can be used in
preflight scripts.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDoctor(cmd)
		},
	}
}

func runDoctor(cmd *cobra.Command) error {
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

	fmt.Fprintln(out, "\npair detected — gemba serve will start")
	return nil
}
