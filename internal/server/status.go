// Convenience verbs — workspace status handler (gm-o9t8.1.4.4).
//
// GET /api/v1/workspaces/{wsid}/status backs `gemba` invoked with no
// args: a single-shot read returning the unified work / spec / decision
// posture for the workspace. Replaces the gm-o9t8.14 501 stub.
//
// Response shape:
//
//	{
//	  "workspace_id": "...",
//	  "mode": "ad-hoc",
//	  "repo":   { "head": "<sha>", "dirty": bool, "branch": "main" },
//	  "beads":  { "ready": N, "in_flight": N, "blocked": N,
//	              "open_total": N, "closed_today": N },
//	  "agents": { "active": N, "last_run_id": "...",
//	              "last_run_status": "completed",
//	              "last_run_summary": "..." }
//	}
//
// Degradation rules: every section is best-effort. If the workspace
// can't be resolved to a path we return repo=nil and bead/agent
// fields zeroed (rather than 404'ing) so the dashboard can still
// render. Errors from subsystems are logged-but-not-fatal — the
// dashboard prefers a partial render over no render.

package server

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/config"
)

// WorkspaceStatusResponse is the JSON body GET
// /api/v1/workspaces/{wsid}/status returns. Fields use pointer / omitempty
// semantics so an unresolvable subsystem renders as null rather than as
// a misleading zero.
type WorkspaceStatusResponse struct {
	WorkspaceID   string                `json:"workspace_id"`
	Mode          string                `json:"mode"`
	Repo          *WorkspaceRepoStatus  `json:"repo"`
	Beads         WorkspaceBeadCounts   `json:"beads"`
	Agents        WorkspaceAgentSummary `json:"agents"`
	EmbeddedDolt  *EmbeddedDoltStatus   `json:"embedded_dolt"`
}

// EmbeddedDoltStatus surfaces the embedded-dolt supervisor's posture so
// the SPA can render a degraded-state banner when the subprocess is
// restarting or has given up (gm-o9t8.1.9). Null when the supervisor
// isn't wired (external Dolt via --dolt-url, or the noop adaptor).
//
// State values:
//
//   - "starting"   — Start() in flight; first SELECT 1 hasn't landed yet
//   - "ready"      — last probe succeeded; subprocess healthy
//   - "restarting" — a recent probe failed; supervisor is in backoff /
//                    respawn between probes
//   - "failed"     — maxConsecutiveFailures exhausted; supervisor gave up
type EmbeddedDoltStatus struct {
	Enabled      bool   `json:"enabled"`
	State        string `json:"state"`
	Port         int    `json:"port"`
	DataDir      string `json:"data_dir"`
	RestartCount int64  `json:"restart_count"`
	LastError    string `json:"last_error,omitempty"`
}

// WorkspaceRepoStatus carries the git posture for the workspace repo.
// Nil-valued when the workspace isn't backed by a clone yet (e.g. a
// freshly-created project that hasn't run `git init` / clone).
type WorkspaceRepoStatus struct {
	Head   string `json:"head"`
	Dirty  bool   `json:"dirty"`
	Branch string `json:"branch"`
}

// WorkspaceBeadCounts mirrors the buckets the dashboard renders.
// `ready` and `in_flight` are mapped from native bd statuses; the
// state_category columns aren't used here because the dashboard text
// is keyed on bd's vocabulary.
type WorkspaceBeadCounts struct {
	Ready       int `json:"ready"`
	InFlight    int `json:"in_flight"`
	Blocked     int `json:"blocked"`
	OpenTotal   int `json:"open_total"`
	ClosedToday int `json:"closed_today"`
}

// WorkspaceAgentSummary captures the "agents" stripe. Active counts
// sessions that haven't recorded a terminal event in the last
// agentActiveWindow. LastRunSummary is a best-effort one-line
// description; today it's just "agent X on bead Y" — proper transcript
// summarisation is a follow-up.
type WorkspaceAgentSummary struct {
	Active         int    `json:"active"`
	LastRunID      string `json:"last_run_id,omitempty"`
	LastRunStatus  string `json:"last_run_status,omitempty"`
	LastRunSummary string `json:"last_run_summary,omitempty"`
}

// agentActiveWindow defines the cutoff for "active" sessions. A session
// counts as active if it is non-terminal AND we've seen a heartbeat or
// status transition within this window. The 5-minute number is a
// pragmatic mid-point between "instantaneous" and "session lingered
// hours waiting on input" — tunable when we wire real liveness signals.
const agentActiveWindow = 5 * time.Minute

