// gemba dispatch (gm-s47n.6.5).
//
// Operator surface for the auto-dispatch policy: status / on /
// off / set. The kill switch is one command:
//
//   gemba dispatch off          → planner stops auto-dispatching
//   gemba dispatch on           → planner resumes
//
// All subcommands operate against a JSON file (default
// ./.gemba/dispatch.json). The auto-dispatch daemon (gm-s47n.6.3)
// reads the same file at the start of every loop tick — no
// long-lived connection needed for the kill switch to take effect.

package cli

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/MikeBengtson/gemba/internal/planner"
)

// defaultPolicyFilePath is the conventional location for the
// dispatch policy file. Callers may override via --file. Kept under
// .gemba/ so it lives alongside the rest of per-rig settings.
const defaultPolicyFilePath = ".gemba/dispatch.json"

func newDispatchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dispatch",
		Short: "Inspect / toggle the auto-dispatch policy",
		Long: `Auto-dispatch is opt-in per rig. The planner reads the
dispatch policy from .gemba/dispatch.json (override via --file)
before each loop tick, so toggling 'gemba dispatch off' takes
effect on the next iteration without restarting any daemon.

Subcommands:
  status — print the current policy + flag warnings.
  on     — flip Enabled to true (auto-dispatch resumes).
  off    — flip Enabled to false (kill switch).
  set    — set MinIntervalPerSession or MaxConcurrent.`,
	}
	cmd.PersistentFlags().String("file", "",
		"path to the dispatch policy JSON (default ./.gemba/dispatch.json)")
	cmd.AddCommand(
		newDispatchStatusCmd(),
		newDispatchOnCmd(),
		newDispatchOffCmd(),
		newDispatchSetCmd(),
	)
	return cmd
}

func resolvePolicyFile(cmd *cobra.Command) string {
	if v, _ := cmd.Flags().GetString("file"); v != "" {
		return v
	}
	if v, _ := cmd.Root().PersistentFlags().GetString("file"); v != "" {
		return v
	}
	// Walk parents — Cobra puts the persistent flag on the parent,
	// not the leaf. Fall through to the convention.
	for c := cmd; c != nil; c = c.Parent() {
		if f := c.Flag("file"); f != nil && f.Value.String() != "" {
			return f.Value.String()
		}
	}
	return defaultPolicyFilePath
}

func newDispatchStatusCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Print the current dispatch policy",
		RunE: func(cmd *cobra.Command, args []string) error {
			path := resolvePolicyFile(cmd)
			pol, err := planner.LoadPolicyFile(path)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if asJSON {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(struct {
					Path   string                 `json:"path"`
					Policy planner.DispatchPolicy `json:"policy"`
				}{Path: path, Policy: pol})
			}
			fmt.Fprintf(out, "policy file: %s\n", path)
			fmt.Fprintf(out, "  enabled:                  %v\n", pol.Enabled)
			fmt.Fprintf(out, "  min_interval_per_session: %s\n", policyDuration(pol.MinIntervalPerSession, planner.DefaultMinIntervalPerSession))
			fmt.Fprintf(out, "  max_concurrent:           %d\n", policyInt(pol.MaxConcurrent, planner.DefaultMaxConcurrent))
			if !pol.Enabled {
				fmt.Fprintln(out, "\nauto-dispatch is OFF (kill switch engaged).")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-parseable JSON")
	return cmd
}

func newDispatchOnCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "on",
		Short: "Enable auto-dispatch (the planner resumes pulling work)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return mutatePolicy(cmd, func(p *planner.DispatchPolicy) {
				p.Enabled = true
			}, "auto-dispatch ENABLED")
		},
	}
}

func newDispatchOffCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "off",
		Short: "Disable auto-dispatch (kill switch — takes effect on next loop tick)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return mutatePolicy(cmd, func(p *planner.DispatchPolicy) {
				p.Enabled = false
			}, "auto-dispatch DISABLED")
		},
	}
}

func newDispatchSetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set [key=value ...]",
		Short: "Tune dispatch policy fields (min-interval / max-concurrent)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return mutatePolicy(cmd, func(p *planner.DispatchPolicy) {
				for _, kv := range args {
					applyKV(p, kv, cmd)
				}
			}, "policy updated")
		},
	}
	return cmd
}

// applyKV parses a single key=value pair and mutates the policy
// in place. Unknown keys print a warning to stderr but don't
// error — operators iterating from the help text get a clear
// signal without breaking the rest of their batch.
func applyKV(p *planner.DispatchPolicy, kv string, cmd *cobra.Command) {
	for i := 0; i < len(kv); i++ {
		if kv[i] == '=' {
			key := kv[:i]
			val := kv[i+1:]
			switch key {
			case "min_interval_per_session", "min-interval-per-session", "min-interval":
				if d, err := time.ParseDuration(val); err == nil {
					p.MinIntervalPerSession = d
					return
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s: bad duration\n", kv)
			case "max_concurrent", "max-concurrent":
				if n, err := strconv.Atoi(val); err == nil {
					p.MaxConcurrent = n
					return
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s: not an integer\n", kv)
			case "enabled":
				if b, err := strconv.ParseBool(val); err == nil {
					p.Enabled = b
					return
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s: not a bool\n", kv)
			default:
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: unknown key %q\n", key)
			}
			return
		}
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "warning: %q is not a key=value pair\n", kv)
}

// mutatePolicy is the read-modify-write helper every mutating
// subcommand shares. Loads the policy (or starts from zero if the
// file's missing), runs the mutator, writes back, prints the
// confirmation line.
func mutatePolicy(cmd *cobra.Command, mut func(*planner.DispatchPolicy), msg string) error {
	path := resolvePolicyFile(cmd)
	pol, err := planner.LoadPolicyFile(path)
	if err != nil {
		return err
	}
	mut(&pol)
	// Materialise the file's parent so SavePolicyFile's MkdirAll
	// has a sensible base when the operator hand-rolled --file.
	if filepath.Dir(path) != "" {
		// SavePolicyFile already mkdirs; no extra work here.
	}
	if err := planner.SavePolicyFile(path, pol); err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), msg)
	return nil
}

// policyDuration formats the duration field, substituting "default"
// when the value is zero (LoadPolicyFile preserves zero so the
// resolution rule lives at the gate's check-time, not the loader).
func policyDuration(d, def time.Duration) string {
	if d <= 0 {
		return fmt.Sprintf("default (%s)", def)
	}
	return d.String()
}

func policyInt(n, def int) int {
	if n <= 0 {
		return def
	}
	return n
}
