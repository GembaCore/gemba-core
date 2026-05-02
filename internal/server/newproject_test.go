// Tests for /api/v1/newproject/* (gm-root.17). Covers:
//   - HTTP-level wiring for /start, /turn, /ratify (happy paths + 503).
//   - Atomic ratify transaction with a stub command runner so tests
//     don't depend on the host's bd / git binaries — every step's
//     failure path drives a rollback assertion.

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/GembaCore/gemba-core/internal/config"
)

// ─── HTTP wiring ────────────────────────────────────────────────────

// newNewProjectRouter wires a Router with an in-memory store + stub
// turner + a fake ratifier so tests can exercise the SPA contract
// without spawning subprocesses. Returns the router + the store +
// the ratifier so individual cases can probe state.
func newNewProjectRouter(t *testing.T, ratifier NewProjectRatifier) (*Router, NewProjectStore) {
	t.Helper()
	r := NewRouter(config.ServeConfig{}, fakeSPA(), nil)
	t.Cleanup(r.Close)
	store := NewMemoryNewProjectStore()
	r.AttachNewProject(store, NewStubSkillTurner(), ratifier)
	return r, store
}

func newProjectReq(t *testing.T, method, path string, body any) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	return req
}

func TestNewProject_NotConfigured_503(t *testing.T) {
	r := NewRouter(config.ServeConfig{}, fakeSPA(), nil)
	t.Cleanup(r.Close)
	// Intentionally NOT calling AttachNewProject.

	cases := []struct {
		method, path string
		needsNonce   bool
	}{
		{http.MethodPost, "/api/v1/newproject/start", false},
		{http.MethodPost, "/api/v1/newproject/foo/turn", false},
		{http.MethodPost, "/api/v1/newproject/foo/ratify", true},
	}
	for _, c := range cases {
		req := newProjectReq(t, c.method, c.path, map[string]any{"message": "x"})
		if c.needsNonce {
			req.Header.Set(ConfirmHeader, "test-nonce-503")
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s: want 503, got %d body=%q",
				c.method, c.path, rec.Code, rec.Body.String())
		}
	}
}

func TestNewProject_StartTurn_HappyPath(t *testing.T) {
	r, _ := newNewProjectRouter(t, nil)

	// /start
	req := newProjectReq(t, http.MethodPost, "/api/v1/newproject/start", map[string]any{})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("/start: want 201, got %d body=%q", rec.Code, rec.Body.String())
	}
	var startResp startNewProjectResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &startResp); err != nil {
		t.Fatalf("decode /start: %v", err)
	}
	if !strings.HasPrefix(startResp.SessionID, "newproj-") {
		t.Errorf("session id should be prefixed: %q", startResp.SessionID)
	}
	if startResp.Greeting == "" {
		t.Errorf("expected non-empty greeting")
	}
	if startResp.State.Turn != 0 {
		t.Errorf("initial turn should be 0; got %d", startResp.State.Turn)
	}

	// /turn
	turnReq := newProjectReq(t, http.MethodPost,
		"/api/v1/newproject/"+startResp.SessionID+"/turn",
		map[string]any{"message": "build a TODO web app"})
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, turnReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("/turn: want 200, got %d body=%q", rec.Code, rec.Body.String())
	}
	var turnResp turnResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &turnResp); err != nil {
		t.Fatalf("decode /turn: %v", err)
	}
	if turnResp.State.Turn != 1 {
		t.Errorf("turn should be 1 after first /turn; got %d", turnResp.State.Turn)
	}
	if len(turnResp.State.Milestones) != 1 {
		t.Errorf("expected 1 stub milestone; got %d", len(turnResp.State.Milestones))
	}
}

// ─── Ratify happy path with a stub runner ──────────────────────────

// stubRunner records every (binary, args) pair the ratifier issues.
// Each binary's response is keyed off the FIRST arg (the subcommand)
// so cases can stub `bd create` to return a deterministic id without
// stubbing every other call. Unrecognised calls fail loudly so a
// missing fixture surfaces as a test failure, not a silent
// transaction failure.
type stubRunner struct {
	mu    sync.Mutex
	calls []stubCall
	// idSeq counts bd-create calls so each new bead gets a unique id.
	idSeq    int
	failOn   func(call stubCall) error
	bdInitOK bool
}

type stubCall struct {
	Dir  string
	Bin  string
	Args []string
}

func newStubRunner() *stubRunner {
	return &stubRunner{bdInitOK: true}
}

