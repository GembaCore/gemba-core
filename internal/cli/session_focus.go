// gm-v5z2.3 — `gemba session focus` CLI.
//
// Operator-pinned focus directive scoped to one session. Three
// orthogonal restrictors AND together; the SELECTION layer reads
// the live row to demote out-of-intent candidates by the intent's
// demotion factor.
//
// Subcommands:
//
//   gemba session focus <session-id> --epic <id> [--rationale "..."]
//   gemba session focus <session-id> --label <name>
//   gemba session focus <session-id> --regex <pattern>
//   gemba session focus <session-id> --clear
//   gemba session focus list
//   gemba session focus get <session-id>
//   gemba session focus audit <session-id>
//
// All write paths emit a one-line confirmation; --json on a read
// path emits the stable wire envelope.

package cli

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/user"

	_ "github.com/go-sql-driver/mysql"
	"github.com/spf13/cobra"

	"github.com/GembaCore/gemba-core/internal/planner/intent"
)

func newSessionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "session",
		Short: "Per-session planner controls",
		Long: `Subcommands operate on per-session planner state. Today the only
surface is 'focus' (gm-v5z2.3 operator-pinned intent); future beads
land 'runway' (gm-v5z2.4) and 'health' under the same parent.`,
	}
	cmd.AddCommand(newSessionFocusCmd())
	return cmd
}

func newSessionFocusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "focus",
		Short: "Set or clear the operator-pinned focus directive for a session",
		Long: `Set or clear the operator-pinned focus directive for a session.
The selection layer (gm-v5z2.7) reads the live row to demote
out-of-intent candidates by the intent's demotion factor (default
0.4). The rule is SOFT: a P0 bead outside intent can still beat a
P3 bead inside intent if the score gap is wide enough.

Three orthogonal restrictors AND together; at least one must be set
on a focus call. To narrow further, run focus a second time with
the additional restrictor — the row is upserted, not patched.`,
	}
	cmd.PersistentFlags().String("dolt-url", "",
		"Dolt URL (e.g. mysql://root@127.0.0.1:3307/gemba) — required for live operations")
	cmd.AddCommand(
		newSessionFocusSetCmd(),
		newSessionFocusListCmd(),
		newSessionFocusGetCmd(),
		newSessionFocusAuditCmd(),
	)
	return cmd
}

// resolveDoltURL walks parents looking for --dolt-url since Cobra
// puts persistent flags on the parent, not the leaf invocation.
func resolveDoltURL(cmd *cobra.Command) string {
	for c := cmd; c != nil; c = c.Parent() {
		if v, _ := c.Flags().GetString("dolt-url"); v != "" {
			return v
		}
	}
	return ""
}

func openIntentStore(cmd *cobra.Command) (*intent.Store, *sql.DB, error) {
	url := resolveDoltURL(cmd)
	if url == "" {
		return nil, nil, fmt.Errorf("--dolt-url is required")
	}
	dsn, err := doltDSN(url)
	if err != nil {
		return nil, nil, err
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, nil, err
	}
	return intent.NewStore(db), db, nil
}

// currentActor stamps "cli:<user>" on every CLI-driven write so
// the audit log distinguishes operator-driven changes from future
// SPA / automation paths.
func currentActor() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return "cli:" + u.Username
	}
	if v := os.Getenv("USER"); v != "" {
		return "cli:" + v
	}
	return "cli:unknown"
}