// workspaceStatus is the GET /api/v1/workspaces/{wsid}/status handler.
// Replaces the gm-o9t8.14 501 stub. Every sub-section is best-effort
// so an absent host / unbound adaptor degrades to zero counts rather
// than a fatal envelope.
//
// TODO(gm-v0sp): mode is hard-wired to "ad-hoc". Real mode detection
// reads specs/*/spec.md + .lockfile.json under the ASDD epic.
func (r *Router) workspaceStatus(w http.ResponseWriter, req *http.Request) {
	wsid := chi.URLParam(req, "wsid")
	resp := WorkspaceStatusResponse{
		WorkspaceID: wsid,
		Mode:        "ad-hoc",
	}

	// Resolve the workspace path. wsid "_" / empty falls back to the
	// active project (gm-root.18) or the first complete project; any
	// other wsid is matched on directory basename. Failure to resolve
	// is fatal-but-soft: repo stays nil; bead/agent stripes still try.
	wsPath := r.resolveWorkspacePath(wsid)
	if wsPath != "" {
		if rs := readRepoStatus(req.Context(), wsPath); rs != nil {
			resp.Repo = rs
		}
	}

	// Beads counts via the bound WorkPlane. Skipped when no host or
	// adaptor is bound (tests, degraded servers) so the response shape
	// stays consistent.
	if r.host != nil {
		if wp := r.host.WorkPlane(); wp != nil {
			resp.Beads = collectBeadCounts(req.Context(), wp)
		}
	}

	// Agent stripe via the OrchestrationPlane's session list. Same
	// degradation rules as beads.
	if r.host != nil {
		if op := r.host.OrchestrationPlane(); op != nil {
			resp.Agents = collectAgentSummary(req.Context(), op)
		}
	}

	// Embedded-dolt stripe (gm-o9t8.1.9). Only populated when the
	// attached supervisor implements the richer DoltStatusReporter
	// surface — preserves the existing `--dolt-url` / no-checker
	// modes where embedded_dolt stays null.
	r.doltMu.RLock()
	sup := r.doltSupervisor
	r.doltMu.RUnlock()
	if reporter, ok := sup.(DoltStatusReporter); ok {
		resp.EmbeddedDolt = embeddedDoltStatus(reporter)
	}

	writeJSON(w, http.StatusOK, resp)
}

// embeddedDoltStatus converts a DoltStatusReporter into the JSON
// payload the SPA consumes. Pulled out for test reuse.
func embeddedDoltStatus(r DoltStatusReporter) *EmbeddedDoltStatus {
	out := &EmbeddedDoltStatus{
		Enabled:      true,
		State:        r.State(),
		Port:         r.Port(),
		DataDir:      r.DataDir(),
		RestartCount: r.RestartCount(),
	}
	if err := r.LastError(); err != nil {
		out.LastError = err.Error()
	}
	return out
}

// resolveWorkspacePath maps a wsid to a workspace root on disk. It
// uses the same project registry /api/v1/projects walks: load user
// config, list projects, match on name. wsid "_" or "" resolves to
// the active project (gm-root.18 in-process state). Returns "" when
// no match is found — callers treat that as "skip repo stripe".
func (r *Router) resolveWorkspacePath(wsid string) string {
	ucfg, err := config.LoadUserConfig(r.cfg.ConfigPath)
	if err != nil {
		return ""
	}
	defaultDir, err := config.ResolveDefaultDir(ucfg)
	if err != nil {
		return ""
	}
	found, err := config.ListAllProjects(defaultDir, ucfg.Projects.ExtraRoots)
	if err != nil || len(found) == 0 {
		return ""
	}

	target := strings.TrimSpace(wsid)
	if target == "" || target == "_" {
		if active := activeProjectNameRead(); active != "" {
			target = active
		}
	}

	// Exact basename match first.
	if target != "" {
		for _, p := range found {
			if p.Name == target {
				return p.Path
			}
		}
	}
	// Fallback: first complete project. Keeps "_" useful when no
	// project has been switched-to yet.
	for _, p := range found {
		if p.Kind == config.KindComplete {
			return p.Path
		}
	}
	return ""
}

// activeProjectNameRead is a tiny shim around the package-level
// activeProjectName mutex so this file doesn't reach into projects.go
// internals.
func activeProjectNameRead() string {
	activeProjectMu.RLock()
	defer activeProjectMu.RUnlock()
	return activeProjectName
}

// readRepoStatus shells out to git to capture HEAD, branch, and the
// dirty flag. Returns nil when the directory isn't a git repo (no
// .git, or `git rev-parse` rejects it) so the caller can omit the
// repo field. A short timeout caps the syscall budget — `gemba`
// invoked with no args is interactive; we can't block on a wedged git.
func readRepoStatus(ctx context.Context, root string) *WorkspaceRepoStatus {
	// Hard time cap so a broken repo can't hang the dashboard.
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	// Cheap precondition: .git must exist. Saves an exec on the empty
	// case (a workspace.toml without a clone — common on fresh
	// ratify).
	if _, err := exec.LookPath("git"); err != nil {
		return nil
	}
	gitDir := filepath.Join(root, ".git")
	if _, err := os.Stat(gitDir); err != nil {
		return nil
	}

	head, err := runGit(ctx, root, "rev-parse", "HEAD")
	if err != nil {
		return nil
	}
	rs := &WorkspaceRepoStatus{Head: strings.TrimSpace(head)}

	if branch, err := runGit(ctx, root, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		rs.Branch = strings.TrimSpace(branch)
	}
	if porc, err := runGit(ctx, root, "status", "--porcelain"); err == nil {
		rs.Dirty = strings.TrimSpace(porc) != ""
	}
	return rs
}