func (s *stubRunner) run(_ context.Context, dir, name string, args ...string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	call := stubCall{Dir: dir, Bin: name, Args: append([]string(nil), args...)}
	s.calls = append(s.calls, call)
	if s.failOn != nil {
		if err := s.failOn(call); err != nil {
			return nil, err
		}
	}
	switch name {
	case "git":
		return nil, nil
	case "bd":
		if len(args) == 0 {
			return nil, errors.New("stub bd: no args")
		}
		switch args[0] {
		case "init":
			if !s.bdInitOK {
				return nil, errors.New("stub bd init configured to fail")
			}
			return []byte(""), nil
		case "create":
			s.idSeq++
			id := fmt.Sprintf("bd-%03d", s.idSeq)
			out := fmt.Sprintf(`{"id":%q,"title":"stub"}`, id)
			return []byte(out), nil
		case "dep":
			return nil, nil
		}
	}
	return nil, fmt.Errorf("stub runner: unknown call %s %v", name, args)
}

func sampleState() NewProjectState {
	return NewProjectState{
		ProjectName: "demo",
		Description: "a sample project",
		TechStack:   []string{"go", "react"},
		Milestones: []DraftMilestone{
			{
				Title:       "M1",
				Description: "first milestone",
				Labels:      []string{"area:onboard"},
				Epics: []DraftEpic{
					{
						Title: "E1",
						Beads: []DraftBead{
							{Title: "B1", Type: "task"},
							{Title: "B2", Type: "task"},
						},
					},
				},
			},
		},
		DraftProjectMD: "# demo\n\nA sample project.\n",
	}
}

func TestRatify_HappyPath(t *testing.T) {
	tmp := t.TempDir()
	stub := newStubRunner()
	rfr := NewRatifier(RatifierConfig{
		DefaultDirOverride: tmp,
		Runner:             stub.run,
	})

	resp, err := rfr.Ratify(context.Background(), sampleState())
	if err != nil {
		t.Fatalf("Ratify: %v", err)
	}
	wantPath := filepath.Join(tmp, "demo")
	if resp.ProjectPath != wantPath {
		t.Errorf("project_path: want %q, got %q", wantPath, resp.ProjectPath)
	}
	if resp.ProjectName != "demo" {
		t.Errorf("project_name: want %q, got %q", "demo", resp.ProjectName)
	}
	if resp.MilestoneCount != len(sampleState().Milestones) {
		t.Errorf("milestone_count: want %d, got %d",
			len(sampleState().Milestones), resp.MilestoneCount)
	}
	wantEpics := 0
	for _, m := range sampleState().Milestones {
		wantEpics += len(m.Epics)
	}
	if resp.EpicCount != wantEpics {
		t.Errorf("epic_count: want %d, got %d", wantEpics, resp.EpicCount)
	}

	// Project dir must exist.
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("project dir missing: %v", err)
	}
	// .gemba/workspace.toml must exist + carry the project name.
	ws, err := os.ReadFile(filepath.Join(wantPath, ".gemba", "workspace.toml"))
	if err != nil {
		t.Fatalf("read workspace.toml: %v", err)
	}
	if !bytes.Contains(ws, []byte(`name = "demo"`)) {
		t.Errorf("workspace.toml missing project name; body=%q", ws)
	}
	// docs/project.md must exist.
	if _, err := os.Stat(filepath.Join(wantPath, "docs", "project.md")); err != nil {
		t.Fatalf("project.md missing: %v", err)
	}

	// Calls expected: git init, bd init, 1×bd create (milestone),
	// 1×bd create (epic), 2×bd create (beads), git add, git commit.
	wantBinaries := map[string]int{"git": 0, "bd": 0}
	for _, c := range stub.calls {
		wantBinaries[c.Bin]++
	}
	if wantBinaries["bd"] < 4 {
		t.Errorf("expected ≥4 bd calls (init + 4 creates), got %d (calls=%+v)", wantBinaries["bd"], stub.calls)
	}
	// Find the git-commit call and verify its message.
	foundCommit := false
	for _, c := range stub.calls {
		if c.Bin != "git" {
			continue
		}
		// commit args: [-c user.name=… -c user.email=… commit -m "feat: …"]
		joined := strings.Join(c.Args, " ")
		if strings.Contains(joined, "commit") && strings.Contains(joined, "feat: initial project bootstrap") {
			foundCommit = true
			break
		}
	}
	if !foundCommit {
		t.Errorf("expected `git commit -m 'feat: initial project bootstrap'`; calls=%+v", stub.calls)
	}
}

