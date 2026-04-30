package native

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/adapter/native/agents"
	"github.com/GembaCore/gemba-core/internal/adapter/native/backend"
)

// fakeBackend is the in-memory backend.Backend used by StartSession
// tests. Every method records its inputs so assertions can verify
// the adaptor called the right things.
type fakeBackend struct {
	mu    sync.Mutex
	name  string
	panes map[string]backend.Pane
	next  int

	spawnCalls []backend.SpawnSpec
	killCalls  []string
	spawnErr   error
}

func newFakeBackend() *fakeBackend {
	return &fakeBackend{
		name:  "fake",
		panes: make(map[string]backend.Pane),
	}
}

func (f *fakeBackend) Name() string { return f.name }

func (f *fakeBackend) ListPanes(context.Context) ([]backend.Pane, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]backend.Pane, 0, len(f.panes))
	for _, p := range f.panes {
		out = append(out, p)
	}
	return out, nil
}

func (f *fakeBackend) SpawnPane(_ context.Context, spec backend.SpawnSpec) (backend.Pane, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.spawnCalls = append(f.spawnCalls, spec)
	if f.spawnErr != nil {
		return backend.Pane{}, f.spawnErr
	}
	f.next++
	id := fmt.Sprintf("%%%d", f.next)
	p := backend.Pane{
		ID:    id,
		Cwd:   spec.Cwd,
		Title: spec.Title,
	}
	f.panes[id] = p
	return p, nil
}

func (f *fakeBackend) SendKeys(context.Context, string, string) error { return nil }

func (f *fakeBackend) CapturePane(context.Context, string, int) (string, error) { return "", nil }

func (f *fakeBackend) Kill(_ context.Context, paneID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.killCalls = append(f.killCalls, paneID)
	delete(f.panes, paneID)
	return nil
}

// defaultRegistry is the fixture registry tests use.
func defaultRegistry() agents.Registry {
	return agents.Registry{
		Agents: []agents.AgentType{
			{
				Name: "claude", Binary: "claude", Model: "claude-opus-4-7",
				Preamble: agents.PreambleClaudeMD, Hooks: agents.HookClaudeCode,
			},
			{
				Name: "shell-only", Binary: "zsh",
				Preamble: agents.PreambleStdoutBanner, Hooks: agents.HookPromptCommand,
			},
		},
	}
}

// initRepo sets up a throwaway git repo for worktree provisioning.
func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "test")
	run("commit", "--allow-empty", "-m", "initial")
	return dir
}

func freshPrompt(beadID string, extras map[string]any) core.SessionPrompt {
	ext := map[string]any{
		extKeyBeadID:    beadID,
		extKeyAgentType: "claude",
	}
	for k, v := range extras {
		ext[k] = v
	}
	return core.SessionPrompt{Extension: ext}
}

func TestStartSessionHappyPath(t *testing.T) {
	repo := initRepo(t)
	fb := newFakeBackend()

	p := NewWithConfig(Config{
		Backend:  fb,
		Registry: defaultRegistry(),
		RepoRoot: repo,
	})

	sess, err := p.StartSession(context.Background(), "assign-1", freshPrompt("gm-foo", nil))
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if sess.ID == "" {
		t.Error("session id empty")
	}
	if sess.AssignmentID != "assign-1" {
		t.Errorf("assignment id: got %q", sess.AssignmentID)
	}
	// StartSession's initial status is Initializing — the adaptor
	// transitions to Working when the first gemba-state signal lands
	// via the bridge (gm-cdph).
	if sess.Status != core.SessionInitializing {
		t.Errorf("status: got %q, want %q", sess.Status, core.SessionInitializing)
	}
	if sess.ProviderMetadata["backend"] != "fake" {
		t.Errorf("metadata.backend: got %v", sess.ProviderMetadata["backend"])
	}
	if sess.ProviderMetadata["bead_id"] != "gm-foo" {
		t.Errorf("metadata.bead_id: got %v", sess.ProviderMetadata["bead_id"])
	}
	wt, _ := sess.ProviderMetadata["worktree"].(string)
	if wt == "" || !strings.HasSuffix(wt, "bead-gm-foo") {
		t.Errorf("worktree metadata wrong: %q", wt)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Errorf("worktree not on disk: %v", err)
	}

	// Spawn was called with the right env + command shape.
	if len(fb.spawnCalls) != 1 {
		t.Fatalf("spawn call count: %d", len(fb.spawnCalls))
	}
	spec := fb.spawnCalls[0]
	if spec.Env["GEMBA_SESSION_ID"] != sess.ID {
		t.Errorf("env GEMBA_SESSION_ID: got %q want %q", spec.Env["GEMBA_SESSION_ID"], sess.ID)
	}
	if spec.Env["GEMBA_AGENT_TYPE"] != "claude" {
		t.Errorf("env GEMBA_AGENT_TYPE: %q", spec.Env["GEMBA_AGENT_TYPE"])
	}
	if len(spec.Command) == 0 || spec.Command[0] != "claude" {
		t.Errorf("command: %v", spec.Command)
	}
}

