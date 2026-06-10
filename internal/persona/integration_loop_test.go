// gm-twp2 integration test — proves the persona consult loop works
// end-to-end with real wiring (Dispatcher + events.Hub + FanFromHub +
// real epic_order skill validation), faking only the spawn driver
// (no tmux, no Claude binary). Catches the kind of wiring regression
// a unit test scoped to one component would miss.
//
// What's faked:
//   - Spawn driver: instead of launching Claude Code through the
//     native adaptor, the spawn func publishes a canned
//     SkillOutputEmitted event directly to the hub (the same event
//     shape the bridge translator emits when a real agent calls
//     emit_skill_output). This proves Dispatcher.Receive picks up
//     the lines through the FanFromHub subscriber.
//
// What's real:
//   - Dispatcher (Begin / Receive / Apply / Finish + audit log)
//   - epic_order.Skill (ValidateInput + ValidateOutputLine — the
//     model output goes through the real schema validator)
//   - corepersona.Persona (loaded from the workspace's actual
//     project-manager.toml so the prompt envelope mirrors prod)
//   - events.Hub + persona.FanFromHub
//   - core PersonaCanInvoke gate
//
// A separate, environment-gated test (filed as a follow-up bead)
// will spawn a real Claude Code session via the native backend when
// `claude` is on PATH; that test's value is "the Claude binary still
// emits emit_skill_output frames the bridge can parse" — orthogonal
// to this test's "the in-process loop wires up correctly".

package persona_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	corepersona "github.com/GembaCore/gemba-core/internal/core/persona"
	"github.com/GembaCore/gemba-core/internal/events"
	"github.com/GembaCore/gemba-core/internal/persona"
	"github.com/GembaCore/gemba-core/internal/skills/epic_order"
)

