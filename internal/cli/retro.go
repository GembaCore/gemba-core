// gm-s47n.8.4 — `gemba retro` CLI.
//
// Queryable view over the scorer_grades table. Two modes:
//
//   gemba retro list --dolt-url URL [--diverged-gt N] [--session ID]
//     Connects directly to a Dolt instance and runs the planner's
//     retro.Store.List query.
//
//   gemba retro list --file grades.json
//     Reads a JSON envelope of {grades: [Grade...]} and applies
//     the same in-memory filter. Useful for tests, post-mortem
//     analysis, and pipelines that don't have a live Dolt.
//
// Default output is a per-row text card; --json emits the stable
// envelope.

package cli

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	// Register the MySQL driver so sql.Open("mysql", ...) works
	// without the caller having to import it explicitly. Keeping
	// the dependency local to the CLI surface mirrors the dolt
	// adaptor's pattern.
	_ "github.com/go-sql-driver/mysql"
	"github.com/spf13/cobra"

	"github.com/MikeBengtson/gemba/internal/planner/retro"
)

// RetroListInput is the offline-mode wire shape for `gemba retro list`.
// Grades is the input set; ListFilter narrows it the same way the
// dolt-mode SQL filter would.
type RetroListInput struct {
	Grades []retro.Grade `json:"grades"`
}

// RetroListOut is the stable --json envelope.
type RetroListOut struct {
	Rows []retro.Grade `json:"rows"`
}

func newRetroCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "retro",
		Short: "Inspect turn-retrospective grades (gm-s47n.8)",
		Long: `Read scorer_grades rows produced by the bead-close retrospective
(gm-s47n.8.3). The retro pipeline compares declared targets/concepts
to actuals on bead close; this command surfaces those records for
operator review per spec §7.4.`,
	}
	cmd.AddCommand(newRetroListCmd())
	return cmd
}

func newRetroListCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List retrospective grades, optionally filtered by divergence",
		Long: `List scorer_grades rows newest-first. Supports threshold
filtering ("show me beads where actual targets diverged > 50% from
declared" — spec §7.4) and per-session filtering.

Modes:
  --dolt-url URL    Query a Dolt instance directly.
  --file PATH       Read a {grades: [...]} JSON envelope.

Filters:
  --diverged-gt N   Keep rows where target_divergence > N (the
                    classic spec example uses 0.5).
  --concept-gt N    Keep rows where concept_divergence > N.
  --session ID      Keep rows for this session id.
  --limit N         Cap result count (default 50; 0 = use server cap).
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRetroList(cmd, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-parseable JSON instead of text")
	cmd.Flags().String("file", "", "read a {grades:[...]} JSON envelope (offline mode)")
	cmd.Flags().String("dolt-url", "", "Dolt URL (e.g. mysql://root@127.0.0.1:3307/gemba)")
	cmd.Flags().Float64("diverged-gt", 0, "keep rows with target_divergence > N (default 0 = no filter)")
	cmd.Flags().Float64("concept-gt", 0, "keep rows with concept_divergence > N (default 0 = no filter)")
	cmd.Flags().String("session", "", "filter to this session id")
	cmd.Flags().Int("limit", 50, "max rows to return (0 = up to the server cap)")
	return cmd
}

func runRetroList(cmd *cobra.Command, asJSON bool) error {
	out := cmd.OutOrStdout()
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	file, _ := cmd.Flags().GetString("file")
	doltURL, _ := cmd.Flags().GetString("dolt-url")
	if file != "" && doltURL != "" {
		return fmt.Errorf("--file and --dolt-url are mutually exclusive")
	}
	if file == "" && doltURL == "" {
		// Default to stdin when neither is set — matches every other
		// envelope-reading command in this CLI.
		file = "-"
	}

	tgtThresh, _ := cmd.Flags().GetFloat64("diverged-gt")
	cptThresh, _ := cmd.Flags().GetFloat64("concept-gt")
	sessionID, _ := cmd.Flags().GetString("session")
	limit, _ := cmd.Flags().GetInt("limit")

	var rows []retro.Grade
	if doltURL != "" {
		var err error
		rows, err = listFromDolt(ctx, doltURL, tgtThresh, cptThresh, sessionID, limit)
		if err != nil {
			return err
		}
	} else {
		var err error
		rows, err = listFromFile(cmd, file, tgtThresh, cptThresh, sessionID, limit)
		if err != nil {
			return err
		}
	}

	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(RetroListOut{Rows: rows})
	}
	return printRetroText(out, rows)
}

func listFromDolt(ctx context.Context, doltURL string, tgtThresh, cptThresh float64, sessionID string, limit int) ([]retro.Grade, error) {
	db, err := openDoltForRetro(doltURL)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	store := retro.NewStore(db)
	return store.List(ctx, retro.ListFilter{
		MinTargetDivergence:  tgtThresh,
		MinConceptDivergence: cptThresh,
		SessionID:            sessionID,
		Limit:                limit,
	})
}

// openDoltForRetro mirrors internal/adapter/dolt's open path but
// without dragging the WorkPlane's full lifecycle through. The
// query-only retro CLI doesn't need worktree pools, schema migration,
// or capability registration — just a SQL connection.
func openDoltForRetro(rawURL string) (*sql.DB, error) {
	dsn, err := doltDSN(rawURL)
	if err != nil {
		return nil, err
	}
	return sql.Open("mysql", dsn)
}

// doltDSN converts a `mysql://user[:pass]@host/dbname?params` URL
// into the go-sql-driver DSN form `user:pass@tcp(host)/dbname?params`.
// Mirrors internal/adapter/dolt/parseDoltURL but is duplicated here
// rather than exported so the CLI surface doesn't depend on the
// dolt adaptor package (which carries WorkPlane, schemas, and a
// non-trivial init cost).
func doltDSN(rawURL string) (string, error) {
	rawURL = strings.TrimPrefix(rawURL, "mysql://")
	if rawURL == "" {
		return "", fmt.Errorf("empty dolt URL")
	}
	at := strings.Index(rawURL, "@")
	var user, hostAndDB string
	if at < 0 {
		user = "root"
		hostAndDB = rawURL
	} else {
		user = rawURL[:at]
		hostAndDB = rawURL[at+1:]
	}
	slash := strings.Index(hostAndDB, "/")
	if slash < 0 {
		return "", fmt.Errorf("dolt URL missing /dbname")
	}
	host := hostAndDB[:slash]
	dbAndParams := hostAndDB[slash+1:]
	return fmt.Sprintf("%s@tcp(%s)/%s", user, host, dbAndParams), nil
}

