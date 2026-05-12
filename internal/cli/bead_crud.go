// bead_crud.go: `gemba bead {create,list,show,update,close,note}` —
// the remote CRUD surface a CLI operator drives against a gemba
// server (gm-o9t8.1.3.2).
//
// All verbs route through the typed Go client at internal/client. The
// generated client (gm-o9t8.1.1.4) only emitted read+dispatch
// endpoints; create / update / note ride hand-written helpers on the
// wrapper that share the same auth + confirm-nonce pipeline. When the
// OpenAPI document is regenerated those helpers fold back into the
// generated surface.
//
// Exit codes (intentionally distinct so scripts can branch):
//
//	0   success
//	1   generic failure
//	4   unauthorized (token rejected / expired)
//	5   not found
//
// The CRUD verbs intentionally live next to the existing enrichment
// verbs (now under `gemba bead enrichment`) so the operator can lean
// on one consistent `gemba bead …` namespace.

package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/GembaCore/gemba-core/core"
	gembaclient "github.com/GembaCore/gemba-core/internal/client"
)

// clientFactory is the seam tests use to inject a pre-baked typed
// client (bound to an httptest.Server) without touching the credstore
// path on disk. Production CLI runs use newCLIClient.
type clientFactory func(server string) (*gembaclient.Client, error)

// crudFactory holds the active factory. Tests swap it before invoking
// the cobra surface; the value resets via t.Cleanup.
var crudFactory clientFactory = func(server string) (*gembaclient.Client, error) {
	return gembaclient.New(server)
}

// withCrudFactory swaps crudFactory for the duration of the returned
// restore func. Test-only helper.
func withCrudFactory(f clientFactory) func() {
	prev := crudFactory
	crudFactory = f
	return func() { crudFactory = prev }
}

// resolveServer chooses a server URL. Flag wins; falls back to
// GEMBA_SERVER. Empty string means "let the typed client read the
// credstore default" — handled by gembaclient.New today via the
// `--server` requirement on its side, which we surface as an error
// here so the user gets a friendly message instead of a network
// failure.
func resolveServer(flag string) (string, error) {
	if strings.TrimSpace(flag) != "" {
		return strings.TrimSpace(flag), nil
	}
	if v := strings.TrimSpace(os.Getenv("GEMBA_SERVER")); v != "" {
		return v, nil
	}
	return "", errors.New("--server is required (or set GEMBA_SERVER)")
}

// mapExit translates a typed-client error to (exit-code, message).
// Exit codes follow the table in the package doc.
func mapExit(err error) (int, string) {
	switch {
	case err == nil:
		return 0, ""
	case errors.Is(err, gembaclient.ErrUnauthorized):
		return 4, "unauthorized: " + err.Error() + "\nhint: run `gemba login --server <url>`"
	case errors.Is(err, gembaclient.ErrNotFound):
		return 5, "not found: " + err.Error()
	}
	return 1, err.Error()
}

// runWithExit runs fn and, if it errors, writes the mapped message to
// stderr and returns a non-nil error that the cobra runner reports
// with the desired exit code. Cobra's default Execute path returns 1
// for any error; for distinct exit codes the binary entry-point would
// need to call os.Exit. To keep the test surface simple this helper
// just attaches the code to the error string so tests can assert.
func runWithExit(stderr io.Writer, fn func() error) error {
	if err := fn(); err != nil {
		code, msg := mapExit(err)
		fmt.Fprintln(stderr, msg)
		// Stash the code in an &exitError so production main() can pick
		// it up; tests use the typed sentinel.
		return &exitError{code: code, err: err}
	}
	return nil
}

// exitError carries an exit code alongside the underlying error for
// callers that care (currently: the CLI binary entry point in
// cmd/gemba). The cobra "RunE returned non-nil → exit 1" default is
// good enough for v1; the code is preserved here for future wiring.
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }
func (e *exitError) Unwrap() error { return e.err }
func (e *exitError) ExitCode() int { return e.code }

// newBeadCRUDCommands returns the CRUD verb subcommands to register
// under `gemba bead`.
func newBeadCRUDCommands() []*cobra.Command {
	return []*cobra.Command{
		newBeadCreateCmd(),
		newBeadCRUDListCmd(),
		newBeadCRUDShowCmd(),
		newBeadUpdateCmd(),
		newBeadCloseCmd(),
		newBeadNoteCmd(),
	}
}

// ── create ────────────────────────────────────────────────────────