// ─── Failure-path rollback tests ────────────────────────────────────

func TestRatify_DirAlreadyExists_NeverDeletes(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "demo")
	// Pre-create the dir with a sentinel file so we can detect any
	// rollback that would (incorrectly) wipe it.
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	sentinel := filepath.Join(target, "DO-NOT-TOUCH")
	if err := os.WriteFile(sentinel, []byte("preexisting"), 0o644); err != nil {
		t.Fatalf("sentinel: %v", err)
	}

	rfr := NewRatifier(RatifierConfig{
		DefaultDirOverride: tmp,
		Runner:             newStubRunner().run,
	})
	_, err := rfr.Ratify(context.Background(), sampleState())
	if err == nil {
		t.Fatalf("Ratify: want error for pre-existing dir")
	}
	re, ok := err.(*RatifyError)
	if !ok {
		t.Fatalf("Ratify: want *RatifyError, got %T: %v", err, err)
	}
	if re.Code != "dir_exists" {
		t.Errorf("code: want dir_exists, got %q", re.Code)
	}
	if re.Step != 2 {
		t.Errorf("step: want 2, got %d", re.Step)
	}
	// Crucially, the sentinel must still be intact — the transaction
	// must NEVER touch a pre-existing dir.
	if data, err := os.ReadFile(sentinel); err != nil {
		t.Fatalf("sentinel must survive: %v", err)
	} else if string(data) != "preexisting" {
		t.Errorf("sentinel mutated: %q", data)
	}
}

func TestRatify_BeadsInitFails_RollsBackDir(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "demo")
	stub := newStubRunner()
	stub.bdInitOK = false

	rfr := NewRatifier(RatifierConfig{
		DefaultDirOverride: tmp,
		Runner:             stub.run,
	})
	_, err := rfr.Ratify(context.Background(), sampleState())
	if err == nil {
		t.Fatalf("Ratify: want error for failed bd init")
	}
	re, ok := err.(*RatifyError)
	if !ok {
		t.Fatalf("Ratify: want *RatifyError, got %T", err)
	}
	if re.Step != 5 || re.Code != "beads_init_failed" {
		t.Errorf("want step 5/beads_init_failed, got step %d/%s", re.Step, re.Code)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Errorf("project dir must be rolled back; stat=%v", err)
	}
}

func TestRatify_MilestoneCreateFails_RollsBackDir(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "demo")
	stub := newStubRunner()
	stub.failOn = func(c stubCall) error {
		if c.Bin == "bd" && len(c.Args) > 0 && c.Args[0] == "create" {
			return errors.New("simulated milestone-create failure")
		}
		return nil
	}

	rfr := NewRatifier(RatifierConfig{
		DefaultDirOverride: tmp,
		Runner:             stub.run,
	})
	_, err := rfr.Ratify(context.Background(), sampleState())
	if err == nil {
		t.Fatalf("Ratify: want error for failed bd create")
	}
	re, ok := err.(*RatifyError)
	if !ok {
		t.Fatalf("want *RatifyError, got %T", err)
	}
	if re.Step != 6 || re.Code != "milestone_create_failed" {
		t.Errorf("want step 6/milestone_create_failed, got step %d/%s", re.Step, re.Code)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Errorf("project dir must be rolled back; stat=%v", err)
	}
}

func TestRatify_CycleDetected_AbortsAndRollsBack(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "demo")
	stub := newStubRunner()
	rfr := NewRatifier(RatifierConfig{
		DefaultDirOverride: tmp,
		Runner:             stub.run,
	})

	// Build a cyclic plan: bead 0 depends on bead 1; bead 1 depends
	// on bead 0. Cycle detection at step 8a must abort.
	state := sampleState()
	state.Milestones[0].Epics[0].Beads = []DraftBead{
		{Title: "B0", Type: "task", DependsOnRefs: []string{"milestone:0/epic:0/bead:1"}},
		{Title: "B1", Type: "task", DependsOnRefs: []string{"milestone:0/epic:0/bead:0"}},
	}

	_, err := rfr.Ratify(context.Background(), state)
	if err == nil {
		t.Fatalf("Ratify: want cycle detection error")
	}
	re, ok := err.(*RatifyError)
	if !ok {
		t.Fatalf("want *RatifyError, got %T", err)
	}
	if re.Code != "cycle_detected" {
		t.Errorf("want cycle_detected, got %q (msg=%q)", re.Code, re.Message)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Errorf("project dir must be rolled back; stat=%v", err)
	}
	// Crucially: NO `bd dep add` calls should have been issued — we
	// detect the cycle in-process before applying any edges.
	for _, c := range stub.calls {
		if c.Bin == "bd" && len(c.Args) > 0 && c.Args[0] == "dep" {
			t.Errorf("bd dep add must NOT run when a cycle is detected; got %v", c.Args)
		}
	}
}

