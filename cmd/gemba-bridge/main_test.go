package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunWritesFrame(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GEMBA_SESSION_ID", "tmux:%12")
	t.Setenv("GEMBA_AGENT_TYPE", "claude")
	t.Setenv("GEMBA_HOOK_EVENT", "SessionStart")

	err := run(bytes.NewBufferString(`{"turn_id":"t1","cwd":"/tmp/w"}`), []string{"gemba-bridge"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	// safeSessionID replaces both `:` and `%` with `_` → two underscores.
	path := filepath.Join(home, ".gemba", "sessions", "tmux__12.jsonl")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !bytes.HasSuffix(b, []byte("\n")) {
		t.Error("frame must end with newline")
	}
	var f Frame
	if err := json.Unmarshal(bytes.TrimSpace(b), &f); err != nil {
		t.Fatalf("parse frame: %v (raw=%s)", err, string(b))
	}
	if f.SessionID != "tmux:%12" {
		t.Errorf("session id: got %q", f.SessionID)
	}
	if f.AgentType != "claude" {
		t.Errorf("agent type: got %q", f.AgentType)
	}
	if f.Hook != "SessionStart" {
		t.Errorf("hook: got %q", f.Hook)
	}
	if len(f.Payload) == 0 {
		t.Error("payload must be preserved")
	}
	if f.EventID == "" {
		t.Error("event id must be set")
	}
}

func TestRunAppendsOnSecondInvocation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GEMBA_SESSION_ID", "s1")
	t.Setenv("GEMBA_AGENT_TYPE", "claude")
	t.Setenv("GEMBA_HOOK_EVENT", "SessionStart")

	if err := run(bytes.NewBufferString(`{}`), []string{"gemba-bridge"}); err != nil {
		t.Fatal(err)
	}
	if err := run(bytes.NewBufferString(`{}`), []string{"gemba-bridge"}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(home, ".gemba", "sessions", "s1.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) != 2 {
		t.Errorf("want 2 frames appended, got %d", len(lines))
	}
}

func TestRunStoresRawWhenPayloadNotJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GEMBA_SESSION_ID", "s1")
	t.Setenv("GEMBA_HOOK_EVENT", "RawEvent")
	if err := run(bytes.NewBufferString("not-json"), []string{"gemba-bridge"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(home, ".gemba", "sessions", "s1.jsonl"))
	var f Frame
	if err := json.Unmarshal(bytes.TrimSpace(b), &f); err != nil {
		t.Fatalf("parse frame: %v", err)
	}
	if f.PayloadRaw != "not-json" {
		t.Errorf("want raw fallback, got %+v", f)
	}
	if len(f.Payload) != 0 {
		t.Error("Payload must be empty when input is not JSON")
	}
}

func TestRunRequiresSessionID(t *testing.T) {
	t.Setenv("GEMBA_SESSION_ID", "")
	err := run(bytes.NewBufferString(`{}`), []string{"gemba-bridge"})
	if err == nil || !strings.Contains(err.Error(), "GEMBA_SESSION_ID") {
		t.Errorf("want session id error, got %v", err)
	}
}

func TestSafeSessionID(t *testing.T) {
	got := safeSessionID("tmux:%12/abc")
	if got != "tmux__12_abc" {
		t.Errorf("safeSessionID: got %q", got)
	}
}
