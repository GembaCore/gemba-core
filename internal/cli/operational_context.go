// gm-s47n.5.4 — `gemba operational-context` CLI.
//
// Operator-facing view of the same join /api/operational-context
// returns. Two modes:
//
//   gemba operational-context --session-id ID
//     Calls a running gemba serve at --base-url
//     (default http://localhost:7666). Operators with a live setup
//     run this to see the planner's join in seconds.
//
//   gemba operational-context --file input.json [--session-id ID]
//     Computes the join offline from a stub set of readers seeded
//     by the JSON envelope. Useful for diagnostics, tests, and
//     pipelines that don't want to touch a server. When --session-id
//     is omitted, all sessions in the input are emitted.
//
// Either mode supports --json for the stable wire envelope; default
// is a per-session text card with the same fields the SPA strip
// (gm-s47n.6.7) renders.

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"time"

	"github.com/spf13/cobra"

	"github.com/MikeBengtson/gemba/internal/core"
	"github.com/MikeBengtson/gemba/internal/planner"
)

// OperationalContextInput is the offline-mode envelope. Operators
// supply the session/agent/workspace/profile pieces they have and
// the CLI runs ReadOperationalContext against in-memory readers.
//
// Populating only Sessions still produces a useful result —
// TimeOnTask is meaningful from a Session alone (see gm-s47n.5.1).
// Populate Profiles too for ContextPressure + ConceptDrift.
type OperationalContextInput struct {
	Sessions   []*core.Session                    `json:"sessions"`
	Agents     []*core.AgentRef                   `json:"agents,omitempty"`
	Workspaces []*core.Workspace                  `json:"workspaces,omitempty"`
	Profiles   map[string]*planner.SessionProfile `json:"profiles,omitempty"` // key = session id
	// Optional: explicit clock for deterministic test runs (RFC3339).
	Now string `json:"now,omitempty"`
}

// staticSessionLookup is the offline SessionLookup; just an
// in-memory map keyed by session id.
type staticSessionLookup struct{ byID map[string]*core.Session }

func (s staticSessionLookup) FindSession(_ context.Context, sessionID string) (*core.Session, error) {
	return s.byID[sessionID], nil
}

type staticAgentLookup struct {
	byID map[core.AgentID]*core.AgentRef
}

func (a staticAgentLookup) ReadAgent(_ context.Context, id core.AgentID) (*core.AgentRef, error) {
	return a.byID[id], nil
}

type staticWorkspaceLookup struct{ byID map[string]*core.Workspace }

func (w staticWorkspaceLookup) InspectWorkspace(_ context.Context, workspaceID string) (core.Workspace, error) {
	if ws, ok := w.byID[workspaceID]; ok && ws != nil {
		return *ws, nil
	}
	return core.Workspace{}, nil
}

type staticProfileLookup struct {
	byID map[string]*planner.SessionProfile
}

func (p staticProfileLookup) GetProfile(_ context.Context, sessionID string) (*planner.SessionProfile, error) {
	return p.byID[sessionID], nil
}

func newOperationalContextCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "operational-context",
		Short: "Print the planner's per-session operational-context join",
		Long: `Print the AgentRef + Session + Workspace + Assignment +
SessionProfile + SessionHealth join for a session — the same shape
GET /api/operational-context returns.

Two modes:

  --base-url URL     Query a running gemba serve. Default is
                     http://localhost:7666. --session-id required.

  --file PATH        Compute offline from an OperationalContextInput
                     JSON envelope. --session-id selects one entry;
                     omit it to print every session in the input.

In offline mode the readers run against the in-memory stub. The
result mirrors the live endpoint exactly — if a Profile, Workspace,
or Agent isn't supplied, the corresponding pointer in the output
is nil (graceful degradation per spec §4 Layer 1).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runOperationalContext(cmd, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-parseable JSON instead of text")
	cmd.Flags().String("file", "", "read OperationalContextInput from this file (offline mode)")
	cmd.Flags().String("session-id", "", "session id to look up (required for --base-url; optional for --file)")
	cmd.Flags().String("base-url", "", "base URL of a running gemba serve (e.g. http://localhost:7666)")
	return cmd
}

func runOperationalContext(cmd *cobra.Command, asJSON bool) error {
	out := cmd.OutOrStdout()
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	file, _ := cmd.Flags().GetString("file")
	sessionID, _ := cmd.Flags().GetString("session-id")
	baseURL, _ := cmd.Flags().GetString("base-url")

	if file != "" && baseURL != "" {
		return fmt.Errorf("--file and --base-url are mutually exclusive")
	}

	if baseURL != "" {
		if sessionID == "" {
			return fmt.Errorf("--session-id is required with --base-url")
		}
		opCtx, err := fetchOperationalContext(ctx, baseURL, sessionID)
		if err != nil {
			return err
		}
		return renderOperationalContexts(out, []*planner.OperationalContext{opCtx}, asJSON)
	}

	in, err := readOpCtxInput(file, cmd.InOrStdin())
	if err != nil {
		return err
	}
	results, err := readOfflineContexts(ctx, in, sessionID)
	if err != nil {
		return err
	}
	return renderOperationalContexts(out, results, asJSON)
}

func readOpCtxInput(path string, stdin io.Reader) (*OperationalContextInput, error) {
	rd := stdin
	if path != "" {
		f, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", path, err)
		}
		defer f.Close()
		rd = f
	}
	var in OperationalContextInput
	dec := json.NewDecoder(rd)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return nil, fmt.Errorf("decode input: %w", err)
	}
	return &in, nil
}

// readOfflineContexts builds in-memory readers from the input
// envelope and runs ReadOperationalContext for each requested
// session. When sessionID is "", every session in the input gets
// resolved (sorted by id for stable output).
func readOfflineContexts(ctx context.Context, in *OperationalContextInput, sessionID string) ([]*planner.OperationalContext, error) {
	sessByID := map[string]*core.Session{}
	for _, s := range in.Sessions {
		if s != nil {
			sessByID[s.ID] = s
		}
	}
	agentByID := map[core.AgentID]*core.AgentRef{}
	for _, a := range in.Agents {
		if a != nil {
			agentByID[a.ID] = a
		}
	}
	wsByID := map[string]*core.Workspace{}
	for _, w := range in.Workspaces {
		if w != nil {
			wsByID[w.ID] = w
		}
	}
	profByID := in.Profiles
	if profByID == nil {
		profByID = map[string]*planner.SessionProfile{}
	}

	now := time.Now
	if in.Now != "" {
		t, err := time.Parse(time.RFC3339, in.Now)
		if err != nil {
			return nil, fmt.Errorf("parse 'now': %w", err)
		}
		now = func() time.Time { return t }
	}

	readers := planner.OperationalContextReaders{
		Sessions:   staticSessionLookup{byID: sessByID},
		Agents:     staticAgentLookup{byID: agentByID},
		Workspaces: staticWorkspaceLookup{byID: wsByID},
		Profiles:   staticProfileLookup{byID: profByID},
		Now:        now,
	}

	ids := []string{}
	if sessionID != "" {
		ids = append(ids, sessionID)
	} else {
		for id := range sessByID {
			ids = append(ids, id)
		}
		sort.Strings(ids)
	}

	out := make([]*planner.OperationalContext, 0, len(ids))
	for _, id := range ids {
		opCtx, err := planner.ReadOperationalContext(ctx, id, readers)
		if err != nil {
			return nil, fmt.Errorf("session %s: %w", id, err)
		}
		out = append(out, opCtx)
	}
	return out, nil
}

func fetchOperationalContext(ctx context.Context, baseURL, sessionID string) (*planner.OperationalContext, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse base-url: %w", err)
	}
	u.Path = "/api/operational-context"
	q := u.Query()
	q.Set("session_id", sessionID)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", u.String(), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GET %s: %s: %s", u.String(), resp.Status, string(body))
	}
	var out planner.OperationalContext
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return &out, nil
}

func renderOperationalContexts(out io.Writer, results []*planner.OperationalContext, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{"contexts": results})
	}
	for i, c := range results {
		if i > 0 {
			fmt.Fprintln(out, "")
		}
		renderOperationalContextText(out, c)
	}
	return nil
}

// renderOperationalContextText prints the per-session card the SPA
// strip (gm-s47n.6.7) will mirror — agent identity, workspace
// (repo / branch / worktree / isolation), session status + heartbeat,
// top concepts, pressure / drift / time-on-task.
func renderOperationalContextText(out io.Writer, c *planner.OperationalContext) {
	if c == nil || c.Session == nil {
		fmt.Fprintln(out, "(no session)")
		return
	}
	s := c.Session
	fmt.Fprintf(out, "session: %s\n", s.ID)
	fmt.Fprintf(out, "  status:        %s\n", s.Status)
	if s.LastHeartbeat != nil {
		fmt.Fprintf(out, "  last heartbeat: %s\n", s.LastHeartbeat.Format(time.RFC3339))
	}
	if c.Agent != nil {
		fmt.Fprintf(out, "  agent:         %s (kind=%s, role=%s)\n",
			c.Agent.ID, c.Agent.Kind, c.Agent.Role)
	}
	if c.Workspace != nil {
		ws := c.Workspace
		fmt.Fprintf(out, "  workspace:     %s/%s (kind=%s)\n", ws.Repository, ws.Branch, ws.Kind)
		if path := core.WorkspaceWorktreePath(*ws); path != "" {
			fmt.Fprintf(out, "  worktree:      %s\n", path)
		}
		if ws.Isolation.FSScoped || ws.Isolation.NetIsolated || ws.Isolation.CPULimited {
			fmt.Fprintf(out, "  isolation:     fs=%v net=%v cpu=%v mem=%v snapshot=%v\n",
				ws.Isolation.FSScoped, ws.Isolation.NetIsolated, ws.Isolation.CPULimited,
				ws.Isolation.MemLimited, ws.Isolation.SnapshotRestore)
		}
	}
	if c.Profile != nil {
		top := topConcepts(c.Profile.Concepts, 5)
		if len(top) > 0 {
			fmt.Fprintf(out, "  top concepts:  %s\n", top)
		}
	}
	if c.Health != nil {
		fmt.Fprintf(out, "  pressure:      %.2f\n", c.Health.ContextPressure)
		fmt.Fprintf(out, "  drift:         %.2f\n", c.Health.ConceptDrift)
		fmt.Fprintf(out, "  time-on-task:  %s\n", formatDuration(c.Health.TimeOnTask))
	}
}

// topConcepts returns the top-N concept tags by weight as a
// concise inline list "auth=0.42, spa-routing=0.31, ...".
func topConcepts(concepts map[planner.ConceptTag]float64, n int) string {
	type pair struct {
		tag    planner.ConceptTag
		weight float64
	}
	pairs := make([]pair, 0, len(concepts))
	for k, v := range concepts {
		pairs = append(pairs, pair{tag: k, weight: v})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].weight != pairs[j].weight {
			return pairs[i].weight > pairs[j].weight
		}
		return pairs[i].tag < pairs[j].tag
	})
	if len(pairs) > n {
		pairs = pairs[:n]
	}
	out := ""
	for i, p := range pairs {
		if i > 0 {
			out += ", "
		}
		out += fmt.Sprintf("%s=%.2f", p.tag, p.weight)
	}
	return out
}
