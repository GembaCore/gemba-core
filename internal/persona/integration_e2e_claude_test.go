// gm-twp2.2 — env-gated end-to-end smoke test that spawns a real
// Claude Code session via the native backend and verifies the
// emit_skill_output frame the bridge parses matches the schema.
//
// Orthogonal to the in-process loop test (integration_loop_test.go):
// that one fakes the spawn driver and proves "the wiring works"; this
// one runs against a real claude binary and proves "the Claude CLI
// still emits emit_skill_output frames in a shape the bridge can
// parse" — a contract with an external tool whose schema can drift.
//
// Three gates:
//   1. testing.Short()   — never in -short
//   2. claude on PATH    — operator-installed CLI
//   3. tmux on PATH      — native backend dependency
//   4. GEMBA_E2E_CLAUDE=1 in env — explicit opt-in (the model call
//      costs money + takes ~30s; we don't want CI burning a budget
//      when claude happens to be installed on the host)
//
// On success the test writes the observed model + token counts to
// the test log so the operator can see what just happened. On
// timeout the captured pane output is logged for debugging.

package persona_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/MikeBengtson/gemba/internal/adapter/native"
	"github.com/MikeBengtson/gemba/internal/adapter/native/agents"
	"github.com/MikeBengtson/gemba/internal/adapter/native/backend"
	"github.com/MikeBengtson/gemba/internal/adapter/native/bridge"
	"github.com/MikeBengtson/gemba/internal/core"
	corepersona "github.com/MikeBengtson/gemba/internal/core/persona"
	"github.com/MikeBengtson/gemba/internal/events"
	"github.com/MikeBengtson/gemba/internal/persona"
	"github.com/MikeBengtson/gemba/internal/skills/epic_order"
)

// gate_e2e_claude is the environment variable an operator sets to
// opt into the actual model call. Hard-coded here so the test is
// self-documenting — `GEMBA_E2E_CLAUDE=1 go test ./internal/persona/...`
// is the recipe.
const gateE2EClaude = "GEMBA_E2E_CLAUDE"

const e2eTimeout = 60 * time.Second

