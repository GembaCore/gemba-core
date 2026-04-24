package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// gm-97w7.1: happy path — gemba-ask with valid args writes a
// GembaAsk frame to the session log.
func TestRunHappyPath(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("GEMBA_SESSION_ID", "tmux:%42")
	t.Setenv("GEMBA_AGENT_TYPE", "claude")
	t.Setenv("GEMBA_INTERACTION_MODE", "balanced")

	var stderr bytes.Buffer
	err := run([]string{
		"--kind", "blocker",
		"--role", "manager",
		"--text", "Need STRIPE_SECRET_KEY in env.",
		"--bead", "gm-42",
	}, &stderr)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	path := filepath.Join(tmp, ".gemba", "sessions", "tmux__42.jsonl")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read session log: %v", err)
	}
	line := strings.TrimSpace(string(body))
	if line == "" {
		t.Fatal("session log empty")
	}

	var f Frame
	if err := json.Unmarshal([]byte(line), &f); err != nil {
		t.Fatalf("unmarshal frame: %v; body=%q", err, line)
	}
	if f.Hook != "GembaAsk" {
		t.Errorf("Hook: got %q, want GembaAsk", f.Hook)
	}
	if f.SessionID != "tmux:%42" {
		t.Errorf("SessionID: got %q", f.SessionID)
	}
	if f.AgentType != "claude" {
		t.Errorf("AgentType: got %q", f.AgentType)
	}

	var p GembaAskPayload
	if err := json.Unmarshal(f.Payload, &p); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if p.Kind != "blocker" || p.Role != "manager" || p.Mode != "balanced" ||
		p.BeadID != "gm-42" {
		t.Errorf("payload fields: %+v", p)
	}
	if p.Text != "Need STRIPE_SECRET_KEY in env." {
		t.Errorf("payload text: %q", p.Text)
	}
}

// gm-97w7.1: Coach cannot raise a blocker. CLI rejects fast so
// the skill author sees the error.
func TestRunCoachBlockerRejected(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("GEMBA_SESSION_ID", "s1")
	t.Setenv("GEMBA_INTERACTION_MODE", "balanced")

	err := run([]string{
		"--kind", "blocker",
		"--role", "coach",
		"--text", "this is illegal",
	}, io_Discard())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "cannot raise") {
		t.Errorf("want 'cannot raise' in error; got %q", err.Error())
	}
}

// gm-97w7.1: dangerous mode forbids any ask. Fail fast.
func TestRunDangerousModeRejects(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("GEMBA_SESSION_ID", "s1")
	t.Setenv("GEMBA_INTERACTION_MODE", "dangerous")

	err := run([]string{
		"--kind", "question",
		"--role", "coach",
		"--text", "hi",
	}, io_Discard())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "dangerous") {
		t.Errorf("want 'dangerous' in error; got %q", err.Error())
	}
}

// Missing GEMBA_SESSION_ID → hard error.
func TestRunMissingSessionIDFails(t *testing.T) {
	t.Setenv("GEMBA_SESSION_ID", "")
	t.Setenv("GEMBA_INTERACTION_MODE", "balanced")
	err := run([]string{"--kind", "question", "--role", "coach", "--text", "x"},
		io_Discard())
	if err == nil || !strings.Contains(err.Error(), "GEMBA_SESSION_ID") {
		t.Fatalf("want GEMBA_SESSION_ID error; got %v", err)
	}
}

// Missing GEMBA_INTERACTION_MODE is tolerated with a stderr warning
// (falls back to balanced) for sessions spawned before the contract
// expanded.
func TestRunMissingModeFallsBack(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("GEMBA_SESSION_ID", "s1")
	t.Setenv("GEMBA_INTERACTION_MODE", "")

	var stderr bytes.Buffer
	if err := run([]string{
		"--kind", "question",
		"--role", "coach",
		"--text", "x",
	}, &stderr); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(stderr.String(), "balanced") {
		t.Errorf("want warning about fallback to balanced; got %q", stderr.String())
	}

	path := filepath.Join(tmp, ".gemba", "sessions", "s1.jsonl")
	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), `"mode":"balanced"`) {
		t.Errorf("frame should carry mode:balanced; body=%q", string(body))
	}
}

func TestRunInvalidKind(t *testing.T) {
	t.Setenv("GEMBA_SESSION_ID", "s")
	t.Setenv("GEMBA_INTERACTION_MODE", "balanced")
	err := run([]string{"--kind", "nonsense", "--role", "coach", "--text", "x"},
		io_Discard())
	if err == nil || !strings.Contains(err.Error(), "--kind") {
		t.Fatalf("want --kind error; got %v", err)
	}
}

// io_Discard returns a writer that throws away output; nicer than
// io.Discard which is a *io.discard, not a bytes.Buffer replacement.
func io_Discard() *bytes.Buffer { return &bytes.Buffer{} }
