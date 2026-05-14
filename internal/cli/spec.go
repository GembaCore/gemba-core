// gemba spec — ASDD interception layer command tree.
//
// Subcommands:
//
//	spec new <slug>     Scaffold a new spec directory (and optionally Kit files).
//	spec init           Seed ASDD project artifacts (constitution, hooks, CLAUDE.md).
//	spec hook           PreToolUse hook handlers (Write/Edit/NotebookEdit, Bash).
//	spec preflight      Refuse /implement when tasks.md still exists.
//	spec reconcile      Stub — to be implemented by the analyzer (gm-v0sp.21).
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/GembaCore/gemba-core/internal/spec/constitution"
	"github.com/GembaCore/gemba-core/internal/spec/hook"
	"github.com/GembaCore/gemba-core/internal/spec/lint"
	"github.com/GembaCore/gemba-core/internal/spec/preflight"
	"github.com/GembaCore/gemba-core/internal/spec/scaffold"
)

func newSpecCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "spec",
		Short: "ASDD spec interception layer (slash commands, hooks, preflight)",
	}
	cmd.AddCommand(
		newSpecNewCmd(),
		newSpecInitCmd(),
		newSpecHookCmd(),
		newSpecPreflightCmd(),
		newSpecReconcileStubCmd(),
		newSpecLintCmd(),
		newSpecSnapshotCmd(),
		newSpecAnalyzeCmd(),
		newSpecDiffCmd(),
		newSpecApplyCmd(),
		newSpecPromoteCmd(),
		newSpecCloseCheckCmd(),
	)
	return cmd
}

// --- spec new ---

func newSpecNewCmd() *cobra.Command {
	var kit bool
	var force bool
	var projectRoot string
	cmd := &cobra.Command{
		Use:   "new <slug>",
		Short: "Scaffold a new spec directory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			slug := args[0]
			root, err := resolveRoot(projectRoot)
			if err != nil {
				return err
			}
			specDir := filepath.Join(root, "specs", slug)
			if err := os.MkdirAll(specDir, 0o755); err != nil {
				return err
			}
			specMD := filepath.Join(specDir, "spec.md")
			if _, err := os.Stat(specMD); os.IsNotExist(err) {
				if err := os.WriteFile(specMD, []byte("# Spec: "+slug+"\n\n## Tasks\n"), 0o644); err != nil {
					return err
				}
			}
			// Decide --kit: explicit flag wins; otherwise enable when constitution says asdd_mode.
			c, _, _ := constitution.Load(root)
			wantKit := kit || c.ASDDModeEffective()
			if wantKit {
				r, err := scaffold.Apply(root, scaffold.Options{
					Slug:               slug,
					WriteSlashCommands: true,
					UpdateClaudeMD:     true,
				})
				if err != nil {
					return err
				}
				for _, p := range r.Created {
					fmt.Fprintln(cmd.OutOrStdout(), "created:", p)
				}
			}
			fmt.Fprintln(cmd.OutOrStdout(), "spec:", specMD)

			// Phase-2 canonical spec doc + lockfile (gm-v0sp.13).
			owner := resolveOwner(root)
			canonical, lockPath, err := writePhase2Spec(root, slug, time.Now(), force, owner)
			if err != nil {
				if errors.Is(err, ErrSpecExists) {
					fmt.Fprintln(cmd.OutOrStdout(), "doc: exists (use --force to overwrite):", canonical)
					return nil
				}
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "doc:", canonical)
			fmt.Fprintln(cmd.OutOrStdout(), "lock:", lockPath)
			return nil
		},
	}
	cmd.Flags().BoolVar(&kit, "kit", false, "scaffold .claude/commands/{tasks,implement}.md slash commands")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing docs/specs/<date>-<slug>.md")
	cmd.Flags().StringVar(&projectRoot, "project", "", "project root (default: cwd)")
	return cmd
}

// --- spec init ---

func newSpecInitCmd() *cobra.Command {
	var updateClaudeMD bool
	var asdd bool
	var projectRoot string
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Seed ASDD project artifacts (constitution, hooks, CLAUDE.md)",
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRoot(projectRoot)
			if err != nil {
				return err
			}
			opts := scaffold.Options{
				WriteHooks:         asdd,
				WriteConstitution:  asdd,
				UpdateClaudeMD:     asdd || updateClaudeMD,
				WriteSlashCommands: asdd,
			}
			r, err := scaffold.Apply(root, opts)
			if err != nil {
				return err
			}
			for _, p := range r.Created {
				fmt.Fprintln(cmd.OutOrStdout(), "created:", p)
			}
			for _, p := range r.Updated {
				fmt.Fprintln(cmd.OutOrStdout(), "updated:", p)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asdd, "asdd", false, "full ASDD seed (constitution + hooks + commands + CLAUDE.md)")
	cmd.Flags().BoolVar(&updateClaudeMD, "update-claude-md", false, "append the ASDD stanza to CLAUDE.md")
	cmd.Flags().StringVar(&projectRoot, "project", "", "project root (default: cwd)")
	return cmd
}

