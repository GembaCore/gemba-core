// Convenience verbs — workspace status handler tests (gm-o9t8.1.4.4).
//
// The status endpoint replaces the gm-o9t8.14 501 stub with a real
// 200 envelope. These tests pin:
//
//   - auth: token-mode requires a bearer (the GET has no nonce gate)
//   - openapi: the path is still declared
//   - shape: the response carries workspace_id + mode + beads + agents,
//     and repo iff the workspace path resolves to a git repo
//
// The handler degrades gracefully: with no host wired (the default
// fakeSPA constructor), beads/agents stripes come back zeroed and
// repo is nil — that's the path TestWorkspaceStatus_HandlerDegraded
// exercises. Real-data cases (clone present, fake adaptors) live in
// the integration tests beside the adaptor wiring.

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/config"
)

// TestWorkspaceStatus_OpenAPIAndAuth keeps the table-driven auth /
// openapi-registration invariants the 501 stub had, swapped to expect
// 200 from the real handler. The status route is a read; there is no
// X-GEMBA-Confirm gate.
func TestWorkspaceStatus_OpenAPIAndAuth(t *testing.T) {
	cfg := config.ServeConfig{AuthMode: "token", AuthToken: "testpat"}
	h := NewRouter(cfg, fakeSPA(), nil)

	// 1. OpenAPI declares the route.
	var doc struct {
		Paths map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(OpenAPISpec(), &doc); err != nil {
		t.Fatalf("parse OpenAPISpec: %v", err)
	}
	if _, ok := doc.Paths["/api/v1/workspaces/{wsid}/status"]; !ok {
		t.Errorf("openapi.json missing /api/v1/workspaces/{wsid}/status")
	}

	// 2. Anonymous request → 401.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/dummy/status", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("anon = %d, want 401; body=%q", rec.Code, rec.Body.String())
	}

	// 3. Authed request → 200 with the new envelope.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/dummy/status", nil)
	req.Header.Set("Authorization", "Bearer testpat")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("authed = %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	var resp WorkspaceStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v; body=%q", err, rec.Body.String())
	}
	if resp.WorkspaceID != "dummy" {
		t.Errorf("workspace_id = %q, want dummy", resp.WorkspaceID)
	}
	if resp.Mode != "ad-hoc" {
		t.Errorf("mode = %q, want ad-hoc", resp.Mode)
	}
}

// TestWorkspaceStatus_HandlerDegraded covers the "no host wired"
// branch — every stripe degrades to a zero value, repo stays nil,
// no panic. The CLI's no-args dashboard hits this shape against a
// minimally-configured server.
func TestWorkspaceStatus_HandlerDegraded(t *testing.T) {
	// Pin HOME to an empty dir so resolveWorkspacePath sees no real
	// projects (the real ~/.gemba would resolve to a checked-in
	// workspace.toml otherwise).
	t.Setenv("HOME", t.TempDir())
	cfg := config.ServeConfig{ConfigPath: filepath.Join(t.TempDir(), "nonexistent.toml")}
	h := NewRouter(cfg, fakeSPA(), nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/wsx/status", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	var resp WorkspaceStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.WorkspaceID != "wsx" {
		t.Errorf("workspace_id = %q, want wsx", resp.WorkspaceID)
	}
	if resp.Beads.OpenTotal != 0 || resp.Beads.Ready != 0 {
		t.Errorf("beads counts should be zero in degraded mode, got %+v", resp.Beads)
	}
	if resp.Agents.Active != 0 || resp.Agents.LastRunID != "" {
		t.Errorf("agents should be empty in degraded mode, got %+v", resp.Agents)
	}
	// Repo is nil unless a workspace path resolves; the degraded path
	// has no config wired so it should be nil.
	if resp.Repo != nil {
		t.Errorf("repo should be nil when workspace can't be resolved, got %+v", resp.Repo)
	}
}