func TestRatify_UnknownDepRef_FailsWithoutEdges(t *testing.T) {
	tmp := t.TempDir()
	stub := newStubRunner()
	rfr := NewRatifier(RatifierConfig{
		DefaultDirOverride: tmp,
		Runner:             stub.run,
	})
	state := sampleState()
	state.Milestones[0].Epics[0].Beads[0].DependsOnRefs = []string{"milestone:9/epic:9/bead:9"}
	_, err := rfr.Ratify(context.Background(), state)
	if err == nil {
		t.Fatalf("want error for unknown dep ref")
	}
	re, ok := err.(*RatifyError)
	if !ok {
		t.Fatalf("want *RatifyError, got %T", err)
	}
	if re.Code != "dep_resolve_failed" {
		t.Errorf("want dep_resolve_failed, got %q", re.Code)
	}
}

func TestRatify_InvalidProjectName_NoDirCreated(t *testing.T) {
	tmp := t.TempDir()
	stub := newStubRunner()
	rfr := NewRatifier(RatifierConfig{
		DefaultDirOverride: tmp,
		Runner:             stub.run,
	})

	state := sampleState()
	state.ProjectName = ""
	_, err := rfr.Ratify(context.Background(), state)
	if err == nil {
		t.Fatalf("want validation error")
	}
	re, ok := err.(*RatifyError)
	if !ok {
		t.Fatalf("want *RatifyError, got %T", err)
	}
	if re.Code != "validation_failed" {
		t.Errorf("want validation_failed, got %q", re.Code)
	}
	// Stub runner must not have been invoked.
	if len(stub.calls) > 0 {
		t.Errorf("expected zero subprocess calls on validation failure; got %+v", stub.calls)
	}

	state.ProjectName = "has spaces"
	if _, err := rfr.Ratify(context.Background(), state); err == nil {
		t.Fatalf("want validation error for spaces in name")
	}
}

// ─── Cycle-detector unit tests ──────────────────────────────────────

func TestDetectCycle_Acyclic(t *testing.T) {
	edges := []depEdge{
		{from: "A", to: "B"},
		{from: "B", to: "C"},
		{from: "A", to: "C"},
	}
	if got := detectCycle(edges); got != nil {
		t.Errorf("acyclic graph reported cycle: %v", got)
	}
}

func TestDetectCycle_SimpleTwoNode(t *testing.T) {
	edges := []depEdge{
		{from: "A", to: "B"},
		{from: "B", to: "A"},
	}
	got := detectCycle(edges)
	if got == nil {
		t.Fatalf("expected cycle, got nil")
	}
	// Path should reference both A and B.
	joined := strings.Join(got, "->")
	if !strings.Contains(joined, "A") || !strings.Contains(joined, "B") {
		t.Errorf("cycle path missing nodes: %v", got)
	}
}

func TestDetectCycle_ThreeNodeCycle(t *testing.T) {
	edges := []depEdge{
		{from: "A", to: "B"},
		{from: "B", to: "C"},
		{from: "C", to: "A"},
	}
	if got := detectCycle(edges); got == nil {
		t.Fatalf("expected cycle, got nil")
	}
}

// ─── Wire-shape sanity check ────────────────────────────────────────

