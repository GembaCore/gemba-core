package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GembaCore/gemba-core/internal/adapter/native/install"
)

// runInstall is the user-facing entry that the cobra command wires
// into; tests exercise it directly so they don't have to spin up a
// command tree.

func TestInstallBridgeClaudeAgent(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	if err := runInstall(context.Background(), &out, dir, "claude", false); err != nil {
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
	if _, ok := settings[install.SentinelKey]; !ok {
		t.Fatalf("sentinel key %q missing", install.SentinelKey)
	}
}

func TestInstallBridgeSecondRunIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	if err := runInstall(context.Background(), &out, dir, "claude", false); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".claude", "settings.local.json")
	before, _ := os.ReadFile(path)

	out.Reset()
	if err := runInstall(context.Background(), &out, dir, "claude", false); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Errorf("second run must leave file unchanged\nbefore=%s\nafter=%s", before, after)
	}
	if !strings.Contains(out.String(), "skipped") {
		t.Errorf("expected skipped action in second-run output, got %q", out.String())
	}
}

func TestInstallBridgePreservesOperatorJSONKeys(t *testing.T) {
	dir := t.TempDir()
	claude := filepath.Join(dir, ".claude")
	if err := os.MkdirAll(claude, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(claude, "settings.local.json")
	if err := os.WriteFile(path, []byte(`{"env":{"FOO":"bar"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runInstall(context.Background(), &out, dir, "claude", false); err != nil {
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

func TestInstallBridgeShellOnlyAgent(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	if err := runInstall(context.Background(), &out, dir, "shell_only", false); err != nil {
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
	if !strings.Contains(string(b), install.ShellSentinel) {
		t.Errorf("shellrc must contain sentinel %q", install.ShellSentinel)
	}
}

func TestInstallBridgeDryRun(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	if err := runInstall(context.Background(), &out, dir, "claude", true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "settings.local.json")); !os.IsNotExist(err) {
		t.Error("dry-run must not write settings file")
	}
	if !strings.Contains(out.String(), "dry-run") {
		t.Errorf("dry-run summary missing: %q", out.String())
	}
}

func TestInstallBridgeUnknownAgent(t *testing.T) {
	var out bytes.Buffer
	err := runInstall(context.Background(), &out, t.TempDir(), "space_opera", false)
	if err == nil || !strings.Contains(err.Error(), "unknown installer") {
		t.Errorf("want unknown-installer error, got %v", err)
	}
}

func TestResolveAgentLegacyProfile(t *testing.T) {
	cases := []struct {
		profile string
		want    string
	}{
		{"claude_code", "claude"},
		{"prompt_command", "shell_only"},
		{"shell_only", "shell_only"},
	}
	for _, c := range cases {
		got, err := resolveAgent("", c.profile)
		if err != nil {
			t.Errorf("profile=%s: %v", c.profile, err)
			continue
		}
		if got != c.want {
			t.Errorf("profile=%s: got %q, want %q", c.profile, got, c.want)
		}
	}
}

func TestResolveAgentDisagreement(t *testing.T) {
	_, err := resolveAgent("claude", "prompt_command")
	if err == nil {
		t.Fatal("want error when --agent and --profile disagree")
	}
	if !strings.Contains(err.Error(), "disagree") {
		t.Errorf("error should mention disagreement: %v", err)
	}
}
