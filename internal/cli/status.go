// status.go: `gemba status` (and bare `gemba`) workspace dashboard
// (gm-o9t8.1.6.4).
//
// `gemba` invoked with no args is equivalent to `gemba status` per the
// locked verb taxonomy in gm-o9t8.5. The mode-signaling spec in
// gm-o9t8.8 defines four output templates (Ad-hoc / Scaffolded /
// Governed / Reverse-governed); only Ad-hoc is implemented here. The
// other three depend on ASDD spec.md + .lockfile.json machinery from
// epic gm-v0sp which isn't built yet. The mode-detection helper is
// structured as detectMode(root) so the additional templates slot in
// without refactoring the command.
//
// Data path (v1, pragmatic):
//   1. Try GET /api/v1/workspaces/{wsid}/status (gm-o9t8.1.4.4). On a
//      200 we drive the dashboard's counts + mode + repo banner off
//      that response and only fetch the Ready / In Flight item lists
//      from /api/work-items (the status endpoint returns counts, not
//      bead rows, so the renderer still needs the rows for its tables).
//   2. Fallback for legacy servers: if the status endpoint returns 501
//      (older server with the stub) or 404 (wsid not registered), or
//      we hit a network error talking to it, the CLI falls back to
//      assembling everything from /api/work-items?status=... lists.
//
// Flags:
//
//   --server URL  target server (overrides GEMBA_SERVER)
//   --json        emit raw JSON (server response + computed mode)
//   --mode        print just the mode word, nothing else
//                 (useful for scripting / shell prompts)

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/GembaCore/gemba-core/core"
	gembaclient "github.com/GembaCore/gemba-core/internal/client"
)

// detectMode returns the workspace mode word for the dashboard
// template selector. Stubbed to always return "ad-hoc" for v1.
//
// Full mode detection requires reading specs/*/spec.md frontmatter
// plus .lockfile.json to distinguish:
//   - "ad-hoc"            : no spec / no lockfile
//   - "scaffolded"        : spec present, no lockfile
//   - "governed"          : spec + lockfile, code conforms
//   - "reverse-governed"  : lockfile derived from code
//
// Neither spec.md nor .lockfile.json exist on disk yet — they're
// scheduled under ASDD epic gm-v0sp. When that lands, expand this
// function (and the matching case in renderStatus) rather than
// changing the call site.
func detectMode(workspaceRoot string) string {
	// Currently a no-op; reserved arg so the signature is stable
	// for future expansion (mode detection will read files under
	// workspaceRoot).
	_ = workspaceRoot
	return "ad-hoc"
}