// findRepoRoot walks up from the test's source file to find the repo
// root (where .gemba/personas lives). Lets the test load the real
// project-manager.toml without hardcoding an absolute path.
func findRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// internal/persona/integration_loop_test.go → repo root is two
	// dirs up.
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func TestIntegration_ConsultLoop_BeginToFinish(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test; skipped under -short")
	}

	root := findRepoRoot(t)
	pmPath := filepath.Join(root, ".gemba", "personas", "project-manager.toml")
	pm, err := corepersona.LoadFile(pmPath)
	if err != nil {
		// The repo's PM persona is part of the integration test's
		// fixture surface; if it's missing the test environment is
		// the problem, not the code.
		t.Fatalf("loading PM persona from %s: %v", pmPath, err)
	}
	if !corepersona.PersonaCanInvoke(pm, epic_order.ID) {
		t.Fatalf("PM persona is not authorized for epic_order: %v", pm.Skills)
	}

	// Build the real machinery: dispatcher, audit log on a temp
	// dir, real skill, real hub, the bridge subscriber.
	auditLog := persona.NewAuditLog(t.TempDir())
	d := persona.NewDispatcher(auditLog,
		persona.WithWorkspaceDir(t.TempDir()),
		persona.WithClock(func() time.Time { return time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC) }),
	)
	skill := epic_order.New()
	if err := registerSkillForTest(t, d, skill); err != nil {
		t.Fatalf("registering skill: %v", err)
	}
	hub := events.NewHub(events.Config{})
	defer hub.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bridgeDone := make(chan struct{})
	go func() {
		persona.FanFromHub(ctx, d, hub)
		close(bridgeDone)
	}()
	// Wait for the FanFromHub subscriber to register before we
	// publish — Hub.Publish before Subscribe drops silently.
	for i := 0; i < 100 && hub.SubscriberCount() == 0; i++ {
		time.Sleep(time.Millisecond)
	}

	// Spawn func publishes a canned skill-output frame to the hub
	// keyed by consult.ID. Mirrors the bridge translator's payload
	// exactly so the FanFromHub → Dispatcher.Receive path runs
	// against the real wire shape.
	d.SetSpawnFunc(func(_ context.Context, c *persona.Consult) error {
		hub.Publish(events.GembaEvent{
			ID:        "ev-fixture-1",
			Kind:      events.SkillOutputEmitted,
			SessionID: c.ID, // consult ID is the session correlation key
			Payload: map[string]any{
				"skill_id":   epic_order.ID,
				"line_count": 3,
				"lines": []any{
					json.RawMessage(`{"type":"strategy","workspace":"gemba","as_of":"2026-04-26T12:00:00Z","model":"fixture","reasoning":"top-down by blocker depth","total_considered":2,"total_ranked":2}`),
					json.RawMessage(`{"type":"recommendation","rank":1,"epic_id":"gm-1","confidence":0.9,"rationale":"unblocks the most"}`),
					json.RawMessage(`{"type":"summary","ranked_count":1,"model":"fixture","confidence_overall":0.85,"advisor_cost_dollars":0.01,"tokens_in":100,"tokens_out":250,"latency_ms":1500}`),
				},
			},
		})
		return nil
	})

	// Begin a consult with a real EpicOrderInput shape. The
	// dispatcher's Skill.ValidateInput rejects malformed bodies, so
	// passing here proves the wire shape is in lockstep with the
	// types in internal/skills/epic_order/types.go.
	rawInput := json.RawMessage(`{
  "workspace": "gemba",
  "workspace_name": "Gemba",
  "as_of": "2026-04-26T12:00:00Z",
  "candidate_epics": [
    { "epic_id": "gm-1", "title": "first epic",  "ui_state": "on_deck" },
    { "epic_id": "gm-2", "title": "second epic", "ui_state": "on_deck" }
  ],
  "constraints": {}
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

	// Compose check: the rendered prompt must carry both the
	// persona's role and the skill's task fragment so the spawned
	// session reads its instructions on first turn.
	if c.Composed.System == "" {
		t.Errorf("Composed.System is empty; persona system_prompt did not splice in")
	}
	if c.Composed.User == "" {
		t.Errorf("Composed.User is empty; user message did not splice in")
	}

	// MaybeSpawn fires the canned hub publish — wait for the
	// FanFromHub subscriber to deliver the lines into Receive.
	if err := d.MaybeSpawn(ctx, c); err != nil {
		t.Fatalf("MaybeSpawn: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		live, _ := d.Get(c.ID)
		if live != nil && len(live.ValidatedLines) >= 3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout: ValidatedLines never reached 3 (got %d)",
				lineCount(d, c.ID))
		}
		time.Sleep(5 * time.Millisecond)
	}

	live, _ := d.Get(c.ID)
	if len(live.ValidatedLines) != 3 {
		t.Errorf("ValidatedLines len = %d, want 3", len(live.ValidatedLines))
	}
	if len(live.LineErrors) != 0 {
		t.Errorf("LineErrors = %v; canned fixture should validate cleanly", live.LineErrors)
	}

	// Apply the recommendation at idx 1 — proves the dispatcher's
	// Apply path against a real skill-output line.
	res, err := d.Apply(ctx, c.ID, 1)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(res.AppliedIdx) != 1 || res.AppliedIdx[0] != 1 {
		t.Errorf("AppliedIdx = %v, want [1]", res.AppliedIdx)
	}

	// Duplicate apply MUST 409 (dispatcher's own gate, not the
	// HTTP nonce middleware which we're not exercising here).
	if _, err := d.Apply(ctx, c.ID, 1); err == nil {
		t.Error("duplicate Apply did not error; gate is not effective")
	}

	// Finish the consult — proves the audit-log write and the
	// AppliedIdx persistence end-to-end.
	rec, err := d.Finish(c.ID, persona.FinishInfo{
		Tokens: corepersona.TokenUsage{In: 100, Out: 250},
		Model:  "fixture",
	})
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if rec == nil || rec.ID != c.ID {
		t.Fatalf("audit record missing or id mismatch: %+v", rec)
	}
	if len(rec.AppliedIdx) != 1 || rec.AppliedIdx[0] != 1 {
		t.Errorf("audit AppliedIdx = %v, want [1]", rec.AppliedIdx)
	}
	if rec.Model != "fixture" {
		t.Errorf("audit Model = %q, want fixture", rec.Model)
	}

	// Audit log on disk: GET via the AuditLog reader (the same
	// path the HTTP fall-through uses).
	got, err := auditLog.Get(c.ID)
	if err != nil {
		t.Fatalf("audit Get: %v", err)
	}
	if got.ID != c.ID || len(got.AppliedIdx) != 1 {
		t.Errorf("audit record mismatch: %+v", got)
	}

	// Live dispatcher should have removed the consult on Finish.
	if _, ok := d.Get(c.ID); ok {
		t.Error("Finish did not remove consult from live registry")
	}

	cancel()
	select {
	case <-bridgeDone:
	case <-time.After(time.Second):
		t.Error("FanFromHub did not exit on context cancel")
	}
}

// registerSkillForTest mirrors what cmd/gemba serve does at boot —
// the dispatcher itself doesn't own the registry; tests bind one and
// hand the skill in via Begin.
func registerSkillForTest(t *testing.T, _ *persona.Dispatcher, _ corepersona.Skill) error {
	t.Helper()
	// No-op for now: the dispatcher uses the skill passed via
	// BeginRequest directly; the registry lives at the HTTP layer.
	// Kept as a function so the signature documents the production
	// wiring shape and a future change (e.g. dispatcher-owned
	// registry) lands in one spot.
	return nil
}

// lineCount peeks at the live consult's line count without holding
// the dispatcher's lock — best-effort for the timeout's error
// message; the real assertion is the loop's Get + len check.
func lineCount(d *persona.Dispatcher, id string) int {
	c, ok := d.Get(id)
	if !ok {
		return -1
	}
	return len(c.ValidatedLines)
}

// TestIntegration_ConsultLoop_FailsCleanlyOnInvalidInput is the
// negative case — the same loop with malformed input must reject at
// Begin, not propagate into Receive. Catches a regression where
// Skill.ValidateInput's contract slips.
func TestIntegration_ConsultLoop_FailsCleanlyOnInvalidInput(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test; skipped under -short")
	}
	root := findRepoRoot(t)
	pm, err := corepersona.LoadFile(filepath.Join(root, ".gemba", "personas", "project-manager.toml"))
	if err != nil {
		t.Fatal(err)
	}
	d := persona.NewDispatcher(persona.NewAuditLog(t.TempDir()),
		persona.WithWorkspaceDir(t.TempDir()),
		persona.WithClock(func() time.Time { return time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC) }),
	)
	skill := epic_order.New()

	// Missing required fields (workspace + candidate_epics).
	_, err = d.Begin(persona.BeginRequest{
		Persona:   pm,
		Skill:     skill,
		Workspace: "gemba",
		RawInput:  json.RawMessage(`{"workspace_name":"Gemba","as_of":"2026-04-26T12:00:00Z","constraints":{}}`),
	})
	if err == nil {
		t.Fatal("Begin accepted malformed input")
	}
	if !errors.Is(err, err) { // tautology: just smoke-check err type
		t.Logf("Begin error: %v", err)
	}
}
