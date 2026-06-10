package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildCodexExecArgsAddsSessionScopedMCPOverrides(t *testing.T) {
	t.Setenv("GEMBA_SESSION_ID", "sess-42")
	t.Setenv("GEMBA_AGENT_TYPE", "codex")
	t.Setenv("GEMBA_BEAD_ID", "gm-42")
	t.Setenv("GEMBA_INTERACTION_MODE", "balanced")
	t.Setenv("GEMBA_STATE_COMMAND", "/opt/gemba-state")

	args := buildCodexExecArgs(codexExecOptions{
		Model:       "gpt-5.4-mini",
		Sandbox:     "workspace-write",
		Approval:    "never",
		LastMessage: "/tmp/last.txt",
		MCPName:     "gemba",
		MCPCommand:  "/opt/gemba-mcp",
	})

	if len(args) == 0 || args[0] != "exec" {
		t.Fatalf("args should start with exec: %v", args)
	}
	for _, want := range []string{
		`mcp_servers.gemba.command="/opt/gemba-mcp"`,
		`mcp_servers.gemba.env.GEMBA_SESSION_ID="sess-42"`,
		`mcp_servers.gemba.env.GEMBA_AGENT_TYPE="codex"`,
		`mcp_servers.gemba.env.GEMBA_BEAD_ID="gm-42"`,
		`mcp_servers.gemba.env.GEMBA_INTERACTION_MODE="balanced"`,
		`mcp_servers.gemba.env.GEMBA_STATE_COMMAND="/opt/gemba-state"`,
	} {
		if !containsArg(args, want) {
			t.Fatalf("missing override %q in %s", want, strings.Join(args, " "))
		}
	}
	if got := args[len(args)-1]; got != "-" {
		t.Fatalf("last arg = %q, want stdin marker", got)
	}
}

func TestBuildCodexExecArgsOmitsMCPWhenCommandMissing(t *testing.T) {
	args := buildCodexExecArgs(codexExecOptions{
		Model:    "gpt-5.4-mini",
		Sandbox:  "workspace-write",
		Approval: "never",
	})
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "mcp_servers.") {
		t.Fatalf("unexpected MCP override in %s", joined)
	}
}

func TestResolveMCPCommandRequiresExistingExecutablePath(t *testing.T) {
	dir := t.TempDir()
	cmd := filepath.Join(dir, "gemba-mcp")
	if err := os.WriteFile(cmd, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := resolveMCPCommand(cmd); got != cmd {
		t.Fatalf("resolveMCPCommand existing path = %q, want %q", got, cmd)
	}
	if got := resolveMCPCommand(filepath.Join(dir, "missing")); got != "" {
		t.Fatalf("resolveMCPCommand missing path = %q, want empty", got)
	}
}

func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}