// newStatusCmd returns the `gemba status` subcommand.
func newStatusCmd() *cobra.Command {
	var (
		server   string
		asJSON   bool
		modeOnly bool
	)
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show the workspace dashboard (ready work, in-flight, counts)",
		Long: `Render the workspace dashboard for the connected gemba server.
This is the same surface as bare 'gemba' (no args).

Modes (ad-hoc / scaffolded / governed / reverse-governed) come from
gm-o9t8.8. Only ad-hoc is implemented in v1; the others require the
ASDD spec/lockfile machinery from epic gm-v0sp.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runStatus(cmd, server, asJSON, modeOnly)
		},
	}
	cmd.Flags().StringVar(&server, "server", "", "gemba server base URL (overrides GEMBA_SERVER)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON (raw server response + computed mode)")
	cmd.Flags().BoolVar(&modeOnly, "mode", false, "print just the workspace mode word")
	return cmd
}

// statusPayload is the unified shape the renderer consumes. Populated
// either from the real /workspaces/{wsid}/status response (when it
// exists) or assembled client-side from list endpoints (v1 fallback).
type statusPayload struct {
	Workspace   string          `json:"workspace"`
	Server      string          `json:"server"`
	Mode        string          `json:"mode"`
	Ready       []core.WorkItem `json:"ready"`
	InFlight    []core.WorkItem `json:"in_flight"`
	OpenTotal   int             `json:"open_total"`
	ClosedToday int             `json:"closed_today"`
	CacheAge    time.Duration   `json:"cache_age_ns"`
	// Repo + Agents + EmbeddedDolt are populated only when the
	// primary status endpoint is available. Nil / zero in the
	// fallback path.
	Repo         *gembaclient.WorkspaceRepoStatus  `json:"repo,omitempty"`
	Agents       *gembaclient.WorkspaceAgentStatus `json:"agents,omitempty"`
	EmbeddedDolt *gembaclient.EmbeddedDoltStatus   `json:"embedded_dolt,omitempty"`
}

func runStatus(cmd *cobra.Command, server string, asJSON, modeOnly bool) error {
	srv, err := resolveServer(server)
	if err != nil {
		return err
	}
	c, err := crudFactory(srv)
	if err != nil {
		return err
	}
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	payload, err := fetchStatus(ctx, c, srv)
	if err != nil {
		return err
	}

	// Mode detection happens client-side. workspaceRoot is cwd for
	// now; future versions may pull it from --workspace or config.
	root, _ := os.Getwd()
	payload.Mode = detectMode(root)
	if payload.Workspace == "" {
		payload.Workspace = filepath.Base(root)
	}
	if payload.Server == "" {
		payload.Server = srv
	}

	if modeOnly {
		fmt.Fprintln(cmd.OutOrStdout(), payload.Mode)
		return nil
	}
	if asJSON {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(payload)
	}
	renderStatus(cmd.OutOrStdout(), payload)
	return nil
}

// fetchStatus tries the workspace status endpoint first; if the
// server returns the real envelope we drive counts + mode + repo off
// that response and only fetch Ready / In Flight item rows (which the
// status endpoint doesn't carry). On a 501 / 404 we fall back to
// assembling everything from /api/work-items. A 401 from the primary
// surfaces immediately so the caller can render a "not logged in"
// hint; transport errors against the primary also propagate so an
// unreachable server isn't masked by a silent fallback.
func fetchStatus(ctx context.Context, c *gembaclient.Client, srv string) (statusPayload, error) {
	_ = srv
	resp, ok, err := tryWorkspaceStatus(ctx, c)
	if err != nil {
		return statusPayload{}, err
	}
	if ok {
		// Primary path: status endpoint returned 200. Fetch the item
		// rows the dashboard renders — counts come from the envelope.
		ready, err := c.ListWorkItemsByStatus(ctx, "ready")
		if err != nil {
			return statusPayload{}, err
		}
		inflight, err := c.ListWorkItemsByStatus(ctx, "in_progress")
		if err != nil {
			return statusPayload{}, err
		}
		return statusPayload{
			Workspace:    resp.WorkspaceID,
			Mode:         resp.Mode,
			Ready:        ready,
			InFlight:     inflight,
			OpenTotal:    resp.Beads.OpenTotal,
			ClosedToday:  resp.Beads.ClosedToday,
			Repo:         resp.Repo,
			Agents:       resp.Agents,
			EmbeddedDolt: resp.EmbeddedDolt,
		}, nil
	}
	// Fallback (501 / 404 from primary): list-by-status + compute counts.
	ready, err := c.ListWorkItemsByStatus(ctx, "ready")
	if err != nil {
		return statusPayload{}, err
	}
	inflight, err := c.ListWorkItemsByStatus(ctx, "in_progress")
	if err != nil {
		return statusPayload{}, err
	}
	// Open total is a best-effort: sum of unstarted + started lanes.
	// A real status endpoint will compute this server-side with
	// state-category counts; until then we use a single open-status
	// list call.
	openItems, err := c.ListWorkItemsByStatus(ctx, "open")
	if err != nil {
		// Some adaptors won't recognize "open" as a status; that's
		// fine — fall back to ready+in_progress as the floor.
		openItems = make([]core.WorkItem, 0, len(ready)+len(inflight))
		openItems = append(openItems, ready...)
		openItems = append(openItems, inflight...)
	}
	closedToday := 0
	if closed, err := c.ListWorkItemsByStatus(ctx, "closed"); err == nil {
		cutoff := time.Now().Add(-24 * time.Hour)
		for _, wi := range closed {
			if wi.UpdatedAt.After(cutoff) {
				closedToday++
			}
		}
	}
	return statusPayload{
		Ready:       ready,
		InFlight:    inflight,
		OpenTotal:   len(openItems),
		ClosedToday: closedToday,
	}, nil
}

// tryWorkspaceStatus probes /api/v1/workspaces/_/status through the
// typed client (which injects the bearer token from the credstore).
// The special wsid "_" tells the server to resolve the active
// workspace for the authenticated user.
//
// Returns (resp, ok, err) where:
//   - ok=true:  server returned the 200 envelope; use it
//   - ok=false, err=nil: graceful fallback signal — the endpoint
//     returned 501 (older server with the stub) or 404 (wsid not
//     registered yet on this server)
//   - err != nil: real error (401 unauthorized, network, decode).
//     Propagated to the caller so a 401 / unreachable server isn't
//     masked by the legacy list fallback.
func tryWorkspaceStatus(ctx context.Context, c *gembaclient.Client) (*gembaclient.WorkspaceStatusResponse, bool, error) {
	resp, err := c.WorkspaceStatus(ctx, "_")
	if err == nil {
		return resp, true, nil
	}
	// 501 / 404 → graceful fallback. Everything else (401, network,
	// 5xx) propagates so the caller can surface a real error.
	if errors.Is(err, gembaclient.ErrNotImplemented) || errors.Is(err, gembaclient.ErrNotFound) {
		return nil, false, nil
	}
	return nil, false, err
}

// renderStatus writes the Ad-hoc dashboard template (gm-o9t8.8) to w.
// Other mode templates would branch here once they're implemented.
func renderStatus(w io.Writer, p statusPayload) {
	switch p.Mode {
	case "ad-hoc", "":
		renderAdHoc(w, p)
	default:
		// Future: scaffolded / governed / reverse-governed templates
		// land here. For v1 anything non-ad-hoc falls through to the
		// ad-hoc shape so we never blow up on unexpected modes.
		renderAdHoc(w, p)
	}
}

// renderAdHoc prints the dashboard described in gm-o9t8.8 §Ad-hoc.
func renderAdHoc(w io.Writer, p statusPayload) {
	ws := p.Workspace
	if ws == "" {
		ws = "(unknown)"
	}
	srv := p.Server
	if srv == "" {
		srv = "(unknown)"
	}
	cache := ""
	if p.CacheAge > 0 {
		cache = fmt.Sprintf(" (cached %s ago)", humanDuration(p.CacheAge))
	}
	fmt.Fprintf(w, "gemba · workspace: %s · server: %s%s\n", ws, srv, cache)

	// Repo banner — only present when the new /workspaces/{wsid}/
	// status endpoint is in play and the workspace has a clone.
	if p.Repo != nil && p.Repo.Head != "" {
		head := p.Repo.Head
		if len(head) > 12 {
			head = head[:12]
		}
		dirty := ""
		if p.Repo.Dirty {
			dirty = " · dirty"
		}
		branch := p.Repo.Branch
		if branch == "" {
			branch = "(detached)"
		}
		fmt.Fprintf(w, "repo · %s @ %s%s\n", branch, head, dirty)
	}
	// Embedded-dolt degraded-state banner (gm-o9t8.1.9). Only shown
	// when the supervisor is wired AND not in the steady "ready" state
	// — otherwise the banner would scream on every dashboard render.
	if p.EmbeddedDolt != nil && p.EmbeddedDolt.Enabled && p.EmbeddedDolt.State != "ready" {
		msg := fmt.Sprintf("⚠ embedded-dolt: %s", p.EmbeddedDolt.State)
		if p.EmbeddedDolt.RestartCount > 0 {
			noun := "restarts"
			if p.EmbeddedDolt.RestartCount == 1 {
				noun = "restart"
			}
			msg += fmt.Sprintf(" (%d %s)", p.EmbeddedDolt.RestartCount, noun)
		}
		if p.EmbeddedDolt.LastError != "" {
			msg += " · " + p.EmbeddedDolt.LastError
		}
		fmt.Fprintln(w, msg)
	}

	// Agents banner — only when we have a run on record.
	if p.Agents != nil && (p.Agents.Active > 0 || p.Agents.LastRunID != "") {
		switch {
		case p.Agents.LastRunID != "":
			summary := p.Agents.LastRunSummary
			if summary == "" {
				summary = p.Agents.LastRunStatus
			}
			fmt.Fprintf(w, "agents · %d active · last run %s (%s)\n",
				p.Agents.Active, p.Agents.LastRunID, summary)
		default:
			fmt.Fprintf(w, "agents · %d active\n", p.Agents.Active)
		}
	}
	fmt.Fprintln(w)

	if len(p.Ready) == 0 && len(p.InFlight) == 0 && p.OpenTotal == 0 {
		fmt.Fprintln(w, "No beads yet. Try `gemba bead create` to get started.")
		return
	}

	// Ready section.
	fmt.Fprintln(w, "Ready:")
	if len(p.Ready) == 0 {
		fmt.Fprintln(w, "  (none)")
	} else {
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		for _, wi := range sortByPriority(p.Ready) {
			fmt.Fprintf(tw, "  ○\t%s\t%s\t%s\n",
				string(wi.ID), priorityBadge(wi.Priority), wi.Title)
		}
		_ = tw.Flush()
	}
	fmt.Fprintln(w)

	// In flight section.
	fmt.Fprintln(w, "In flight:")
	if len(p.InFlight) == 0 {
		fmt.Fprintln(w, "  (none)")
	} else {
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		for _, wi := range sortByPriority(p.InFlight) {
			owner := ""
			if wi.Owner != nil && wi.Owner.ID != "" {
				owner = string(wi.Owner.ID)
			} else if wi.Assignee != nil && wi.Assignee.ID != "" {
				owner = string(wi.Assignee.ID)
			}
			age := humanDuration(time.Since(wi.UpdatedAt))
			suffix := ""
			switch {
			case owner != "" && !wi.UpdatedAt.IsZero():
				suffix = fmt.Sprintf("  (%s, %s)", owner, age)
			case owner != "":
				suffix = fmt.Sprintf("  (%s)", owner)
			case !wi.UpdatedAt.IsZero():
				suffix = fmt.Sprintf("  (%s)", age)
			}
			fmt.Fprintf(tw, "  ◐\t%s\t%s\t%s%s\n",
				string(wi.ID), priorityBadge(wi.Priority), wi.Title, suffix)
		}
		_ = tw.Flush()
	}
	fmt.Fprintln(w)

	fmt.Fprintf(w, "%d open total · %d closed today\n", p.OpenTotal, p.ClosedToday)
}

// sortByPriority returns items sorted by priority ascending (P0 first;
// nil priority last) with title as a tiebreak.
func sortByPriority(items []core.WorkItem) []core.WorkItem {
	out := make([]core.WorkItem, len(items))
	copy(out, items)
	sort.SliceStable(out, func(i, j int) bool {
		pi := priorityVal(out[i].Priority)
		pj := priorityVal(out[j].Priority)
		if pi != pj {
			return pi < pj
		}
		return out[i].Title < out[j].Title
	})
	return out
}

func priorityVal(p *int) int {
	if p == nil {
		return 99
	}
	return *p
}

// priorityBadge renders a priority pip like "● P1". Returns the empty
// string when no priority is set so the template still aligns.
func priorityBadge(p *int) string {
	if p == nil {
		return "●  -"
	}
	return fmt.Sprintf("● P%d", *p)
}

// humanDuration renders a Duration as a short human string ("12s",
// "14m", "3h"). Approximation; status output isn't a stopwatch.
func humanDuration(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
