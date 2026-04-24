package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun_missingSessionID(t *testing.T) {
	t.Setenv("GEMBA_SESSION_ID", "")
	err := run([]string{"ready"}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "GEMBA_SESSION_ID") {
		t.Errorf("expected GEMBA_SESSION_ID error, got %v", err)
	}
}

func TestRun_unknownState(t *testing.T) {
	t.Setenv("GEMBA_SESSION_ID", "sess-abc")
	t.Setenv("HOME", t.TempDir())
	err := run([]string{"bogus"}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "unknown state") {
		t.Errorf("expected unknown-state error, got %v", err)
	}
}

func TestRun_writesFrame(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GEMBA_SESSION_ID", "sess-xyz")
	t.Setenv("GEMBA_AGENT_TYPE", "claude")
	if err := run([]string{"--bead", "gm-42", "working"}, &bytes.Buffer{}); err != nil {
		t.Fatalf("run: %v", err)
	}
	path := filepath.Join(home, ".gemba", "sessions", "sess-xyz.jsonl")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	lines := bytes.Split(bytes.TrimRight(b, "\n"), []byte("\n"))
	if len(lines) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(lines))
	}
	var f Frame
	if err := json.Unmarshal(lines[0], &f); err != nil {
		t.Fatalf("unmarshal frame: %v", err)
	}
	if f.SessionID != "sess-xyz" || f.AgentType != "claude" || f.Hook != "GembaState" {
		t.Errorf("frame envelope wrong: %+v", f)
	}
	if f.EventID == "" || f.TS == "" {
		t.Errorf("frame missing event id or ts: %+v", f)
	}
	var p GembaStatePayload
	if err := json.Unmarshal(f.Payload, &p); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if p.State != "working" || p.BeadID != "gm-42" {
		t.Errorf("payload wrong: %+v", p)
	}
}

func TestRun_appendsFrames(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GEMBA_SESSION_ID", "sess-append")
	if err := run([]string{"ready"}, &bytes.Buffer{}); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if err := run([]string{"working"}, &bytes.Buffer{}); err != nil {
		t.Fatalf("second run: %v", err)
	}
	path := filepath.Join(home, ".gemba", "sessions", "sess-append.jsonl")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	lines := bytes.Split(bytes.TrimRight(b, "\n"), []byte("\n"))
	if len(lines) != 2 {
		t.Fatalf("expected 2 frames, got %d", len(lines))
	}
}

func TestSafeSessionID(t *testing.T) {
	cases := map[string]string{
		"sess-1":                   "sess-1",
		"tmux:%0":                  "tmux__0",
		"fake:gm/foo:1234567890":   "fake_gm_foo_1234567890",
		"colons:slashes/and.stops": "colons_slashes_and.stops",
	}
	for in, want := range cases {
		if got := safeSessionID(in); got != want {
			t.Errorf("safeSessionID(%q) = %q; want %q", in, got, want)
		}
	}
}
