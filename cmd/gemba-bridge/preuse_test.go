package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GembaCore/gemba-core/internal/persona"
)

// gm-eazw — Layer 2 enforcement tests.

func writeSurface(t *testing.T, home, sessionID string, s persona.Surface) {
	t.Helper()
	dir := filepath.Join(home, ".gemba", "surfaces")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal surface: %v", err)
	}
	path := filepath.Join(dir, safeSessionID(sessionID)+".json")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write surface: %v", err)
	}
}

func TestEnforcePreToolUse_ReadInsideCwd_Allow(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeSurface(t, home, "s1", persona.Surface{Cwd: "/work/repo"})

	payload, _ := json.Marshal(map[string]any{
		"hook_event_name": "PreToolUse",
		"tool_name":       "Read",
		"tool_input":      map[string]any{"file_path": "/work/repo/main.go"},
	})
	allow, reason := enforcePreToolUse(payload, "s1")
	if !allow {
		t.Errorf("expected allow, got deny: %s", reason)
	}
}

func TestEnforcePreToolUse_WriteOutsideCwd_Deny(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeSurface(t, home, "s1", persona.Surface{Cwd: "/work/repo"})

	payload, _ := json.Marshal(map[string]any{
		"hook_event_name": "PreToolUse",
		"tool_name":       "Write",
		"tool_input":      map[string]any{"file_path": "/etc/passwd"},
	})
	allow, reason := enforcePreToolUse(payload, "s1")
	if allow {
		t.Error("expected deny, got allow")
	}
	for _, want := range []string{"/etc/passwd", "write", "/work/repo"} {
		if !strings.Contains(reason, want) {
			t.Errorf("reason missing %q: got %q", want, reason)
		}
	}
}

func TestEnforcePreToolUse_ReadSiblingRepo_Allow(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeSurface(t, home, "s1", persona.Surface{
		Cwd:          "/work/repo",
		SiblingReads: []string{"/repos/sib"},
	})

	payload, _ := json.Marshal(map[string]any{
		"hook_event_name": "PreToolUse",
		"tool_name":       "Read",
		"tool_input":      map[string]any{"file_path": "/repos/sib/contracts/api.proto"},
	})
	allow, _ := enforcePreToolUse(payload, "s1")
	if !allow {
		t.Error("expected allow for sibling-repo read")
	}
}

func TestEnforcePreToolUse_WriteSiblingRepo_Deny(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeSurface(t, home, "s1", persona.Surface{
		Cwd:          "/work/repo",
		SiblingReads: []string{"/repos/sib"},
	})

	payload, _ := json.Marshal(map[string]any{
		"hook_event_name": "PreToolUse",
		"tool_name":       "Write",
		"tool_input":      map[string]any{"file_path": "/repos/sib/contracts/api.proto"},
	})
	allow, _ := enforcePreToolUse(payload, "s1")
	if allow {
		t.Error("sibling-repo write must be denied")
	}
}

func TestEnforcePreToolUse_BashCatSystemPath_Deny(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeSurface(t, home, "s1", persona.Surface{Cwd: "/work/repo"})

	payload, _ := json.Marshal(map[string]any{
		"hook_event_name": "PreToolUse",
		"tool_name":       "Bash",
		"tool_input":      map[string]any{"command": "cat /etc/shadow"},
	})
	allow, reason := enforcePreToolUse(payload, "s1")
	if allow {
		t.Errorf("expected deny for cat /etc/shadow, got allow")
	}
	if !strings.Contains(reason, "Bash:") {
		t.Errorf("expected Bash: prefix in reason, got %q", reason)
	}
}

func TestEnforcePreToolUse_BashCatLocalFile_Allow(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeSurface(t, home, "s1", persona.Surface{Cwd: "/work/repo"})

	payload, _ := json.Marshal(map[string]any{
		"hook_event_name": "PreToolUse",
		"tool_name":       "Bash",
		"tool_input":      map[string]any{"command": "cat /work/repo/README.md"},
	})
	allow, reason := enforcePreToolUse(payload, "s1")
	if !allow {
		t.Errorf("expected allow for in-cwd cat, got %q", reason)
	}
}

func TestEnforcePreToolUse_BashRmInsideCwd_Allow(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeSurface(t, home, "s1", persona.Surface{Cwd: "/work/repo"})

	payload, _ := json.Marshal(map[string]any{
		"hook_event_name": "PreToolUse",
		"tool_name":       "Bash",
		"tool_input":      map[string]any{"command": "rm /work/repo/scratch.txt"},
	})
	allow, _ := enforcePreToolUse(payload, "s1")
	if !allow {
		t.Error("rm inside cwd should be allowed")
	}
}

func TestEnforcePreToolUse_BashRmOutsideCwd_Deny(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeSurface(t, home, "s1", persona.Surface{Cwd: "/work/repo"})

	payload, _ := json.Marshal(map[string]any{
		"hook_event_name": "PreToolUse",
		"tool_name":       "Bash",
		"tool_input":      map[string]any{"command": "rm /repos/sib/secret.txt"},
	})
	allow, _ := enforcePreToolUse(payload, "s1")
	if allow {
		t.Error("rm outside cwd must be denied")
	}
}