func newSessionFocusSetCmd() *cobra.Command {
	var (
		epic      string
		label     string
		regex     string
		rationale string
		demotion  float64
		clear     bool
	)
	cmd := &cobra.Command{
		Use:   "set <session-id>",
		Short: "Set or clear the focus directive for a session",
		Long: `Set or clear the focus directive for a session. Pass at least
one of --epic / --label / --regex; multiple restrictors AND together.
--clear removes the directive entirely (and lands a 'clear' audit
row).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionID := args[0]
			store, db, err := openIntentStore(cmd)
			if err != nil {
				return err
			}
			defer db.Close()

			out := cmd.OutOrStdout()
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			actor := currentActor()
			if clear {
				if err := store.Clear(ctx, sessionID, actor, nil); err != nil {
					return err
				}
				fmt.Fprintf(out, "intent cleared for session %s\n", sessionID)
				return nil
			}
			if epic == "" && label == "" && regex == "" {
				return fmt.Errorf("set requires at least one of --epic / --label / --regex (or --clear)")
			}
			result, err := store.Set(ctx, intent.SetInput{
				SessionID:      sessionID,
				EpicID:         epic,
				Label:          label,
				BeadIDRegex:    regex,
				Rationale:      rationale,
				DemotionFactor: demotion,
				Actor:          actor,
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "intent set for session %s: epic=%q label=%q regex=%q demotion=%.2f\n",
				result.SessionID, result.EpicID, result.Label, result.BeadIDRegex, result.EffectiveDemotionFactor())
			return nil
		},
	}
	cmd.Flags().StringVar(&epic, "epic", "", "restrict candidates to descendants of this epic")
	cmd.Flags().StringVar(&label, "label", "", "restrict candidates carrying this bd label")
	cmd.Flags().StringVar(&regex, "regex", "", "restrict candidates whose id matches this regex")
	cmd.Flags().StringVar(&rationale, "rationale", "", `freeform "why this focus" text for the audit log`)
	cmd.Flags().Float64Var(&demotion, "demotion", 0,
		"out-of-intent demotion factor (0 = use default 0.4; 1 = no demotion)")
	cmd.Flags().BoolVar(&clear, "clear", false, "clear the focus directive for this session")
	return cmd
}

func newSessionFocusListCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List every session that has an active focus directive",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, db, err := openIntentStore(cmd)
			if err != nil {
				return err
			}
			defer db.Close()
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			items, err := store.List(ctx)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if asJSON {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(map[string]any{"intents": items})
			}
			if len(items) == 0 {
				fmt.Fprintln(out, "no session intents set")
				return nil
			}
			fmt.Fprintln(out, "session              epic              label             regex             demotion")
			for _, i := range items {
				fmt.Fprintf(out, "%-20s %-17s %-17s %-17s %.2f\n",
					i.SessionID, i.EpicID, i.Label, i.BeadIDRegex, i.EffectiveDemotionFactor())
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-parseable JSON")
	return cmd
}

func newSessionFocusGetCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "get <session-id>",
		Short: "Print the current focus directive for a session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, db, err := openIntentStore(cmd)
			if err != nil {
				return err
			}
			defer db.Close()
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			got, err := store.Get(ctx, args[0])
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if asJSON {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				if got == nil {
					return enc.Encode(map[string]any{"intent": nil})
				}
				return enc.Encode(map[string]any{"intent": got})
			}
			if got == nil {
				fmt.Fprintf(out, "no focus directive set for session %s\n", args[0])
				return nil
			}
			fmt.Fprintf(out, "session:   %s\n", got.SessionID)
			if got.EpicID != "" {
				fmt.Fprintf(out, "epic:      %s\n", got.EpicID)
			}
			if got.Label != "" {
				fmt.Fprintf(out, "label:     %s\n", got.Label)
			}
			if got.BeadIDRegex != "" {
				fmt.Fprintf(out, "regex:     %s\n", got.BeadIDRegex)
			}
			fmt.Fprintf(out, "demotion:  %.2f\n", got.EffectiveDemotionFactor())
			if got.Rationale != "" {
				fmt.Fprintf(out, "rationale: %s\n", got.Rationale)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-parseable JSON")
	return cmd
}

func newSessionFocusAuditCmd() *cobra.Command {
	var (
		asJSON bool
		limit  int
	)
	cmd := &cobra.Command{
		Use:   "audit <session-id>",
		Short: "Print the focus-change history for a session, newest first",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, db, err := openIntentStore(cmd)
			if err != nil {
				return err
			}
			defer db.Close()
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			rows, err := store.Audit(ctx, args[0], limit)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if asJSON {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(map[string]any{"audit": rows})
			}
			if len(rows) == 0 {
				fmt.Fprintf(out, "no audit history for session %s\n", args[0])
				return nil
			}
			fmt.Fprintln(out, "at                              action  actor             from → to")
			for _, e := range rows {
				prior := "—"
				next := "—"
				if e.Prior != nil {
					prior = describeIntent(*e.Prior)
				}
				if e.Next != nil {
					next = describeIntent(*e.Next)
				}
				fmt.Fprintf(out, "%-30s %-7s %-17s %s → %s\n",
					e.At.UTC().Format("2006-01-02T15:04:05Z"),
					string(e.Action), e.Actor, prior, next)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-parseable JSON")
	cmd.Flags().IntVar(&limit, "limit", 50, "max rows to return (0 = unbounded)")
	return cmd
}

func describeIntent(i intent.Intent) string {
	parts := []string{}
	if i.EpicID != "" {
		parts = append(parts, "epic="+i.EpicID)
	}
	if i.Label != "" {
		parts = append(parts, "label="+i.Label)
	}
	if i.BeadIDRegex != "" {
		parts = append(parts, "regex="+i.BeadIDRegex)
	}
	if len(parts) == 0 {
		return "(empty)"
	}
	return joinComma(parts)
}

func joinComma(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ","
		}
		out += p
	}
	return out
}