// --- spec hook ---

func newSpecHookCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hook",
		Short: "PreToolUse hook handlers (invoked by Claude Code)",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "deprecated-task-list",
			Short: "Reject Write/Edit/NotebookEdit to tasks.md / todo.md",
			RunE: func(c *cobra.Command, args []string) error {
				root, _ := resolveRoot("")
				con, _, _ := constitution.Load(root)
				code := hook.Run("write", os.Stdin, os.Stderr, con)
				if code != 0 {
					os.Exit(code)
				}
				return nil
			},
		},
		&cobra.Command{
			Use:   "deprecated-task-list-bash",
			Short: "Reject Bash redirects/sed to tasks.md / todo.md",
			RunE: func(c *cobra.Command, args []string) error {
				root, _ := resolveRoot("")
				con, _, _ := constitution.Load(root)
				code := hook.Run("bash", os.Stdin, os.Stderr, con)
				if code != 0 {
					os.Exit(code)
				}
				return nil
			},
		},
	)
	return cmd
}

// --- spec preflight ---

func newSpecPreflightCmd() *cobra.Command {
	var slug, projectRoot, reason string
	var allow bool
	cmd := &cobra.Command{
		Use:   "preflight",
		Short: "Refuse /implement when tasks.md still exists",
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRoot(projectRoot)
			if err != nil {
				return err
			}
			c, _, _ := constitution.Load(root)
			r := preflight.Run(root, slug, c)
			if r.OK {
				return nil
			}
			if allow {
				if err := emitBypassAudit(root, slug, reason); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: audit emit failed: %v\n", err)
				}
				return nil
			}
			fmt.Fprintln(cmd.ErrOrStderr(), r.Reason)
			for _, p := range r.OffenderPaths {
				fmt.Fprintln(cmd.ErrOrStderr(), "  offender:", p)
			}
			os.Exit(2)
			return nil
		},
	}
	cmd.Flags().StringVar(&slug, "slug", "", "spec slug")
	cmd.Flags().StringVar(&projectRoot, "project", "", "project root (default: cwd)")
	cmd.Flags().BoolVar(&allow, "allow-tasks-md", false, "bypass and emit an audit event")
	cmd.Flags().StringVar(&reason, "reason", "", "bypass reason (recorded in the audit event)")
	return cmd
}

// --- spec reconcile (stub for analyzer follow-up) ---

func newSpecReconcileStubCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reconcile <slug>",
		Short: "Reconcile spec.md tasks into bd (stub — full impl in gm-v0sp.21)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), "reconcile: stub — analyzer lives in gm-v0sp.21")
			return nil
		},
	}
}

// --- helpers ---

func resolveRoot(override string) (string, error) {
	if override != "" {
		return filepath.Abs(override)
	}
	return os.Getwd()
}

// emitBypassAudit appends a JSONL audit record under .gemba/audit/ for the
// preflight bypass event. Keeping local-only for now; centralized audit
// dispatch lands when the audit emitter wiring is in place.
func emitBypassAudit(root, slug, reason string) error {
	dir := filepath.Join(root, ".gemba", "audit")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	rec := map[string]any{
		"ts":     time.Now().UTC().Format(time.RFC3339Nano),
		"event":  "spec.preflight.bypassed",
		"slug":   slug,
		"reason": reason,
		"actor":  os.Getenv("USER"),
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, "audit.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(data, '\n'))
	return err
}

// --- constitution lint ---

func newConstitutionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "constitution",
		Short: "Inspect and lint the project constitution",
	}
	cmd.AddCommand(newConstitutionLintCmd())
	return cmd
}

func newConstitutionLintCmd() *cobra.Command {
	var strict bool
	var projectRoot string
	cmd := &cobra.Command{
		Use:   "lint",
		Short: "Lint the project against constitution rules",
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRoot(projectRoot)
			if err != nil {
				return err
			}
			c, cpath, _ := constitution.Load(root)
			findings, err := lint.Scan(root, c)
			if err != nil {
				return err
			}
			cf, err := lint.ScanConstitution(cpath)
			if err != nil {
				return err
			}
			findings = append(findings, cf...)
			for _, f := range findings {
				fmt.Fprintf(cmd.OutOrStdout(), "%s: %s (rule=%s severity=%s)\n", f.Path, f.Message, f.Rule, f.Severity)
			}
			if strict && len(findings) > 0 {
				os.Exit(1)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&strict, "strict", false, "exit nonzero when findings exist")
	cmd.Flags().StringVar(&projectRoot, "project", "", "project root (default: cwd)")
	return cmd
}