// TestReadRepoStatus_NoRepo covers the path where a workspace
// directory exists but has no .git — readRepoStatus returns nil so
// the response omits the repo block.
func TestReadRepoStatus_NoRepo(t *testing.T) {
	dir := t.TempDir()
	if got := readRepoStatus(t.Context(), dir); got != nil {
		t.Errorf("readRepoStatus on non-repo = %+v, want nil", got)
	}
}

// TestReadRepoStatus_RepoWithHead seeds a real git repo + commit and
// verifies HEAD/branch/dirty come back populated. Skips if git is not
// on $PATH (CI sandboxes without git).
func TestReadRepoStatus_RepoWithHead(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	mustGit(t, dir, "init", "-q", "-b", "main")
	mustGit(t, dir, "-c", "user.email=test@example.com", "-c", "user.name=test", "commit", "--allow-empty", "-m", "init", "-q")

	rs := readRepoStatus(t.Context(), dir)
	if rs == nil {
		t.Fatalf("readRepoStatus = nil, want non-nil")
	}
	if len(rs.Head) < 7 {
		t.Errorf("head looks unset: %q", rs.Head)
	}
	if rs.Branch != "main" {
		t.Errorf("branch = %q, want main", rs.Branch)
	}
	if rs.Dirty {
		t.Errorf("dirty = true on fresh repo")
	}

	// Mark the tree dirty.
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if rs := readRepoStatus(t.Context(), dir); rs == nil || !rs.Dirty {
		t.Errorf("dirty after write: %+v", rs)
	}
}

// TestCollectAgentSummary_NoRuns covers "no sessions yet" — the
// summary stripe carries zero active + empty last-run fields. The
// dashboard renders this as "no agents have run yet".
func TestCollectAgentSummary_NoRuns(t *testing.T) {
	op := &fakeOrch{sessions: nil}
	s := collectAgentSummary(t.Context(), op)
	if s.Active != 0 || s.LastRunID != "" || s.LastRunStatus != "" || s.LastRunSummary != "" {
		t.Errorf("summary on empty op = %+v, want zero-valued", s)
	}
}

// TestCollectAgentSummary_LastRun seeds a mix of terminal + active
// sessions; the summary surfaces the most-recent ID and the count of
// non-terminal recent sessions.
func TestCollectAgentSummary_LastRun(t *testing.T) {
	now := time.Now()
	old := now.Add(-2 * time.Hour)
	end := now.Add(-1 * time.Hour)
	op := &fakeOrch{sessions: []core.Session{
		{ID: "s-old", AgentID: "agent-1", AssignmentID: "gm-1", Status: core.SessionCompleted,
			StartedAt: old, EndedAt: &end},
		{ID: "s-new", AgentID: "agent-2", AssignmentID: "gm-2", Status: core.SessionWorking,
			StartedAt: now.Add(-1 * time.Minute)},
	}}
	s := collectAgentSummary(t.Context(), op)
	if s.LastRunID != "s-new" {
		t.Errorf("last_run_id = %q, want s-new", s.LastRunID)
	}
	if s.LastRunStatus != string(core.SessionWorking) {
		t.Errorf("last_run_status = %q, want working", s.LastRunStatus)
	}
	if s.LastRunSummary == "" {
		t.Errorf("last_run_summary empty; want non-empty")
	}
	if s.Active != 1 {
		t.Errorf("active = %d, want 1 (only s-new is non-terminal + recent)", s.Active)
	}
}

// fakeOrch is the minimum surface collectAgentSummary calls — just
// ListSessions. Implementing the full OrchestrationPlaneAdaptor here
// would couple this test to every interface evolution; instead we
// keep the surface narrow and rely on the compile-time check below
// that fakeOrch still satisfies the interface.
type fakeOrch struct {
	core.OrchestrationPlaneAdaptor // embed for stub methods we don't exercise
	sessions                       []core.Session
}

func (f *fakeOrch) ListSessions(_ context.Context, _ core.SessionFilter) ([]core.Session, error) {
	return f.sessions, nil
}