// TestRatify_HTTPRoute exercises the full HTTP path: /start → /turn →
// /ratify with a stub ratifier so we verify the handler wires the
// request through end-to-end (including the nonce gate).
func TestRatify_HTTPRoute(t *testing.T) {
	tmp := t.TempDir()
	stub := newStubRunner()
	r, _ := newNewProjectRouter(t, NewRatifier(RatifierConfig{
		DefaultDirOverride: tmp,
		Runner:             stub.run,
	}))

	// /start
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, newProjectReq(t, http.MethodPost, "/api/v1/newproject/start", map[string]any{}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("/start: %d body=%q", rec.Code, rec.Body.String())
	}
	var startResp startNewProjectResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &startResp)

	// /ratify with a hand-crafted state matching the live session's
	// turn count (currently 0 — the test skips /turn). The session's
	// state has no project name; we can't ratify it as-is, so seed a
	// state at turn 0 with a name.
	state := sampleState()
	state.Turn = 0
	body := ratifyRequest{State: state}
	req := newProjectReq(t, http.MethodPost,
		"/api/v1/newproject/"+startResp.SessionID+"/ratify", body)
	req.Header.Set(ConfirmHeader, "test-nonce-ratify-1")
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/ratify: want 200, got %d body=%q", rec.Code, rec.Body.String())
	}
	var ratifyResp RatifyResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &ratifyResp); err != nil {
		t.Fatalf("decode /ratify: %v", err)
	}
	if !strings.HasSuffix(ratifyResp.ProjectPath, "/demo") {
		t.Errorf("project_path: want suffix /demo, got %q", ratifyResp.ProjectPath)
	}
}

func TestRatify_StaleStateConflict(t *testing.T) {
	tmp := t.TempDir()
	stub := newStubRunner()
	r, store := newNewProjectRouter(t, NewRatifier(RatifierConfig{
		DefaultDirOverride: tmp,
		Runner:             stub.run,
	}))

	sess, err := store.Start(context.Background())
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	// Mutate the session's live state so its Turn diverges from the
	// snapshot the SPA POSTs.
	mut := sess.State
	mut.Turn = 5
	if err := store.Update(context.Background(), sess.ID, mut); err != nil {
		t.Fatalf("update: %v", err)
	}

	// SPA POSTs an older snapshot (turn=0) — should 409.
	body := ratifyRequest{State: sampleState()} // sampleState has turn=0
	req := newProjectReq(t, http.MethodPost,
		"/api/v1/newproject/"+sess.ID+"/ratify", body)
	req.Header.Set(ConfirmHeader, "test-stale-nonce")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("want 409 for stale state, got %d body=%q", rec.Code, rec.Body.String())
	}
}

// ─── Default dir resolution ─────────────────────────────────────────

func TestResolveDefaultDir_FallsBackToBuiltinDefault(t *testing.T) {
	home := t.TempDir()
	got, err := resolveDefaultDir(home)
	if err != nil {
		t.Fatalf("resolveDefaultDir: %v", err)
	}
	want := filepath.Join(home, "gemba", "projects")
	if got != want {
		t.Errorf("default dir: want %q, got %q", want, got)
	}
}

func TestResolveDefaultDir_HonoursConfigToml(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".gemba"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cfg := []byte(`# gemba config
[projects]
default_dir = "~/work/projects"
`)
	if err := os.WriteFile(filepath.Join(home, ".gemba", "config.toml"), cfg, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	got, err := resolveDefaultDir(home)
	if err != nil {
		t.Fatalf("resolveDefaultDir: %v", err)
	}
	want := filepath.Join(home, "work", "projects")
	if got != want {
		t.Errorf("default dir: want %q, got %q", want, got)
	}
}

// ─── gm-root.17.13: lightweight /create + /onboarder/probe ──────────

// TestNewProject_Create_Light_HappyPath: POST /api/v1/newproject/create
// with just a name + description scaffolds a project with empty plan
// tree (no LLM, no Onboarder). Ratify response carries milestone_count
// = 0 / epic_count = 0 so the SPA's handoff knows the project is bare.
func TestNewProject_Create_Light_HappyPath(t *testing.T) {
	tmp := t.TempDir()
	stub := newStubRunner()
	r, _ := newNewProjectRouter(t, NewRatifier(RatifierConfig{
		DefaultDirOverride: tmp,
		Runner:             stub.run,
	}))

	body := createRequest{
		ProjectName: "lightproj",
		Description: "manually managed",
	}
	req := newProjectReq(t, http.MethodPost, "/api/v1/newproject/create", body)
	req.Header.Set(ConfirmHeader, "test-nonce-light-1")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("/create: want 200, got %d body=%q", rec.Code, rec.Body.String())
	}
	var resp RatifyResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ProjectName != "lightproj" {
		t.Errorf("project_name: got %q", resp.ProjectName)
	}
	if resp.MilestoneCount != 0 || resp.EpicCount != 0 {
		t.Errorf("counts: want 0/0, got %d/%d", resp.MilestoneCount, resp.EpicCount)
	}
	wantPath := filepath.Join(tmp, "lightproj")
	if resp.ProjectPath != wantPath {
		t.Errorf("project_path: want %q, got %q", wantPath, resp.ProjectPath)
	}
	// Project dir + .gemba/workspace.toml + docs/project.md all exist.
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("project dir missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wantPath, ".gemba", "workspace.toml")); err != nil {
		t.Fatalf("workspace.toml missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wantPath, "docs", "project.md")); err != nil {
		t.Fatalf("project.md missing: %v", err)
	}
	// No bd-create calls should have fired (empty Milestones[]). Only
	// `bd init` is expected.
	bdCreates := 0
	for _, c := range stub.calls {
		if c.Bin == "bd" && len(c.Args) >= 1 && c.Args[0] == "create" {
			bdCreates++
		}
	}
	if bdCreates != 0 {
		t.Errorf("expected 0 bd-create calls for empty plan, got %d (calls=%+v)",
			bdCreates, stub.calls)
	}
}

// TestNewProject_Create_RequiresName: empty / whitespace project_name
// returns 400.
func TestNewProject_Create_RequiresName(t *testing.T) {
	r, _ := newNewProjectRouter(t, NewRatifier(RatifierConfig{
		DefaultDirOverride: t.TempDir(),
		Runner:             newStubRunner().run,
	}))
	for _, name := range []string{"", "   "} {
		body := createRequest{ProjectName: name}
		req := newProjectReq(t, http.MethodPost, "/api/v1/newproject/create", body)
		req.Header.Set(ConfirmHeader, "test-nonce-create-noname-"+name)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("name=%q: want 400, got %d body=%q",
				name, rec.Code, rec.Body.String())
		}
	}
}

