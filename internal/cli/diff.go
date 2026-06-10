// diff.go: `gemba diff` — stream uncommitted changes from the remote
// workspace repo (gm-o9t8.1.6.2).
//
// The CLI is intentionally thin: open GET /api/v1/workspaces/{wsid}/diff
// and pipe the response body straight to stdout via io.Copy as it
// arrives. The server is the source of truth for what `git diff` would
// print; the operator never needs the workspace checked out locally.
//
// Exit codes match the bead-CRUD table:
//
//	0   success (including empty diff)
//	1   network / generic
//	4   unauthorized (401)
//	5   not found (404)
//
// Workspace resolution: explicit --workspace flag > GEMBA_WORKSPACE env
// var > "_" (the sentinel the server resolves to "the default workspace
// for the authenticated identity", matching the convention used by
// `gemba status`).

package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/GembaCore/gemba-core/internal/auth/credstore"
)

// diffHTTPClient is the seam tests use to inject a real httptest.Server-
// bound *http.Client. Production uses http.DefaultClient.
var diffHTTPClient = func() *http.Client { return http.DefaultClient }

// diffTokenLookup resolves the bearer token for server. Tests override
// this so they don't depend on the user's credstore. Returns the empty
// string and a nil error when no credential exists — the caller passes
// the request through unauthenticated and the server decides whether
// the route is reachable.
var diffTokenLookup = defaultDiffTokenLookup

func defaultDiffTokenLookup(server string) (string, error) {
	path, err := credstore.DefaultPath()
	if err != nil {
		return "", nil // best-effort; unauthenticated request follows
	}
	store, err := credstore.Load(path)
	if err != nil {
		return "", nil
	}
	cred, ok := store.Get(server)
	if !ok {
		return "", nil
	}
	return cred.Token, nil
}

func newDiffCmd() *cobra.Command {
	var (
		server    string
		workspace string
		staged    bool
	)
	cmd := &cobra.Command{
		Use:   "diff [-- path...]",
		Short: "Stream uncommitted changes from the remote workspace repo",
		Long: `Pipes the server-side 'git diff' output to stdout.

The workspace repo lives on the server; you don't need a local
checkout. Use --staged to see the index-vs-HEAD diff (equivalent to
'git diff --cached'). Trailing positional arguments after '--' are
forwarded as path filters.

Exit codes:
  0  success (empty diff is still 0)
  1  network or generic failure
  4  unauthorized
  5  workspace not found`,
		// Allow positional path filters after "--". Cobra parses
		// everything after "--" into args without flag interpretation.
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDiff(cmd, server, workspace, staged, args)
		},
	}
	cmd.Flags().StringVar(&server, "server", "", "gemba server base URL (overrides GEMBA_SERVER)")
	cmd.Flags().StringVar(&workspace, "workspace", "", "workspace id (defaults to GEMBA_WORKSPACE, then '_')")
	cmd.Flags().BoolVar(&staged, "staged", false, "show staged changes only (git diff --cached)")
	return cmd
}

func runDiff(cmd *cobra.Command, server, workspace string, staged bool, paths []string) error {
	return runWithExit(cmd.ErrOrStderr(), func() error {
		srv, err := resolveServer(server)
		if err != nil {
			return err
		}
		wsid := resolveDiffWorkspace(workspace)

		u, err := url.Parse(strings.TrimRight(srv, "/") + "/api/v1/workspaces/" + url.PathEscape(wsid) + "/diff")
		if err != nil {
			return fmt.Errorf("parse server url: %w", err)
		}
		q := u.Query()
		if staged {
			q.Set("staged", "true")
		}
		for _, p := range paths {
			if strings.TrimSpace(p) == "" {
				continue
			}
			q.Add("path", p)
		}
		u.RawQuery = q.Encode()

		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return err
		}
		req.Header.Set("Accept", "text/x-patch, text/plain, */*")
		if tok, terr := diffTokenLookup(srv); terr == nil && tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}

		resp, err := diffHTTPClient().Do(req)
		if err != nil {
			return fmt.Errorf("GET %s: %w", u.String(), err)
		}
		defer resp.Body.Close()

		switch resp.StatusCode {
		case http.StatusOK:
			// Stream straight to stdout. Empty body is fine.
			if _, err := io.Copy(cmd.OutOrStdout(), resp.Body); err != nil {
				return fmt.Errorf("stream diff: %w", err)
			}
			return nil
		case http.StatusUnauthorized:
			// Slurp the body for context in the error message but cap
			// the length so a misbehaving server can't OOM the CLI.
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			return &diffStatusErr{code: 4, msg: fmt.Sprintf("unauthorized: %s\nhint: run `gemba login --server %s`", strings.TrimSpace(string(body)), srv)}
		case http.StatusNotFound:
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			return &diffStatusErr{code: 5, msg: fmt.Sprintf("workspace not found: %s (%s)", wsid, strings.TrimSpace(string(body)))}
		default:
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			return fmt.Errorf("GET %s: %s\n%s", u.String(), resp.Status, strings.TrimSpace(string(body)))
		}
	})
}

// diffStatusErr carries a CLI exit code through runWithExit. mapExit
// in bead_crud.go already handles ErrUnauthorized/ErrNotFound, but the
// raw http path here doesn't surface those sentinels — this thin
// wrapper plays the same role.
type diffStatusErr struct {
	code int
	msg  string
}

func (e *diffStatusErr) Error() string { return e.msg }
func (e *diffStatusErr) ExitCode() int { return e.code }

// resolveDiffWorkspace picks the workspace id from flag → env → sentinel.
func resolveDiffWorkspace(flag string) string {
	if strings.TrimSpace(flag) != "" {
		return strings.TrimSpace(flag)
	}
	if env := os.Getenv("GEMBA_WORKSPACE"); strings.TrimSpace(env) != "" {
		return strings.TrimSpace(env)
	}
	return "_"
}

// Compile-time check that errors.Is still maps cleanly through our
// shim wrapper above (mapExit uses errors.Is on the typed client
// sentinels; diffStatusErr predates them and carries its own code).
var _ = errors.Is