// TestRunSummary_NoTranscriptFallback covers the "no transcripts dir
// yet" path — runSummary falls back to the legacy
// `<agent_id> on <assignment_id>` shape so the dashboard renders on a
// freshly-ratified workspace (gm-o9t8.1.18).
func TestRunSummary_NoTranscriptFallback(t *testing.T) {
	s := &core.Session{ID: "sess-1", AgentID: "agent-1", AssignmentID: "gm-abc.3"}
	// transcriptsDir = "" → fallback path.
	got := runSummary(s, "", time.Now())
	if got != "agent-1 on gm-abc.3" {
		t.Errorf("runSummary fallback = %q, want %q", got, "agent-1 on gm-abc.3")
	}
}

// TestRunSummary_TerminalEventOnly seeds a transcript whose last event
// is a `completed` with a `summary` field; runSummary should surface
// `completed · <summary>`.
func TestRunSummary_TerminalEventOnly(t *testing.T) {
	dir := t.TempDir()
	runDir := filepath.Join(dir, "sess-1")
	if err := os.MkdirAll(runDir, 0o750); err != nil {
		t.Fatal(err)
	}
	tr := filepath.Join(runDir, "events.jsonl")
	body := strings.Join([]string{
		`{"event_type":"started"}`,
		`{"event_type":"completed","summary":"agent landed gm-abc.3"}`,
	}, "\n")
	if err := os.WriteFile(tr, []byte(body), 0o640); err != nil {
		t.Fatal(err)
	}
	s := &core.Session{ID: "sess-1", AgentID: "agent-1", AssignmentID: "gm-abc.3",
		Status: core.SessionCompleted}
	got := runSummary(s, dir, time.Now())
	want := "completed · agent landed gm-abc.3"
	if got != want {
		t.Errorf("runSummary = %q, want %q", got, want)
	}
}

// TestRunSummary_FailureWithReason seeds a failed terminal event with
// a `reason` field; the summary surfaces `failed · <reason>`.
func TestRunSummary_FailureWithReason(t *testing.T) {
	dir := t.TempDir()
	runDir := filepath.Join(dir, "sess-2")
	if err := os.MkdirAll(runDir, 0o750); err != nil {
		t.Fatal(err)
	}
	tr := filepath.Join(runDir, "events.jsonl")
	body := `{"event_type":"failed","reason":"go test ./... returned non-zero"}`
	if err := os.WriteFile(tr, []byte(body), 0o640); err != nil {
		t.Fatal(err)
	}
	s := &core.Session{ID: "sess-2", Status: core.SessionFailed}
	got := runSummary(s, dir, time.Now())
	if got != "failed · go test ./... returned non-zero" {
		t.Errorf("runSummary = %q", got)
	}
}

// TestRunSummary_BeadNoteWins covers the precedence rule: when a
// transcript carries a bead_note_appended event, that note text is the
// summary even if a terminal event also has a summary field.
func TestRunSummary_BeadNoteWins(t *testing.T) {
	dir := t.TempDir()
	runDir := filepath.Join(dir, "sess-3")
	if err := os.MkdirAll(runDir, 0o750); err != nil {
		t.Fatal(err)
	}
	tr := filepath.Join(runDir, "events.jsonl")
	body := strings.Join([]string{
		`{"event_type":"started"}`,
		`{"event_type":"bead_note_appended","note":"operator: shipped /healthz endpoint"}`,
		`{"event_type":"completed","summary":"agent on gm-foo"}`,
	}, "\n")
	if err := os.WriteFile(tr, []byte(body), 0o640); err != nil {
		t.Fatal(err)
	}
	s := &core.Session{ID: "sess-3", Status: core.SessionCompleted}
	got := runSummary(s, dir, time.Now())
	want := "completed · operator: shipped /healthz endpoint"
	if got != want {
		t.Errorf("runSummary = %q, want %q", got, want)
	}
}