func TestE2E_ClaudeBinaryEmitsSkillOutput(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test; skipped under -short")
	}
	if os.Getenv(gateE2EClaude) != "1" {
		t.Skipf("e2e test; set %s=1 to run (incurs Anthropic API cost ~$0.05/run)", gateE2EClaude)
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skipf("claude binary not on PATH: %v", err)
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skipf("tmux binary not on PATH: %v", err)
	}

	// Build the workspace fixture: agents.toml + .gemba/personas/
	// + a worktrees-sibling for the spawn provisioner. Lives under
	// t.TempDir so cleanup is automatic.
	ws := t.TempDir()
	repoRoot := e2eRepoRoot(t)
	if err := writeAgentsTOML(filepath.Join(ws, ".gemba", "agents.toml")); err != nil {
		t.Fatal(err)
	}
	// Copy the workspace's actual project-manager.toml so the
	// persona's system_prompt + budget policy are the real ones,
	// not a fixture. Drift between fixture and prod is exactly
	// what this test is supposed to catch.
	pmSrc := filepath.Join(repoRoot, ".gemba", "personas", "project-manager.toml")
	pmDst := filepath.Join(ws, ".gemba", "personas", "project-manager.toml")
	if err := copyFile(pmSrc, pmDst); err != nil {
		t.Fatalf("copy persona fixture: %v", err)
	}

	// Native backend + adaptor.
	tmuxBackend, err := backend.NewTmux()
	if err != nil {
		t.Fatalf("NewTmux: %v", err)
	}
	registry, err := agents.Load(filepath.Join(ws, ".gemba", "agents.toml"))
	if err != nil {
		t.Fatalf("agents.Load: %v", err)
	}
	op := native.NewWithConfig(native.Config{
		Backend:  tmuxBackend,
		Registry: registry,
		RepoRoot: ws,
		// Provision worktrees inside the workspace so cleanup
		// only walks the temp dir.
		WorktreesDir: filepath.Join(ws, "worktrees"),
		Fanout:       bridge.NewFanout(),
	})

	// Persona consult machinery.
	auditDir := t.TempDir()
	d := persona.NewDispatcher(persona.NewAuditLog(auditDir),
		persona.WithWorkspaceDir(ws),
	)
	skill := epic_order.New()
	d.SetSpawnFunc(persona.NativeSpawn(op, "claude"))

	// Hub + bridge tail. The native adaptor publishes
	// OrchestrationEvents; persona.FanFromHub subscribes.
	hub := events.NewHub(events.Config{})
	defer hub.Close()
	ctx, cancel := context.WithTimeout(context.Background(), e2eTimeout)
	defer cancel()
	go persona.FanFromHub(ctx, d, hub)
	for i := 0; i < 100 && hub.SubscriberCount() == 0; i++ {
		time.Sleep(time.Millisecond)
	}
	subCh, err := op.Subscribe(ctx, core.SubscribeFilter{})
	if err != nil {
		t.Fatalf("op.Subscribe: %v", err)
	}
	hub.AttachOrchestrationStream(ctx, op.Describe().AdaptorID, subCh)

	// Load the persona from the workspace fixture (proves the
	// LoadFile path works against the real PM TOML).
	pm, err := corepersona.LoadFile(pmDst)
	if err != nil {
		t.Fatalf("load PM: %v", err)
	}

	// Begin a consult with a tiny EpicOrderInput. Two candidates,
	// no sprint context — the model has just enough to produce a
	// strategy + at least one recommendation + a summary. Sized
	// to stay under the persona's budget cap (0.25 USD).
	rawInput := json.RawMessage(`{
  "workspace": "gemba",
  "workspace_name": "Gemba",
  "as_of": "2026-04-26T12:00:00Z",
  "candidate_epics": [
    { "epic_id": "gm-1", "title": "ship the persona consult MVP", "ui_state": "on_deck" },
    { "epic_id": "gm-2", "title": "polish the SPA grid",          "ui_state": "on_deck" }
  ],
  "constraints": { "max_dollars": 0.25 }
}`)
	c, err := d.Begin(persona.BeginRequest{
		Persona:   pm,
		Skill:     skill,
		Workspace: "gemba",
		RawInput:  rawInput,
		Template: persona.TemplateValues{
			WorkspaceName: "Gemba",
			ProjectPrefix: "gm",
		},
	})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	t.Logf("e2e: consult %s spawned; waiting up to %s for emit_skill_output frame", c.ID, e2eTimeout)

	defer func() {
		// Best-effort teardown. The spawned pane may already be
		// dead (the agent exited cleanly); EndSession swallows
		// pane-already-gone errors so this is safe to call always.
		endCtx, endCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer endCancel()
		if _, err := op.EndSession(endCtx, c.ID, core.SessionEndCompleted, ""); err != nil {
			t.Logf("e2e teardown: EndSession(%s): %v", c.ID, err)
		}
	}()

	if err := d.MaybeSpawn(ctx, c); err != nil {
		t.Fatalf("MaybeSpawn: %v", err)
	}

	// Poll for at least one validated line to land. The model
	// typically emits the whole array in one tool call so we
	// won't see a trickle — first emit ≈ all emits.
	deadline := time.Now().Add(e2eTimeout)
	for {
		live, _ := d.Get(c.ID)
		if live != nil && len(live.ValidatedLines) > 0 {
			break
		}
		if time.Now().After(deadline) {
			// Capture the pane so the operator can see what the
			// model said before timing out.
			if pane := capturePane(op, c.ID); pane != "" {
				t.Logf("e2e timeout pane capture for %s:\n%s", c.ID, pane)
			}
			t.Fatalf("e2e timeout: no ValidatedLines after %s", e2eTimeout)
		}
		time.Sleep(250 * time.Millisecond)
	}

	live, _ := d.Get(c.ID)
	t.Logf("e2e: consult %s produced %d lines, %d errors",
		c.ID, len(live.ValidatedLines), len(live.LineErrors))

	// Schema check: at least one strategy line, at least one
	// recommendation OR summary. The model can elide a
	// recommendation when it defers everything; both skill-valid
	// terminal shapes are accepted here.
	hasStrategy := false
	hasTerminal := false
	for _, line := range live.ValidatedLines {
		switch line.(type) {
		case *epic_order.StrategyLine:
			hasStrategy = true
		case *epic_order.SummaryLine, *epic_order.RecommendationLine:
			hasTerminal = true
		}
	}
	if !hasStrategy {
		t.Errorf("e2e: model produced no strategy line; lines=%+v", live.ValidatedLines)
	}
	if !hasTerminal {
		t.Errorf("e2e: model produced no recommendation or summary line; lines=%+v", live.ValidatedLines)
	}

	// LineErrors is a soft signal — the model occasionally emits
	// a hallucinated field that strictUnmarshal rejects. Log them
	// rather than failing; the audit log preserves the raw JSON
	// for offline inspection.
	if len(live.LineErrors) > 0 {
		t.Logf("e2e: %d lines failed validation:", len(live.LineErrors))
		for _, le := range live.LineErrors {
			t.Logf("  idx=%d reason=%s raw=%s", le.Index, le.Reason, string(le.Raw))
		}
	}
}

// e2eRepoRoot is the e2e-test variant of integration_loop_test.go's
// findRepoRoot helper. Same logic, scoped name so the two test files
// can coexist in package persona_test without redeclaration.
func e2eRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func writeAgentsTOML(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	body := `# e2e fixture (gm-twp2.2). Single agent type pointing at the
# claude binary the test prereq verified is on PATH.

[[agent]]
name     = "claude"
binary   = "claude"
args     = []
model    = "claude-opus-4-7"
preamble = "claude_md"
hooks    = "claude_code"
`
	return os.WriteFile(path, []byte(body), 0o644)
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o644)
}

// capturePane reads the spawned pane's last 100 lines so a timeout
// surfaces what the operator would see in tmux. Best-effort — when
// the pane is already gone the call returns empty; the caller logs
// whatever it gets.
func capturePane(op *native.OrchestrationPlane, consultID string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	peek, err := op.PeekSession(ctx, consultID)
	if err != nil {
		return ""
	}
	return peek.Transcript
}