// runGit invokes git with a context, in the given working directory,
// and returns trimmed stdout on success. Wrapper so the call sites
// don't repeat the boilerplate.
func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// collectBeadCounts queries the WorkPlane for the buckets the
// dashboard cares about. Each native-status query is independent so
// adaptors that don't recognize a particular status (e.g. "blocked")
// degrade to zero rather than error the whole response.
func collectBeadCounts(ctx context.Context, wp core.WorkPlane) WorkspaceBeadCounts {
	counts := WorkspaceBeadCounts{}

	count := func(status string) int {
		f := core.WorkItemFilter{Statuses: []string{status}, Limit: defaultListLimit}
		items, err := wp.ListWorkItems(ctx, f)
		if err != nil {
			return 0
		}
		return len(items)
	}
	counts.Ready = count("ready")
	counts.InFlight = count("in_progress")
	counts.Blocked = count("blocked")

	// open_total = ready + in_progress + blocked + any other open
	// lane. Easiest accurate path is to ask by state_category for the
	// non-terminal buckets, which is adaptor-agnostic.
	openFilter := core.WorkItemFilter{
		StateCategory: []core.StateCategory{
			core.StateBacklog, core.StateUnstarted, core.StateStaged, core.StateStarted,
		},
		Limit: defaultListLimit,
	}
	if items, err := wp.ListWorkItems(ctx, openFilter); err == nil {
		counts.OpenTotal = len(items)
	} else {
		// Fall back to the naive sum if state_category filtering is
		// unsupported by the adaptor.
		counts.OpenTotal = counts.Ready + counts.InFlight + counts.Blocked
	}

	// closed_today: ask for the completed bucket and filter client-side
	// by UpdatedAt within the last 24h. Adaptors that don't carry
	// UpdatedAt for closed items just contribute zero.
	closedFilter := core.WorkItemFilter{
		StateCategory: []core.StateCategory{core.StateCompleted, core.StateCanceled},
		Limit:         defaultListLimit,
	}
	if items, err := wp.ListWorkItems(ctx, closedFilter); err == nil {
		cutoff := time.Now().Add(-24 * time.Hour)
		for _, wi := range items {
			if wi.UpdatedAt.After(cutoff) {
				counts.ClosedToday++
			}
		}
	}
	return counts
}

// collectAgentSummary walks the orchestration plane's session list to
// derive the "agents" stripe.
//
// Source-of-truth choice: we use OrchestrationPlane.ListSessions
// (terminal + non-terminal) rather than the events.Hub. The hub
// doesn't retain history — Subscribe only sees new events — so it
// can't answer "what was the last run?" after a restart. Sessions are
// the durable record.
func collectAgentSummary(ctx context.Context, op core.OrchestrationPlaneAdaptor) WorkspaceAgentSummary {
	summary := WorkspaceAgentSummary{}

	sessions, err := op.ListSessions(ctx, core.SessionFilter{IncludeTerminal: true})
	if err != nil || len(sessions) == 0 {
		return summary
	}

	now := time.Now()
	var latest *core.Session
	for i := range sessions {
		s := &sessions[i]
		if isActiveSession(s, now) {
			summary.Active++
		}
		if latest == nil || sessionRecency(s).After(sessionRecency(latest)) {
			latest = s
		}
	}
	if latest != nil {
		summary.LastRunID = latest.ID
		summary.LastRunStatus = string(latest.Status)
		summary.LastRunSummary = sessionSummary(latest)
	}
	return summary
}

// isActiveSession decides whether a session counts as "active" right
// now. Non-terminal status + a heartbeat or start within the active
// window. Adaptors that don't populate LastHeartbeat fall back to
// StartedAt so a session that just started always counts.
func isActiveSession(s *core.Session, now time.Time) bool {
	switch s.Status {
	case core.SessionCompleted, core.SessionFailed:
		return false
	}
	if s.EndedAt != nil {
		return false
	}
	last := s.StartedAt
	if s.LastHeartbeat != nil && s.LastHeartbeat.After(last) {
		last = *s.LastHeartbeat
	}
	if last.IsZero() {
		return true // session with no timestamps — assume live
	}
	return now.Sub(last) <= agentActiveWindow
}

// sessionRecency returns the most-recent timestamp on a session.
// Used to pick "the last run" deterministically.
func sessionRecency(s *core.Session) time.Time {
	t := s.StartedAt
	if s.LastHeartbeat != nil && s.LastHeartbeat.After(t) {
		t = *s.LastHeartbeat
	}
	if s.EndedAt != nil && s.EndedAt.After(t) {
		t = *s.EndedAt
	}
	return t
}

// sessionSummary builds a one-line description of a session for the
// dashboard. The shape is `<agent_id> on <assignment_id>`; transcript
// summarisation is a follow-up.
func sessionSummary(s *core.Session) string {
	switch {
	case s.AgentID != "" && s.AssignmentID != "":
		return string(s.AgentID) + " on " + s.AssignmentID
	case s.AgentID != "":
		return string(s.AgentID)
	case s.AssignmentID != "":
		return s.AssignmentID
	}
	return ""
}