// TestRunSummary_BeadNoteTruncated keeps notes from blowing out the
// dashboard line width.
func TestRunSummary_BeadNoteTruncated(t *testing.T) {
	dir := t.TempDir()
	runDir := filepath.Join(dir, "sess-4")
	if err := os.MkdirAll(runDir, 0o750); err != nil {
		t.Fatal(err)
	}
	tr := filepath.Join(runDir, "events.jsonl")
	longNote := strings.Repeat("a", 200)
	body := `{"event_type":"bead_note_appended","note":"` + longNote + `"}` + "\n" +
		`{"event_type":"completed"}`
	if err := os.WriteFile(tr, []byte(body), 0o640); err != nil {
		t.Fatal(err)
	}
	s := &core.Session{ID: "sess-4", Status: core.SessionCompleted}
	got := runSummary(s, dir, time.Now())
	if !strings.HasPrefix(got, "completed · ") {
		t.Fatalf("missing prefix: %q", got)
	}
	// state + " · " + 120 truncated runes.
	bodyPart := strings.TrimPrefix(got, "completed · ")
	if len([]rune(bodyPart)) > 120 {
		t.Errorf("truncation failed: %d runes", len([]rune(bodyPart)))
	}
}

// TestRunSummary_InFlightRun covers the "transcript exists but no
// terminal event yet" path — summary should report `running · <elapsed>`.
func TestRunSummary_InFlightRun(t *testing.T) {
	dir := t.TempDir()
	runDir := filepath.Join(dir, "sess-5")
	if err := os.MkdirAll(runDir, 0o750); err != nil {
		t.Fatal(err)
	}
	tr := filepath.Join(runDir, "events.jsonl")
	if err := os.WriteFile(tr, []byte(`{"event_type":"started"}`), 0o640); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	s := &core.Session{ID: "sess-5", Status: core.SessionWorking,
		StartedAt: now.Add(-4 * time.Minute)}
	got := runSummary(s, dir, now)
	if !strings.HasPrefix(got, "running · ") {
		t.Errorf("runSummary = %q, want 'running · ...' prefix", got)
	}
	if !strings.Contains(got, "elapsed") {
		t.Errorf("expected elapsed suffix; got %q", got)
	}
}

// TestRunSummary_MissingTranscriptDir covers the path where the
// session's run-id directory simply doesn't exist (e.g. an external
// adaptor that never writes transcripts). Falls back to the legacy
// shape rather than emitting an empty line.
func TestRunSummary_MissingTranscriptDir(t *testing.T) {
	dir := t.TempDir() // exists but no run subdirs.
	s := &core.Session{ID: "sess-6", AgentID: "agent-x", AssignmentID: "gm-7"}
	got := runSummary(s, dir, time.Now())
	if got != "agent-x on gm-7" {
		t.Errorf("runSummary = %q, want fallback %q", got, "agent-x on gm-7")
	}
}

// fakeDoltReporter is a minimal DoltStatusReporter for status tests.
type fakeDoltReporter struct {
	ready    bool
	state    string
	port     int
	dataDir  string
	restarts int64
	lastErr  error
}

func (f *fakeDoltReporter) Ready() bool         { return f.ready }
func (f *fakeDoltReporter) State() string       { return f.state }
func (f *fakeDoltReporter) Port() int           { return f.port }
func (f *fakeDoltReporter) DataDir() string     { return f.dataDir }
func (f *fakeDoltReporter) RestartCount() int64 { return f.restarts }
func (f *fakeDoltReporter) LastError() error    { return f.lastErr }

// readyOnly is a DoltReadyChecker that doesn't satisfy
// DoltStatusReporter. Verifies the status handler omits embedded_dolt
// when only the narrow surface is attached.
type readyOnly struct{ ready bool }

func (r *readyOnly) Ready() bool { return r.ready }

