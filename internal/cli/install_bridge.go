// gemba install-bridge wires the per-agent install registry
// (gm-native.18) so subsequent native sessions emit structured frames
// to the bridge log. Idempotent — a second run with the same agent +
// workspace is a no-op.

package cli

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/MikeBengtson/gemba/internal/adapter/native/install"
)

// newInstallBridgeCmd registers the `gemba install-bridge` subcommand.
// --agent <name> picks the strategy (claude | shell_only | future
// types). Legacy --profile flag still accepted with a deprecation
// notice so operators with old aliases / scripts don't break.
func newInstallBridgeCmd() *cobra.Command {
	var (
		agentName string
		profile   string
		workDir   string
		dryRun    bool
	)
	cmd := &cobra.Command{
		Use:   "install-bridge",
		Short: "Install gemba-bridge into the current workspace",
		Long: `install-bridge runs the per-agent installer for the current
workspace so gemba-bridge captures structured events from every
session started by the native orchestrator adaptor (gm-native).

Each installer knows what artifacts to scan for, how to merge with
operator-authored content, and which default skill files to copy when
absent. Operator-authored bytes outside the gemba sentinel block are
never overwritten.

Available installers:
  claude       — .claude/settings.local.json (merge), CLAUDE.md
                 (sentinel block), .claude/skills/ (copy if absent)
  shell_only   — .gemba/shellrc (idempotent overwrite when sentinel
                 present; preserved when operator owns the file)`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir := workDir
			if dir == "" {
				wd, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("cwd: %w", err)
				}
				dir = wd
			}
			selected, err := resolveAgent(agentName, profile)
			if err != nil {
				return err
			}
			return runInstall(cmd.Context(), cmd.OutOrStdout(), dir, selected, dryRun)
		},
	}
	cmd.Flags().StringVar(&agentName, "agent", "claude",
		"installer name (claude | shell_only); see install help for the registry")
	cmd.Flags().StringVar(&profile, "profile", "",
		"DEPRECATED: alias for --agent. claude_code → claude, prompt_command → shell_only")
	cmd.Flags().StringVar(&workDir, "workspace", "", "workspace directory (default: cwd)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print actions without writing files")
	return cmd
}

// resolveAgent normalises --agent + the legacy --profile alias into
// a single Installer name. The deprecation notice is intentionally
// non-fatal: scripts shouldn't break, but operators see "switch to
// --agent" the next time they run by hand.
func resolveAgent(agentName, profile string) (string, error) {
	if profile == "" {
		return agentName, nil
	}
	mapped := profile
	switch profile {
	case "claude_code":
		mapped = "claude"
	case "prompt_command":
		mapped = "shell_only"
	}
	if agentName != "" && agentName != mapped {
		return "", fmt.Errorf("install-bridge: --agent=%q and --profile=%q disagree (profile maps to %q); pick one", agentName, profile, mapped)
	}
	fmt.Fprintf(os.Stderr, "install-bridge: --profile is deprecated; use --agent=%s\n", mapped)
	return mapped, nil
}

// runInstall delegates to the registry. Out is io.Writer not just
// stdout so tests can capture the human-facing report.
func runInstall(ctx context.Context, out io.Writer, dir, agent string, dryRun bool) error {
	inst, err := install.Get(agent)
	if err != nil {
		return err
	}
	rep, err := inst.Install(ctx, install.Options{Dir: dir, DryRun: dryRun})
	for _, a := range rep.Actions {
		fmt.Fprintf(out, "  %-9s %s — %s\n", a.Kind, a.Path, a.Reason)
	}
	if err != nil {
		return fmt.Errorf("install-bridge[%s]: %w", agent, err)
	}
	mode := ""
	if dryRun {
		mode = " (dry-run)"
	}
	fmt.Fprintf(out, "install-bridge[%s]: %d action(s)%s\n", rep.Agent, len(rep.Actions), mode)
	return nil
}