func TestStartSessionNonceIdempotent(t *testing.T) {
	repo := initRepo(t)
	fb := newFakeBackend()
	p := NewWithConfig(Config{Backend: fb, Registry: defaultRegistry(), RepoRoot: repo})

	first, err := p.StartSession(context.Background(), "a1", freshPrompt("gm-foo", map[string]any{extKeyNonce: "n1"}))
	if err != nil {
		t.Fatal(err)
	}
	second, err := p.StartSession(context.Background(), "a1", freshPrompt("gm-foo", map[string]any{extKeyNonce: "n1"}))
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Errorf("nonce replay returned new session: %q vs %q", first.ID, second.ID)
	}
	if len(fb.spawnCalls) != 1 {
		t.Errorf("nonce replay re-spawned: %d spawn calls", len(fb.spawnCalls))
	}
}

func TestStartSessionRejectsUnknownAgent(t *testing.T) {
	fb := newFakeBackend()
	p := NewWithConfig(Config{Backend: fb, Registry: defaultRegistry(), RepoRoot: initRepo(t)})
	_, err := p.StartSession(context.Background(), "a1", freshPrompt("gm-foo", map[string]any{extKeyAgentType: "martian"}))
	if err == nil {
		t.Fatal("want error")
	}
	var aerr *core.AdaptorError
	if !errors.As(err, &aerr) || aerr.Kind != core.KindValidation {
		t.Errorf("want KindValidation, got %T: %v", err, err)
	}
}

func TestStartSessionRequiresBeadID(t *testing.T) {
	p := NewWithConfig(Config{Backend: newFakeBackend(), Registry: defaultRegistry(), RepoRoot: initRepo(t)})
	_, err := p.StartSession(context.Background(), "a1", core.SessionPrompt{
		Extension: map[string]any{extKeyAgentType: "claude"},
	})
	var aerr *core.AdaptorError
	if !errors.As(err, &aerr) || aerr.Kind != core.KindValidation {
		t.Errorf("want KindValidation, got %T: %v", err, err)
	}
}

func TestStartSessionBackendErrorSurfaces(t *testing.T) {
	fb := newFakeBackend()
	fb.spawnErr = errors.New("mock spawn failure")
	p := NewWithConfig(Config{Backend: fb, Registry: defaultRegistry(), RepoRoot: initRepo(t)})
	_, err := p.StartSession(context.Background(), "a1", freshPrompt("gm-foo", nil))
	if err == nil {
		t.Fatal("want error on spawn failure")
	}
	if !strings.Contains(err.Error(), "spawn pane") {
		t.Errorf("error should mention spawn: %v", err)
	}
	var aerr *core.AdaptorError
	if !errors.As(err, &aerr) || aerr.Kind != core.KindProcessFailed {
		t.Errorf("want KindProcessFailed, got %T (kind=%v)", err, aerr)
	}
}

func TestStartSessionRespectsPreResolvedWorkspace(t *testing.T) {
	fb := newFakeBackend()
	pre := filepath.Join(t.TempDir(), "pre-worktree")
	if err := os.MkdirAll(pre, 0o755); err != nil {
		t.Fatal(err)
	}
	p := NewWithConfig(Config{Backend: fb, Registry: defaultRegistry()})
	sess, err := p.StartSession(context.Background(), "a1",
		freshPrompt("gm-foo", map[string]any{extKeyWorkspace: pre}))
	if err != nil {
		t.Fatal(err)
	}
	if sess.ProviderMetadata["worktree"] != pre {
		t.Errorf("pre-resolved workspace not honored: %v", sess.ProviderMetadata["worktree"])
	}
}

