package cli

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/go-sql-driver/mysql" // registers the mysql driver
	"github.com/spf13/cobra"

	"github.com/GembaCore/gemba-core/internal/tenant"
	"github.com/GembaCore/gemba-core/internal/workspaces"
)

// newWorkspaceCmd builds the `gemba workspace` subcommand tree
// (gm-o9t8.2.4). Subcommands operate against the multi-tenant
// workspace registry — either via a direct Dolt SQL connection when
// --dolt-url is supplied, or against an ephemeral in-memory store
// (useful only for `--help` and dry runs; nothing persists).
//
// Wire shape mirrors the registry: list / create / delete. Update is
// intentionally out of scope for the foundation slice; archive +
// rename land in a follow-up.
func newWorkspaceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workspace",
		Short: "Manage the multi-tenant workspace registry (gm-o9t8.2.4)",
		Long: `Manage workspaces registered in the Gemba control plane.

Workspaces are keyed by (tenant_id, slug); the canonical wsid is
"<tenant-prefix>:<slug>". Legacy bare-slug wsids resolve against the
default tenant for backwards compatibility.

Without --dolt-url every subcommand runs against an ephemeral
in-memory registry; the commands stay useful for --help discovery but
do not persist state. Pass --dolt-url to manage the production
registry.`,
	}
	cmd.AddCommand(
		newWorkspaceListCmd(),
		newWorkspaceCreateCmd(),
		newWorkspaceDeleteCmd(),
	)
	return cmd
}

// workspaceRegistryFromFlags opens the registry backing this
// invocation. Returns (reg, closer, err). The closer must be invoked
// on the happy path; nil-safe when the registry is the in-memory
// fallback. The caller is expected to wire ctx into its registry
// operations.
func workspaceRegistryFromFlags(ctx context.Context, doltURL string) (workspaces.Registry, func(), error) {
	if strings.TrimSpace(doltURL) == "" {
		return workspaces.NewMemStore(), func() {}, nil
	}
	db, err := sql.Open("mysql", doltURL)
	if err != nil {
		return nil, nil, fmt.Errorf("open dolt: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, nil, fmt.Errorf("ping dolt: %w", err)
	}
	s := workspaces.NewSQLStore(db)
	if err := s.Migrate(ctx); err != nil {
		_ = db.Close()
		return nil, nil, fmt.Errorf("migrate workspaces: %w", err)
	}
	return s, func() { _ = db.Close() }, nil
}

func newWorkspaceListCmd() *cobra.Command {
	var (
		doltURL         string
		tenantID        string
		includeArchived bool
		limit           int
		format          string
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List registered workspaces",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			reg, closer, err := workspaceRegistryFromFlags(ctx, doltURL)
			if err != nil {
				return err
			}
			defer closer()
			ws, err := reg.List(ctx, workspaces.ListOpts{
				TenantID:        tenantID,
				IncludeArchived: includeArchived,
				Limit:           limit,
			})
			if err != nil {
				return err
			}
			return writeWorkspaceList(cmd.OutOrStdout(), ws, format)
		},
	}
	cmd.Flags().StringVar(&doltURL, "dolt-url", "", "Dolt SQL URL (mysql://user:pass@host:port/db); empty uses an in-memory registry")
	cmd.Flags().StringVar(&tenantID, "tenant", "", "filter to one tenant id (e.g. t-default)")
	cmd.Flags().BoolVar(&includeArchived, "include-archived", false, "include archived workspaces in the result")
	cmd.Flags().IntVar(&limit, "limit", 0, "max rows to return (0 = registry default)")
	cmd.Flags().StringVar(&format, "format", "table", "output format: table | json")
	return cmd
}