// TestWorkspaceStatus_EmbeddedDolt_Populated verifies the embedded_dolt
// stripe carries the reporter's posture when a richer supervisor is
// attached (gm-o9t8.1.9).
func TestWorkspaceStatus_EmbeddedDolt_Populated(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := config.ServeConfig{ConfigPath: filepath.Join(t.TempDir(), "x.toml")}
	h := NewRouter(cfg, fakeSPA(), nil)
	h.AttachDoltSupervisor(&fakeDoltReporter{
		ready: true, state: "ready", port: 12345,
		dataDir: "/var/lib/gemba/data/dolt", restarts: 0,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/dummy/status", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	var resp WorkspaceStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.EmbeddedDolt == nil {
		t.Fatalf("embedded_dolt should be populated when a reporter is attached")
	}
	if resp.EmbeddedDolt.State != "ready" {
		t.Errorf("state = %q, want ready", resp.EmbeddedDolt.State)
	}
	if resp.EmbeddedDolt.Port != 12345 {
		t.Errorf("port = %d, want 12345", resp.EmbeddedDolt.Port)
	}
	if !resp.EmbeddedDolt.Enabled {
		t.Errorf("enabled should be true")
	}
}

// TestWorkspaceStatus_EmbeddedDolt_Restarting covers the degraded path
// — state=restarting + non-zero restart count + non-empty last error.
func TestWorkspaceStatus_EmbeddedDolt_Restarting(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := config.ServeConfig{ConfigPath: filepath.Join(t.TempDir(), "x.toml")}
	h := NewRouter(cfg, fakeSPA(), nil)
	h.AttachDoltSupervisor(&fakeDoltReporter{
		state: "restarting", port: 12345,
		dataDir: "/tmp/dolt", restarts: 3,
		lastErr: errSentinel("SELECT 1: connection refused"),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/dummy/status", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var resp WorkspaceStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.EmbeddedDolt == nil || resp.EmbeddedDolt.State != "restarting" {
		t.Errorf("expected state=restarting, got %+v", resp.EmbeddedDolt)
	}
	if resp.EmbeddedDolt.RestartCount != 3 {
		t.Errorf("restart_count = %d, want 3", resp.EmbeddedDolt.RestartCount)
	}
	if resp.EmbeddedDolt.LastError == "" {
		t.Errorf("last_error should be populated")
	}
}

// TestWorkspaceStatus_EmbeddedDolt_Failed covers the terminal "gave up"
// state — the SPA banner needs to show this distinctly.
func TestWorkspaceStatus_EmbeddedDolt_Failed(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := config.ServeConfig{ConfigPath: filepath.Join(t.TempDir(), "x.toml")}
	h := NewRouter(cfg, fakeSPA(), nil)
	h.AttachDoltSupervisor(&fakeDoltReporter{
		state: "failed", restarts: 5,
		lastErr: errSentinel("gave up after 5 consecutive failures"),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/dummy/status", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var resp WorkspaceStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.EmbeddedDolt == nil || resp.EmbeddedDolt.State != "failed" {
		t.Errorf("expected state=failed, got %+v", resp.EmbeddedDolt)
	}
}

// TestWorkspaceStatus_EmbeddedDolt_NotEnabled keeps the no-supervisor
// path null and the ready-only (narrow surface) path null. Both lanes
// resolve to "external Dolt or noop — no embedded posture to report".
func TestWorkspaceStatus_EmbeddedDolt_NotEnabled(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := config.ServeConfig{ConfigPath: filepath.Join(t.TempDir(), "x.toml")}
	h := NewRouter(cfg, fakeSPA(), nil)

	// 1. No supervisor attached at all.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/dummy/status", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var resp WorkspaceStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.EmbeddedDolt != nil {
		t.Errorf("no supervisor → embedded_dolt should be null, got %+v", resp.EmbeddedDolt)
	}

	// 2. Narrow checker attached — embedded_dolt still null.
	h.AttachDoltSupervisor(&readyOnly{ready: true})
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/dummy/status", nil)
	h.ServeHTTP(rec, req)
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.EmbeddedDolt != nil {
		t.Errorf("narrow checker → embedded_dolt should be null, got %+v", resp.EmbeddedDolt)
	}
}

// errSentinel is a tiny string-based error type so the test doesn't
// pull in fmt/errors fluff at the file top.
type errSentinel string

func (e errSentinel) Error() string { return string(e) }

// mustGit is a tiny helper around exec.Command for the repo-seeding
// path. Failures are fatal because they'd corrupt later assertions.
func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}