// TestNewProject_Create_RequiresNonce: /create writes to disk, so the
// confirm-nonce gate must be enforced.
func TestNewProject_Create_RequiresNonce(t *testing.T) {
	r, _ := newNewProjectRouter(t, NewRatifier(RatifierConfig{
		DefaultDirOverride: t.TempDir(),
		Runner:             newStubRunner().run,
	}))
	body := createRequest{ProjectName: "noNonce"}
	req := newProjectReq(t, http.MethodPost, "/api/v1/newproject/create", body)
	// Intentionally no ConfirmHeader.
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Errorf("expected non-200 without confirm nonce, got 200 body=%q", rec.Body.String())
	}
}

// TestOnboarderProbe_Available: stub turner returns nil from Probe →
// {available: true}.
func TestOnboarderProbe_Available(t *testing.T) {
	r, _ := newNewProjectRouter(t, nil)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/onboarder/probe", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	var resp onboarderProbeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Available {
		t.Errorf("expected available=true, got %+v", resp)
	}
}

// fakeUnavailableTurner returns a probe error so the test can assert
// the unavailable branch.
type fakeUnavailableTurner struct{}

func (fakeUnavailableTurner) Greeting() string { return "" }
func (fakeUnavailableTurner) Turn(_ context.Context, in NewProjectState, _ string, _ map[string]interface{}) (NewProjectState, string, error) {
	return in, "", nil
}
func (fakeUnavailableTurner) Probe(_ context.Context) error {
	return errors.New("No LLM client configured. Export ANTHROPIC_API_KEY in your environment, or set [llm] in ~/.gemba/config.toml, before starting a New project conversation.")
}

// TestOnboarderProbe_Unavailable: turner Probe returns an error →
// {available: false, reason: <message>}, still HTTP 200.
func TestOnboarderProbe_Unavailable(t *testing.T) {
	r := NewRouter(config.ServeConfig{}, fakeSPA(), nil)
	t.Cleanup(r.Close)
	r.AttachNewProject(NewMemoryNewProjectStore(), fakeUnavailableTurner{}, nil)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/onboarder/probe", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	var resp onboarderProbeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Available {
		t.Errorf("expected available=false, got %+v", resp)
	}
	if !strings.Contains(resp.Reason, "No LLM client configured") {
		t.Errorf("expected reason to carry diagnostic, got %q", resp.Reason)
	}
}

// ─── gm-root.24: FTUX seed (agents.toml + personas + CLAUDE.md) ─────