func TestEnforcePreToolUse_UnknownTool_Allow(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeSurface(t, home, "s1", persona.Surface{Cwd: "/work/repo"})

	payload, _ := json.Marshal(map[string]any{
		"hook_event_name": "PreToolUse",
		"tool_name":       "mcp__custom__weird",
		"tool_input":      map[string]any{"file_path": "/etc/passwd"},
	})
	allow, _ := enforcePreToolUse(payload, "s1")
	if !allow {
		t.Error("unknown tools must fail-open (the bridge can't reason about every future tool)")
	}
}

func TestEnforcePreToolUse_SurfaceMissing_FallsBackToCwd(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// no surface file — bridge should fall back to deny-outside-cwd.
	// The fallback uses payload.cwd; we don't touch the working dir
	// of the test process.
	payload, _ := json.Marshal(map[string]any{
		"hook_event_name": "PreToolUse",
		"tool_name":       "Write",
		"tool_input":      map[string]any{"file_path": "/etc/passwd"},
		"cwd":             "/work/repo",
	})
	allow, _ := enforcePreToolUse(payload, "no-such-session")
	if allow {
		t.Error("missing surface must default to deny outside cwd")
	}
}

func TestEnforcePreToolUse_MalformedPayload_Allow(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeSurface(t, home, "s1", persona.Surface{Cwd: "/work/repo"})

	allow, _ := enforcePreToolUse([]byte("not-json"), "s1")
	if !allow {
		t.Error("malformed payload must fail-open (downstream agent has its own validator)")
	}
}

func TestEnforcePreToolUse_AdditionalReads_Allow(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeSurface(t, home, "s1", persona.Surface{
		Cwd:             "/work/repo",
		AdditionalReads: []string{"/notes/spec.md"},
	})

	payload, _ := json.Marshal(map[string]any{
		"hook_event_name": "PreToolUse",
		"tool_name":       "Read",
		"tool_input":      map[string]any{"file_path": "/notes/spec.md"},
	})
	allow, _ := enforcePreToolUse(payload, "s1")
	if !allow {
		t.Error("AdditionalReads override must be honored")
	}
}

func TestEmitDeny_Shape(t *testing.T) {
	var buf bytes.Buffer
	if err := emitDeny(&buf, "test reason"); err != nil {
		t.Fatalf("emitDeny: %v", err)
	}
	var resp claudeDenyResponse
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, buf.String())
	}
	if resp.HookSpecificOutput.HookEventName != "PreToolUse" {
		t.Errorf("hookEventName: %q", resp.HookSpecificOutput.HookEventName)
	}
	if resp.HookSpecificOutput.PermissionDecision != "deny" {
		t.Errorf("permissionDecision: %q", resp.HookSpecificOutput.PermissionDecision)
	}
	if resp.HookSpecificOutput.PermissionDecisionReason != "test reason" {
		t.Errorf("reason: %q", resp.HookSpecificOutput.PermissionDecisionReason)
	}
}

func TestRunPreToolUse_DenyEmitsResponse(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GEMBA_SESSION_ID", "s1")
	t.Setenv("GEMBA_AGENT_TYPE", "claude")
	t.Setenv("GEMBA_HOOK_EVENT", "PreToolUse")
	writeSurface(t, home, "s1", persona.Surface{Cwd: "/work/repo"})

	payload := `{"hook_event_name":"PreToolUse","tool_name":"Write","tool_input":{"file_path":"/etc/passwd"}}`
	var stdout bytes.Buffer
	if err := run(bytes.NewBufferString(payload), &stdout, []string{"gemba-bridge"}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if stdout.Len() == 0 {
		t.Fatal("expected deny response on stdout")
	}
	var resp claudeDenyResponse
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, stdout.String())
	}
	if resp.HookSpecificOutput.PermissionDecision != "deny" {
		t.Errorf("expected deny, got %q", resp.HookSpecificOutput.PermissionDecision)
	}
	// Frame should still be appended to the audit log so the rejection
	// is visible in evidence.
	frame, err := os.ReadFile(filepath.Join(home, ".gemba", "sessions", "s1.jsonl"))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if len(frame) == 0 {
		t.Error("audit frame must still be written on deny")
	}
}

func TestRunPreToolUse_AllowEmitsNoResponse(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GEMBA_SESSION_ID", "s1")
	t.Setenv("GEMBA_AGENT_TYPE", "claude")
	t.Setenv("GEMBA_HOOK_EVENT", "PreToolUse")
	writeSurface(t, home, "s1", persona.Surface{Cwd: "/work/repo"})

	payload := `{"hook_event_name":"PreToolUse","tool_name":"Read","tool_input":{"file_path":"/work/repo/README.md"}}`
	var stdout bytes.Buffer
	if err := run(bytes.NewBufferString(payload), &stdout, []string{"gemba-bridge"}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if stdout.Len() != 0 {
		t.Errorf("allow path must not emit on stdout, got %q", stdout.String())
	}
}
