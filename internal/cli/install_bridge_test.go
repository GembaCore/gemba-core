package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallBridgeClaudeProfile(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	if err := installBridge(&out, dir, "claude_code", false); err != nil {
		t.Fatalf("install: %v", err)
	}
	path := filepath.Join(dir, ".claude", "settings.local.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var settings map[string]interface{}
	if err := json.Unmarshal(b, &settings); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := settings["hooks"]; !ok {
		t.Fatal("hooks stanza missing")
	}
	if _, ok := settings[sentinelKey]; !ok {
		t.Fatal("sentinel key missing")
	}
}

func TestInstallBridgeSecondRunIsNoOp(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	if err := installBridge(&out, dir, "claude_code", false); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".claude", "settings.local.json")
	before, _ := os.ReadFile(path)
	beforeMT, _ := os.Stat(path)

	out.Reset()
	if err := installBridge(&out, dir, "claude_code", false); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Error("second run must leave file unchanged")
	}
	if !strings.Contains(out.String(), "no-op") {
		t.Errorf("expected no-op message, got %q", out.String())
	}
	afterMT, _ := os.Stat(path)
	if afterMT.ModTime() != beforeMT.ModTime() {
		t.Error("second run must not rewrite file")
	}
}

func TestInstallBridgePreservesOperatorKeys(t *testing.T) {
	dir := t.TempDir()
	claude := filepath.Join(dir, ".claude")
	if err := os.MkdirAll(claude, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(claude, "settings.local.json")
	// Operator-authored content.
	initial := `{"env":{"FOO":"bar"}}`
	if err := os.WriteFile(path, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := installBridge(&out, dir, "claude_code", false); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	var settings map[string]interface{}
	if err := json.Unmarshal(b, &settings); err != nil {
		t.Fatal(err)
	}
	env, ok := settings["env"].(map[string]interface{})
	if !ok || env["FOO"] != "bar" {
		t.Errorf("operator env wiped: %+v", settings["env"])
	}
	if _, ok := settings["hooks"]; !ok {
		t.Error("hooks stanza missing after install")
	}
}

func TestInstallBridgePromptCommandProfile(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	if err := installBridge(&out, dir, "prompt_command", false); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".gemba", "shellrc")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !strings.Contains(string(b), "GEMBA_SESSION_ID") {
		t.Error("shellrc must guard on GEMBA_SESSION_ID")
	}
	if !strings.Contains(string(b), "gemba-bridge") {
		t.Error("shellrc must invoke gemba-bridge")
	}
}

func TestInstallBridgeDryRun(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	if err := installBridge(&out, dir, "claude_code", true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "settings.local.json")); !os.IsNotExist(err) {
		t.Error("dry-run must not write file")
	}
	if !strings.Contains(out.String(), "DRY RUN") {
		t.Errorf("dry-run output missing marker: %q", out.String())
	}
}

func TestInstallBridgeUnknownProfile(t *testing.T) {
	var out bytes.Buffer
	err := installBridge(&out, t.TempDir(), "space_opera", false)
	if err == nil || !strings.Contains(err.Error(), "unknown profile") {
		t.Errorf("want unknown-profile error, got %v", err)
	}
}