func listFromFile(cmd *cobra.Command, path string, tgtThresh, cptThresh float64, sessionID string, limit int) ([]retro.Grade, error) {
	var rd io.Reader = cmd.InOrStdin()
	if path != "-" {
		f, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", path, err)
		}
		defer f.Close()
		rd = f
	}
	var in RetroListInput
	dec := json.NewDecoder(rd)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return nil, fmt.Errorf("decode retro list input: %w", err)
	}
	rows := filterGrades(in.Grades, tgtThresh, cptThresh, sessionID)
	// Newest first per spec §7.4 ("show me the LATEST diverged
	// beads"). The dolt-mode query already handles this; offline
	// mode mirrors it.
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].ClosedAt.After(rows[j].ClosedAt) })
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	return rows, nil
}

func filterGrades(in []retro.Grade, tgtThresh, cptThresh float64, sessionID string) []retro.Grade {
	out := make([]retro.Grade, 0, len(in))
	for _, g := range in {
		if tgtThresh > 0 && g.Diff.TargetDivergence < tgtThresh {
			continue
		}
		if cptThresh > 0 && g.Diff.ConceptDivergence < cptThresh {
			continue
		}
		if sessionID != "" && g.SessionID != sessionID {
			continue
		}
		out = append(out, g)
	}
	return out
}

func printRetroText(out io.Writer, rows []retro.Grade) error {
	if len(rows) == 0 {
		fmt.Fprintln(out, "no retrospective grades match the filter")
		return nil
	}
	fmt.Fprintln(out, "bead              session         t-div  c-div  closed")
	for _, r := range rows {
		fmt.Fprintf(out, "%-17s %-15s %5.2f  %5.2f  %s\n",
			string(r.BeadID),
			r.SessionID,
			r.Diff.TargetDivergence,
			r.Diff.ConceptDivergence,
			r.ClosedAt.Format("2006-01-02T15:04Z07:00"),
		)
		if len(r.Diff.UndeclaredActual) > 0 {
			fmt.Fprintf(out, "    + undeclared: %s\n", strings.Join(r.Diff.UndeclaredActual, ", "))
		}
		if len(r.Diff.UnmatchedDeclared) > 0 {
			parts := make([]string, len(r.Diff.UnmatchedDeclared))
			for i, p := range r.Diff.UnmatchedDeclared {
				parts[i] = string(p)
			}
			fmt.Fprintf(out, "    - over-declared: %s\n", strings.Join(parts, ", "))
		}
		if len(r.Diff.NewConcepts) > 0 {
			parts := make([]string, len(r.Diff.NewConcepts))
			for i, c := range r.Diff.NewConcepts {
				parts[i] = string(c)
			}
			fmt.Fprintf(out, "    + new concepts: %s\n", strings.Join(parts, ", "))
		}
	}
	return nil
}