func newWorkspaceCreateCmd() *cobra.Command {
	var (
		doltURL        string
		tenantID       string
		slug           string
		projectPath    string
		workspacesRoot string
		egressTemplate string
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Register a new workspace",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(tenantID) == "" {
				return errors.New("--tenant is required")
			}
			if strings.TrimSpace(slug) == "" {
				return errors.New("--slug is required")
			}
			if strings.TrimSpace(projectPath) == "" {
				if root := strings.TrimSpace(workspacesRoot); root != "" {
					projectPath = filepath.Join(root, slug)
				} else {
					return errors.New("--project-path is required (or pass --workspaces-root to derive a default)")
				}
			}
			// Validate tenant id shape up front so the user sees a
			// clear error rather than the registry's translated form.
			if _, err := tenant.ParseID(tenantID); err != nil {
				return fmt.Errorf("invalid --tenant: %w", err)
			}
			ctx := cmd.Context()
			reg, closer, err := workspaceRegistryFromFlags(ctx, doltURL)
			if err != nil {
				return err
			}
			defer closer()
			ws, err := reg.Create(ctx, workspaces.CreateInput{
				TenantID:       tenantID,
				Slug:           slug,
				ProjectPath:    projectPath,
				EgressTemplate: egressTemplate,
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "registered workspace %s -> %s\n", ws.ID, ws.ProjectPath)
			return nil
		},
	}
	cmd.Flags().StringVar(&doltURL, "dolt-url", "", "Dolt SQL URL (mysql://user:pass@host:port/db); empty uses an in-memory registry")
	cmd.Flags().StringVar(&tenantID, "tenant", "", "tenant id (e.g. t-default or t-<8 alnum>)")
	cmd.Flags().StringVar(&slug, "slug", "", "per-tenant workspace slug (required)")
	cmd.Flags().StringVar(&projectPath, "project-path", "", "absolute path to the on-disk project (defaults to <workspaces-root>/<slug> when --workspaces-root is set)")
	cmd.Flags().StringVar(&workspacesRoot, "workspaces-root", "", "parent dir used to derive --project-path when it is omitted")
	cmd.Flags().StringVar(&egressTemplate, "egress-template", "", "egress template id (optional)")
	return cmd
}

func newWorkspaceDeleteCmd() *cobra.Command {
	var doltURL string
	cmd := &cobra.Command{
		Use:   "delete <wsid>",
		Short: "Delete a workspace by wsid",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			reg, closer, err := workspaceRegistryFromFlags(ctx, doltURL)
			if err != nil {
				return err
			}
			defer closer()
			if err := reg.Delete(ctx, args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "deleted workspace %s\n", args[0])
			return nil
		},
	}
	cmd.Flags().StringVar(&doltURL, "dolt-url", "", "Dolt SQL URL (mysql://user:pass@host:port/db); empty uses an in-memory registry")
	return cmd
}

// writeWorkspaceList renders the slice in the requested format.
// table is the default human-readable form; json emits one document
// per row for downstream tooling. Unknown formats fall through to
// table so a typo never breaks the command.
func writeWorkspaceList(w io.Writer, ws []*workspaces.Workspace, format string) error {
	switch format {
	case "json":
		enc := json.NewEncoder(w)
		for _, x := range ws {
			if err := enc.Encode(x); err != nil {
				return err
			}
		}
		return nil
	default:
		if len(ws) == 0 {
			fmt.Fprintln(w, "(no workspaces)")
			return nil
		}
		// Two-column-ish table; keeps the surface terminal-friendly
		// without dragging in a tabwriter dep we don't already use
		// in this file.
		fmt.Fprintln(w, "WSID\tTENANT\tPATH")
		for _, x := range ws {
			archived := ""
			if x.ArchivedAt != nil {
				archived = " (archived)"
			}
			fmt.Fprintf(w, "%s\t%s\t%s%s\n", x.ID, x.TenantID, x.ProjectPath, archived)
		}
		return nil
	}
}

// (sanity check the package compiles independently of cobra's lazy
// initialisation — wraps stdout for the JSON path during tests.)
var _ = os.Stdout