// TestStartSessionBeadAlreadyInFlightReturnsSentinel pins gm-e3.8 on
// the native adaptor. A second StartSession call against a bead that's
// already the AssignmentID of a non-terminal session must surface the
// core.ErrBeadAlreadyClaimed sentinel — the auto-dispatch daemon's
// inline-claim soft-skip path then picks the next candidate.
func TestStartSessionBeadAlreadyInFlightReturnsSentinel(t *testing.T) {
	repo := initRepo(t)
	fb := newFakeBackend()
	p := NewWithConfig(Config{Backend: fb, Registry: defaultRegistry(), RepoRoot: repo})

	// Anchor session.
	first, err := p.StartSession(context.Background(), "gm-foo", freshPrompt("gm-foo", nil))
	if err != nil {
		t.Fatalf("first StartSession: %v", err)
	}
	if first.ID == "" {
		t.Fatal("first session id empty")
	}

	// Second call referencing the SAME bead from a different
	// dispatcher (different assignmentID, fresh nonce) must lose
	// the inline-claim race.
	_, err = p.StartSession(context.Background(), "gm-foo-take2", freshPrompt("gm-foo", nil))
	if err == nil {
		t.Fatal("expected inline-claim race to reject the second StartSession")
	}
	if !core.IsAlreadyClaimedError(err) {
		t.Errorf("want ErrBeadAlreadyClaimed sentinel; got %T (%v)", err, err)
	}
	if core.AsAdaptorError(err) == nil {
		t.Errorf("error must be tagged AdaptorError; got %T", err)
	}
}

func TestStartSessionZeroConfigStillUnsupported(t *testing.T) {
	// Zero-config adaptor (no Backend) returns KindUnsupported so the
	// server can mount it without a live terminal backend and users
	// see a clear "not wired yet" message.
	p := New()
	_, err := p.StartSession(context.Background(), "a1", freshPrompt("gm-foo", nil))
	var aerr *core.AdaptorError
	if !errors.As(err, &aerr) || aerr.Kind != core.KindUnsupported {
		t.Errorf("want KindUnsupported, got %T: %v", err, err)
	}
}

// gm-hmqj: manual sessions have no bead — they carry gemba:kind="manual"
// + gemba:repository_id, run in the chosen repo's canonical worktree
// (no per-bead branch), and deliver SessionPrompt.Text as the first
// message verbatim.
func TestStartSessionManualMode(t *testing.T) {
	repo := initRepo(t)
	fb := newFakeBackend()
	p := NewWithConfig(Config{
		Backend:  fb,
		Registry: defaultRegistry(),
		RepoRoot: repo,
	})

	sess, err := p.StartSession(context.Background(), "manual-1730000000000-abcd",
		core.SessionPrompt{
			Text: "say hello",
			Extension: map[string]any{
				"gemba:kind":          "manual",
				"gemba:agent_type":    "claude",
				"gemba:repository_id": "gemba",
				"gemba:persona_id":    "coach",
			},
		})
	if err != nil {
		t.Fatalf("manual StartSession: %v", err)
	}
	if sess.AssignmentID != "manual-1730000000000-abcd" {
		t.Errorf("AssignmentID: got %q", sess.AssignmentID)
	}
	if sess.ProviderMetadata["kind"] != "manual" {
		t.Errorf("metadata.kind: got %v", sess.ProviderMetadata["kind"])
	}
	if sess.ProviderMetadata["repository_id"] != "gemba" {
		t.Errorf("metadata.repository_id: got %v", sess.ProviderMetadata["repository_id"])
	}
	if sess.ProviderMetadata["persona_id"] != "coach" {
		t.Errorf("metadata.persona_id: got %v", sess.ProviderMetadata["persona_id"])
	}
	if got, _ := sess.ProviderMetadata["bead_id"].(string); got != "" {
		t.Errorf("metadata.bead_id should be empty in manual mode; got %q", got)
	}
	if sess.ProviderMetadata["worktree"] != repo {
		t.Errorf("manual workspace should fall back to RepoRoot; got %v",
			sess.ProviderMetadata["worktree"])
	}
	if title, _ := sess.ProviderMetadata["pane_title"].(string); !strings.Contains(title, "manual") {
		t.Errorf("pane title should mention manual mode; got %q", title)
	}
}

func TestStartSessionManualModeRequiresRepository(t *testing.T) {
	p := NewWithConfig(Config{
		Backend:  newFakeBackend(),
		Registry: defaultRegistry(),
		RepoRoot: initRepo(t),
	})
	_, err := p.StartSession(context.Background(), "manual-1",
		core.SessionPrompt{
			Text: "hello",
			Extension: map[string]any{
				"gemba:kind":       "manual",
				"gemba:agent_type": "claude",
				// repository_id deliberately missing
			},
		})
	if err == nil {
		t.Fatal("manual mode without repository_id should be rejected")
	}
	var aerr *core.AdaptorError
	if !errors.As(err, &aerr) || aerr.Kind != core.KindValidation {
		t.Errorf("want KindValidation, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "repository_id") {
		t.Errorf("error should mention repository_id; got %v", err)
	}
}