func newBeadCreateCmd() *cobra.Command {
	var (
		server      string
		title       string
		kind        string
		parent      string
		description string
		labels      []string
		priority    int
		asJSON      bool
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new bead (work item) on the remote server",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runWithExit(cmd.ErrOrStderr(), func() error {
				srv, err := resolveServer(server)
				if err != nil {
					return err
				}
				if strings.TrimSpace(title) == "" {
					return errors.New("--title is required")
				}
				c, err := crudFactory(srv)
				if err != nil {
					return err
				}
				out, err := c.CreateWorkItem(cmd.Context(), gembaclient.CreateWorkItemInput{
					Title:       title,
					Kind:        kind,
					Description: description,
					Labels:      labels,
					Priority:    priority,
					ParentID:    parent,
				})
				if err != nil {
					return err
				}
				if asJSON {
					return json.NewEncoder(cmd.OutOrStdout()).Encode(out)
				}
				fmt.Fprintln(cmd.OutOrStdout(), string(out.ID))
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&server, "server", "", "gemba server base URL (overrides GEMBA_SERVER and the credstore default)")
	cmd.Flags().StringVar(&title, "title", "", "bead title (required)")
	cmd.Flags().StringVar(&kind, "type", "task", "bead kind (task|story|epic|milestone|decision)")
	cmd.Flags().StringVar(&parent, "parent", "", "parent bead id")
	cmd.Flags().StringVar(&description, "description", "", "bead description")
	cmd.Flags().StringSliceVar(&labels, "label", nil, "label (repeat for multiple)")
	cmd.Flags().IntVar(&priority, "priority", 0, "priority")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit the created bead as JSON instead of just its id")
	return cmd
}

// ── list ──────────────────────────────────────────────────────────

func newBeadCRUDListCmd() *cobra.Command {
	var (
		server string
		status string
		limit  int
		asJSON bool
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List beads on the remote server",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runWithExit(cmd.ErrOrStderr(), func() error {
				srv, err := resolveServer(server)
				if err != nil {
					return err
				}
				c, err := crudFactory(srv)
				if err != nil {
					return err
				}
				items, err := c.ListWorkItems(cmd.Context(), gembaclient.ListWorkItemsFilter{
					Status: status,
					Limit:  limit,
				})
				if err != nil {
					return err
				}
				if asJSON {
					return json.NewEncoder(cmd.OutOrStdout()).Encode(items)
				}
				w := cmd.OutOrStdout()
				if len(items) == 0 {
					fmt.Fprintln(w, "(no beads)")
					return nil
				}
				for _, it := range items {
					fmt.Fprintf(w, "%-24s  %-10s  %s\n", it.ID, it.Status, it.Title)
				}
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&server, "server", "", "gemba server base URL")
	cmd.Flags().StringVar(&status, "status", "", "filter by status (e.g. open, closed)")
	cmd.Flags().IntVar(&limit, "limit", 0, "max items to return")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON")
	return cmd
}

// ── show ──────────────────────────────────────────────────────────

