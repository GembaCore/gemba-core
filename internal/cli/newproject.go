// gemba newproject — terminal interactive mode for the Onboarder persona
// (gm-1t4z).
//
// Drops into a stdin/stdout REPL that runs the newproject skill against the
// same atomic ratify transaction the HTTP layer uses.  ssh-only operators can
// bootstrap a project without a browser.
//
// Entry point: newNewProjectCmd().
// REPL commands (prefix with ':'):
//   :ratify   — trigger the atomic ratification transaction (prompts for
//               confirmation before committing).
//   :quit     — discard the conversation and exit 0.
//   :help     — print the available REPL commands.
//
// On entry the command:
//  1. Probes the LLM client via onboarder.DefaultResolver; prints the
//     canonical diagnostic and exits non-zero on ErrNoLLMClient.
//  2. Prints the persona's greeting.
//  3. Loops: read a line from stdin → call Persona.Turn → print reply +
//     plan-tree summary.
//  4. :ratify confirms with a plan summary then calls
//     server.NewRatifier.Ratify with the current state.

package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/MikeBengtson/gemba/internal/personas/onboarder"
	"github.com/MikeBengtson/gemba/internal/server"
	"github.com/MikeBengtson/gemba/internal/skills/newproject"
)

func newNewProjectCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "newproject",
		Short: "Start a conversational new-project session (terminal interactive mode)",
		Long: `Drops into a stdin/stdout REPL bound to an Onboarder persona.

The Onboarder guides you through naming milestones, epics, and beads for a new
project via a natural-language conversation, then commits everything atomically
with the same transaction the browser UI uses.

REPL commands (type on their own line):
  :ratify   prompt for confirmation then commit the current plan tree
  :quit     discard the session and exit
  :help     show this command list

On entry the command probes the configured LLM client. If no client is
configured it prints the canonical diagnostic and exits non-zero.

LLM credentials are read from ~/.gemba/config.toml [llm]:

  [llm]
  provider = "anthropic"
  api_key  = "sk-..."             # or set ANTHROPIC_API_KEY

Use --config to override the config-file path.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNewProject(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout(), configPath)
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "",
		"path to gemba config.toml (default: ~/.gemba/config.toml)")

	return cmd
}

// newProjectErrNoClient is re-exported from onboarder so tests can check for
// it without importing the onboarder package. It is the same sentinel.
var newProjectErrNoClient = onboarder.ErrNoLLMClient

// runNewProject is the production entry point for the REPL. Resolves the LLM
// client via DefaultResolver then delegates to runNewProjectWithRatifier.
func runNewProject(ctx context.Context, in io.Reader, out io.Writer, configPath string) error {
	// ── Step 1: probe LLM client ───────────────────────────────────────
	resolver := onboarder.DefaultResolver(configPath)
	persona, err := onboarder.Spawn(ctx, resolver)
	if err != nil {
		if onboarder.IsNoClient(err) {
			fmt.Fprintln(out, "error:", onboarder.MissingClientDiagnostic)
			return fmt.Errorf("newproject: %w", err)
		}
		return fmt.Errorf("newproject: spawn onboarder: %w", err)
	}
	defer persona.Discard()

	ratifier := server.NewRatifier(server.RatifierConfig{})
	return runREPL(ctx, in, out, persona, ratifier)
}

// runNewProjectWithRatifier is the fully-injectable entry point used by tests.
// It bypasses the resolver entirely — the caller provides a pre-built
// LLMClient and a pre-built ratifier so the REPL loop is deterministic.
func runNewProjectWithRatifier(ctx context.Context, in io.Reader, out io.Writer, client newproject.LLMClient, rat server.NewProjectRatifier) error {
	persona, err := onboarder.SpawnWithClient(client)
	if err != nil {
		return fmt.Errorf("newproject: spawn with client: %w", err)
	}
	defer persona.Discard()
	return runREPL(ctx, in, out, persona, rat)
}

// runREPL is the REPL loop shared by the production and test paths.
func runREPL(ctx context.Context, in io.Reader, out io.Writer, persona *onboarder.Persona, ratifier server.NewProjectRatifier) error {
	// ── Step 2: print greeting ─────────────────────────────────────────
	fmt.Fprintln(out, persona.Greeting())
	fmt.Fprintln(out)

	// ── Step 3: REPL ──────────────────────────────────────────────────
	// One scanner owns the buffered reader so :ratify's confirm prompt
	// and the outer REPL loop share the same read position.
	state := newproject.EmptyState()
	scanner := bufio.NewScanner(in)

	printPrompt(out)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		switch {
		case line == ":help":
			printHelp(out)

		case line == ":quit":
			fmt.Fprintln(out, "Session discarded.")
			return nil

		case line == ":ratify":
			if err := runRatify(ctx, out, scanner, ratifier, state); err != nil {
				// A *server.RatifyError is a step failure; surface it and
				// let the operator decide to fix + retry or quit.
				var re *server.RatifyError
				if errors.As(err, &re) {
					fmt.Fprintf(out, "ratify failed (step %d, %s): %s\n", re.Step, re.Code, re.Message)
					if re.Cause != nil {
						fmt.Fprintf(out, "  cause: %v\n", re.Cause)
					}
				} else if errors.Is(err, errRatifyAborted) {
					fmt.Fprintln(out, "Ratify cancelled.")
				} else {
					fmt.Fprintf(out, "ratify error: %v\n", err)
				}
			}
			// On success runRatify already printed the confirmation line;
			// control falls through to the prompt naturally.

		case line == "":
			// blank line — ignore, just re-prompt

		default:
			// Conversational turn.
			result, err := persona.Turn(ctx, state, line)
			if err != nil {
				fmt.Fprintf(out, "error: %v\n", err)
			} else {
				state = result.State
				fmt.Fprintln(out)
				fmt.Fprintln(out, result.Reply)
				fmt.Fprintln(out)
				printPlanSummary(out, state)
			}
		}

		printPrompt(out)
	}

	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("newproject: read stdin: %w", err)
	}
	return nil
}

// errRatifyAborted is the sentinel for "operator said no" at the
// confirmation prompt — distinct from an actual ratify step failure.
var errRatifyAborted = errors.New("ratify aborted by operator")

// runRatify shows the plan summary, prompts for confirmation, then calls the
// ratifier. The scanner is the REPL's shared scanner so the confirmation read
// is in sync with the rest of the line reads.
//
// Returns errRatifyAborted when the operator declines, or a *server.RatifyError
// on transaction failure.
func runRatify(ctx context.Context, out io.Writer, scanner *bufio.Scanner, ratifier server.NewProjectRatifier, state newproject.NewProjectState) error {
	if state.ProjectName == "" {
		fmt.Fprintln(out, "No project name yet — continue the conversation until the plan is ready.")
		return errRatifyAborted
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, "--- Plan to commit ---")
	printPlanSummary(out, state)
	fmt.Fprintln(out, "----------------------")
	fmt.Fprintf(out, "Commit project %q? [y/N] ", state.ProjectName)

	if !scanner.Scan() {
		return errRatifyAborted
	}
	answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
	if answer != "y" && answer != "yes" {
		return errRatifyAborted
	}

	resp, err := ratifier.Ratify(ctx, state)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "\nProject created: %s\n", resp.ProjectPath)
	return nil
}

// printPlanSummary writes a compact tree of the current state to out.
// Format:
//
//	Project: <name>
//	  milestone 0: <title> (<N> epics, <M> beads)
//	    epic 0: <title> (<K> beads)
//	    ...
func printPlanSummary(out io.Writer, state newproject.NewProjectState) {
	if state.ProjectName == "" && len(state.Milestones) == 0 {
		fmt.Fprintln(out, "[no plan yet]")
		return
	}
	if state.ProjectName != "" {
		fmt.Fprintf(out, "Project: %s\n", state.ProjectName)
	}
	for mi, m := range state.Milestones {
		totalBeads := 0
		for _, e := range m.Epics {
			totalBeads += len(e.Beads)
		}
		fmt.Fprintf(out, "  milestone %d: %s (%d epic(s), %d bead(s))\n",
			mi, m.Title, len(m.Epics), totalBeads)
		for ei, e := range m.Epics {
			fmt.Fprintf(out, "    epic %d: %s (%d bead(s))\n",
				ei, e.Title, len(e.Beads))
		}
	}
}

// printPrompt writes the readline-style input prompt.
func printPrompt(out io.Writer) {
	fmt.Fprint(out, "> ")
}

// printHelp writes the REPL command reference.
func printHelp(out io.Writer) {
	fmt.Fprintln(out, "REPL commands:")
	fmt.Fprintln(out, "  :ratify   commit the current plan tree (prompts for confirmation)")
	fmt.Fprintln(out, "  :quit     discard session and exit")
	fmt.Fprintln(out, "  :help     show this help")
	fmt.Fprintln(out, "Any other input is sent to the Onboarder as a conversational message.")
}