// TestRatify_FTUXSeed_VirginRatify asserts a fresh ratify lands the
// FTUX seed files: .gemba/agents.toml, .gemba/personas/<3>.toml,
// CLAUDE.md, and AGENTS.md at the project root.
func TestRatify_FTUXSeed_VirginRatify(t *testing.T) {
	tmp := t.TempDir()
	stub := newStubRunner()
	rfr := NewRatifier(RatifierConfig{
		DefaultDirOverride: tmp,
		Runner:             stub.run,
	})

	resp, err := rfr.Ratify(context.Background(), sampleState())
	if err != nil {
		t.Fatalf("Ratify: %v", err)
	}
	if len(resp.SeedWarnings) != 0 {
		t.Errorf("expected zero seed warnings on virgin ratify; got %v", resp.SeedWarnings)
	}

	root := resp.ProjectPath

	// .gemba/agents.toml — must exist + carry the README's claude shape.
	agentsBody, err := os.ReadFile(filepath.Join(root, ".gemba", "agents.toml"))
	if err != nil {
		t.Fatalf("read agents.toml: %v", err)
	}
	if !bytes.Contains(agentsBody, []byte(`name           = "claude"`)) {
		t.Errorf("agents.toml missing claude entry; body=%q", agentsBody)
	}
	if !bytes.Contains(agentsBody, []byte(`intra_parallel = true`)) {
		t.Errorf("agents.toml missing intra_parallel=true; body=%q", agentsBody)
	}
	if !bytes.Contains(agentsBody, []byte(`max_parallel   = 4`)) {
		t.Errorf("agents.toml missing max_parallel=4; body=%q", agentsBody)
	}

	// .gemba/personas/ — must contain all three bundled TOMLs.
	for _, name := range []string{
		"deployment-engineer.toml",
		"documentarian.toml",
		"project-manager.toml",
	} {
		p := filepath.Join(root, ".gemba", "personas", name)
		info, err := os.Stat(p)
		if err != nil {
			t.Errorf("persona %s missing: %v", name, err)
			continue
		}
		if info.Size() < 100 {
			t.Errorf("persona %s suspiciously small (%d bytes)", name, info.Size())
		}
	}

	// CLAUDE.md — must exist at the project root + interpolate name.
	claudeBody, err := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}
	if !bytes.Contains(claudeBody, []byte("# demo")) {
		t.Errorf("CLAUDE.md missing project-name header; body=%q", claudeBody)
	}
	if !bytes.Contains(claudeBody, []byte("## Conventions")) {
		t.Errorf("CLAUDE.md missing Conventions section; body=%q", claudeBody)
	}
	if !bytes.Contains(claudeBody, []byte("GitNexus is the default backend")) {
		t.Errorf("CLAUDE.md missing source-analysis guidance; body=%q", claudeBody)
	}
	if !bytes.Contains(claudeBody, []byte("Beads is the source of truth")) {
		t.Errorf("CLAUDE.md missing Beads guidance; body=%q", claudeBody)
	}

	// AGENTS.md — Codex-friendly setup file mirrors the Gemba runtime context.
	agentsMD, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		t.Fatalf("read AGENTS.md: %v", err)
	}
	if !bytes.Contains(agentsMD, []byte("# demo Agent Instructions")) {
		t.Errorf("AGENTS.md missing project-name header; body=%q", agentsMD)
	}
	if !bytes.Contains(agentsMD, []byte("gemba")) || !bytes.Contains(agentsMD, []byte("GitNexus")) {
		t.Errorf("AGENTS.md missing MCP/source-analysis guidance; body=%q", agentsMD)
	}
}