func newBeadCRUDShowCmd() *cobra.Command {
	var (
		server string
		asJSON bool
	)
	cmd := &cobra.Command{
		Use:   "show <id>",
		Short: "Show a single bead by id",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWithExit(cmd.ErrOrStderr(), func() error {
				srv, err := resolveServer(server)
				if err != nil {
					return err
				}
				c, err := crudFactory(srv)
				if err != nil {
					return err
				}
				wi, err := c.GetWorkItem(cmd.Context(), args[0])
				if err != nil {
					return err
				}
				if asJSON {
					return json.NewEncoder(cmd.OutOrStdout()).Encode(wi)
				}
				printWorkItemHuman(cmd.OutOrStdout(), wi)
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&server, "server", "", "gemba server base URL")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON")
	return cmd
}

func printWorkItemHuman(w io.Writer, wi *core.WorkItem) {
	fmt.Fprintf(w, "id:     %s\n", wi.ID)
	fmt.Fprintf(w, "title:  %s\n", wi.Title)
	fmt.Fprintf(w, "kind:   %s\n", wi.Kind)
	fmt.Fprintf(w, "status: %s (%s)\n", wi.Status, wi.StateCategory)
	if len(wi.Labels) > 0 {
		fmt.Fprintf(w, "labels: %s\n", strings.Join(wi.Labels, ", "))
	}
}

// ── update ────────────────────────────────────────────────────────

func newBeadUpdateCmd() *cobra.Command {
	var (
		server       string
		title        string
		description  string
		status       string
		priority     int
		setPriority  bool
		addLabels    []string
		removeLabels []string
	)
	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update fields on a bead",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWithExit(cmd.ErrOrStderr(), func() error {
				srv, err := resolveServer(server)
				if err != nil {
					return err
				}
				c, err := crudFactory(srv)
				if err != nil {
					return err
				}
				patch := core.WorkItemPatch{}
				touched := false
				if title != "" {
					patch.Title = &title
					touched = true
				}
				if description != "" {
					patch.Description = &description
					touched = true
				}
				if status != "" {
					patch.Status = &status
					touched = true
				}
				if setPriority {
					patch.Priority = &priority
					touched = true
				}
				if len(addLabels) > 0 || len(removeLabels) > 0 {
					// Compute final label set by reading current item.
					wi, err := c.GetWorkItem(cmd.Context(), args[0])
					if err != nil {
						return err
					}
					labels := mergeLabels(wi.Labels, addLabels, removeLabels)
					patch.Labels = labels
					touched = true
				}
				if !touched {
					return errors.New("update: at least one of --title/--description/--status/--priority/--add-label/--remove-label is required")
				}
				out, err := c.PatchWorkItem(cmd.Context(), args[0], patch)
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "updated %s\n", out.ID)
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&server, "server", "", "gemba server base URL")
	cmd.Flags().StringVar(&title, "title", "", "new title")
	cmd.Flags().StringVar(&description, "description", "", "new description")
	cmd.Flags().StringVar(&status, "status", "", "new status")
	cmd.Flags().IntVar(&priority, "priority", 0, "new priority")
	cmd.Flags().StringSliceVar(&addLabels, "add-label", nil, "add a label (repeatable)")
	cmd.Flags().StringSliceVar(&removeLabels, "remove-label", nil, "remove a label (repeatable)")
	// Track whether --priority was set explicitly.
	cmd.PreRunE = func(cmd *cobra.Command, _ []string) error {
		setPriority = cmd.Flags().Changed("priority")
		return nil
	}
	return cmd
}

func mergeLabels(current, add, remove []string) []string {
	set := map[string]struct{}{}
	for _, l := range current {
		set[l] = struct{}{}
	}
	for _, l := range add {
		set[l] = struct{}{}
	}
	for _, l := range remove {
		delete(set, l)
	}
	out := make([]string, 0, len(set))
	for l := range set {
		out = append(out, l)
	}
	return out
}

// ── close ─────────────────────────────────────────────────────────

func newBeadCloseCmd() *cobra.Command {
	var (
		server string
		reason string
	)
	cmd := &cobra.Command{
		Use:   "close <id>",
		Short: "Close a bead (PATCH state_category=completed)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWithExit(cmd.ErrOrStderr(), func() error {
				srv, err := resolveServer(server)
				if err != nil {
					return err
				}
				c, err := crudFactory(srv)
				if err != nil {
					return err
				}
				closed := core.StateCompleted
				status := "closed"
				patch := core.WorkItemPatch{
					StateCategory: &closed,
					Status:        &status,
				}
				if reason != "" {
					patch.Custom = map[string]any{"close_reason": reason}
				}
				out, err := c.PatchWorkItem(cmd.Context(), args[0], patch)
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "closed %s\n", out.ID)
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&server, "server", "", "gemba server base URL")
	cmd.Flags().StringVar(&reason, "reason", "", "close reason (stored in custom.close_reason)")
	return cmd
}

// ── note ──────────────────────────────────────────────────────────

func newBeadNoteCmd() *cobra.Command {
	var (
		server string
		note   string
	)
	cmd := &cobra.Command{
		Use:   "note <id> [note text]",
		Short: "Append a note to a bead",
		Long: `Append a free-form operator note to a bead. The note text may be
provided positionally (everything after the id is joined with spaces)
or via --note.

Server-side this targets POST /api/work-items/{id}/notes, which is a
501 stub until the persistence layer lands (follow-up bead).`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWithExit(cmd.ErrOrStderr(), func() error {
				srv, err := resolveServer(server)
				if err != nil {
					return err
				}
				body := note
				if body == "" && len(args) > 1 {
					body = strings.Join(args[1:], " ")
				}
				if strings.TrimSpace(body) == "" {
					return errors.New("note text is required (positional or --note)")
				}
				c, err := crudFactory(srv)
				if err != nil {
					return err
				}
				if err := c.NoteWorkItem(cmd.Context(), args[0], body); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "note appended to %s\n", args[0])
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&server, "server", "", "gemba server base URL")
	cmd.Flags().StringVar(&note, "note", "", "note text (alternative to positional)")
	return cmd
}