// TestRatify_FTUXSeed_IdempotentSkip asserts the seed steps do not
// clobber pre-existing files. Each seed sub-step is exercised with a
// pre-seeded directory (via direct seedFTUXFiles invocation, not a full
// ratify, since the existing transaction rejects pre-existing project
// dirs at step 2).
func TestRatify_FTUXSeed_IdempotentSkip(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "demo")

	// Seed pre-existing operator-curated content.
	if err := os.MkdirAll(filepath.Join(target, ".gemba", "personas"), 0o755); err != nil {
		t.Fatalf("seed dir: %v", err)
	}
	preAgents := []byte(`# operator-managed agents.toml — DO NOT TOUCH`)
	if err := os.WriteFile(filepath.Join(target, ".gemba", "agents.toml"), preAgents, 0o644); err != nil {
		t.Fatalf("seed agents: %v", err)
	}
	preClaude := []byte(`# operator-managed CLAUDE.md — DO NOT TOUCH`)
	if err := os.WriteFile(filepath.Join(target, "CLAUDE.md"), preClaude, 0o644); err != nil {
		t.Fatalf("seed claude: %v", err)
	}
	preAgentsMD := []byte(`# operator-managed AGENTS.md — DO NOT TOUCH`)
	if err := os.WriteFile(filepath.Join(target, "AGENTS.md"), preAgentsMD, 0o644); err != nil {
		t.Fatalf("seed AGENTS.md: %v", err)
	}
	prePersona := []byte(`id = "operator-curated"`)
	if err := os.WriteFile(
		filepath.Join(target, ".gemba", "personas", "operator-curated.toml"),
		prePersona, 0o644); err != nil {
		t.Fatalf("seed persona: %v", err)
	}

	// Invoke the seed step directly. It should skip every sub-step.
	rfr := NewRatifier(RatifierConfig{
		DefaultDirOverride: tmp,
		Runner:             newStubRunner().run,
	}).(*ratifier)
	warnings := rfr.seedFTUXFiles(target, "demo")
	if len(warnings) != 0 {
		t.Errorf("expected no warnings on pre-seeded project; got %v", warnings)
	}

	// Pre-existing files must be byte-identical.
	if got, _ := os.ReadFile(filepath.Join(target, ".gemba", "agents.toml")); !bytes.Equal(got, preAgents) {
		t.Errorf("agents.toml mutated; got %q want %q", got, preAgents)
	}
	if got, _ := os.ReadFile(filepath.Join(target, "CLAUDE.md")); !bytes.Equal(got, preClaude) {
		t.Errorf("CLAUDE.md mutated; got %q want %q", got, preClaude)
	}
	if got, _ := os.ReadFile(filepath.Join(target, "AGENTS.md")); !bytes.Equal(got, preAgentsMD) {
		t.Errorf("AGENTS.md mutated; got %q want %q", got, preAgentsMD)
	}
	// The non-empty personas dir must be untouched: only the operator's
	// file should remain — the bundled three must NOT have been added.
	entries, err := os.ReadDir(filepath.Join(target, ".gemba", "personas"))
	if err != nil {
		t.Fatalf("read personas dir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "operator-curated.toml" {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("personas dir mutated; entries=%v", names)
	}
}

// TestRatify_FTUXSeed_EmptyPersonasDir asserts that a personas/
// directory which exists but is empty IS populated (the "empty"
// branch of the empty-or-absent check).
func TestRatify_FTUXSeed_EmptyPersonasDir(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "demo")
	if err := os.MkdirAll(filepath.Join(target, ".gemba", "personas"), 0o755); err != nil {
		t.Fatalf("seed dir: %v", err)
	}

	rfr := NewRatifier(RatifierConfig{
		DefaultDirOverride: tmp,
		Runner:             newStubRunner().run,
	}).(*ratifier)
	if w := rfr.seedPersonas(target); w != "" {
		t.Errorf("expected no warning, got %q", w)
	}

	for _, name := range []string{
		"deployment-engineer.toml",
		"documentarian.toml",
		"project-manager.toml",
	} {
		if _, err := os.Stat(filepath.Join(target, ".gemba", "personas", name)); err != nil {
			t.Errorf("persona %s missing after empty-dir seed: %v", name, err)
		}
	}
}

// TestRatify_FTUXSeed_StagedAndCommitted asserts the seed files land
// before step 10's `git add -A` so they are part of the initial
// commit rather than left as untracked working-tree files.
func TestRatify_FTUXSeed_StagedAndCommitted(t *testing.T) {
	tmp := t.TempDir()
	stub := newStubRunner()
	rfr := NewRatifier(RatifierConfig{
		DefaultDirOverride: tmp,
		Runner:             stub.run,
	})
	if _, err := rfr.Ratify(context.Background(), sampleState()); err != nil {
		t.Fatalf("Ratify: %v", err)
	}

	// Find the index of the bd-init call (step 5) and the git add -A
	// call (step 10) in the runner log. agents.toml / personas /
	// CLAUDE.md are written between them. We assert the writes happened
	// AFTER bd-init (so beads is initialised first) and the git-add was
	// the last filesystem-touching call before commit.
	addIdx, commitIdx := -1, -1
	for i, c := range stub.calls {
		if c.Bin != "git" {
			continue
		}
		joined := strings.Join(c.Args, " ")
		if strings.Contains(joined, "add -A") {
			addIdx = i
		}
		if strings.Contains(joined, "commit") && strings.Contains(joined, "feat: initial project bootstrap") {
			commitIdx = i
		}
	}
	if addIdx < 0 || commitIdx < 0 {
		t.Fatalf("missing git add / commit; calls=%+v", stub.calls)
	}
	if addIdx >= commitIdx {
		t.Errorf("git add must precede commit; addIdx=%d commitIdx=%d", addIdx, commitIdx)
	}
}
