package native

import (
	"os"
	"strings"
	"testing"

	"github.com/MikeBengtson/gemba/internal/adapter/native/agents"
	"github.com/MikeBengtson/gemba/internal/persona"
)

func TestBuildSpawnSpec_TmuxPathUnchanged(t *testing.T) {
	// Regression guard: the container work must not alter the
	// SpawnSpec that tmux agents have been producing all along.
	agent := agents.AgentType{
		Name:     "claude",
		Binary:   "claude",
		Preamble: agents.PreambleClaudeMD,
		Hooks:    agents.HookClaudeCode,
	}
	spec, err := buildSpawnSpec(agent, "/home/op/work", "sess-1", "claude", "title", nil)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Image != "" {
		t.Errorf("tmux path must not set Image, got %q", spec.Image)
	}
	if len(spec.Mounts) != 0 {
		t.Errorf("tmux path must not set Mounts, got %v", spec.Mounts)
	}
	if spec.Cwd != "/home/op/work" {
		t.Errorf("Cwd: %q", spec.Cwd)
	}
}

func TestBuildSpawnSpec_ContainerAutoWorkspaceMount(t *testing.T) {
	ro := true
	agent := agents.AgentType{
		Name:     "claude-sandbox",
		Binary:   "claude",
		Preamble: agents.PreambleFirstMessage,
		Hooks:    agents.HookClaudeCode,
		Container: agents.ContainerSpec{
			Image:          "img:1",
			Cwd:            "/work",
			CPUs:           2.0,
			Memory:         "4g",
			PidsLimit:      256,
			Network:        "none",
			Secrets:        []string{"anthropic_api_key"},
			ReadOnlyRootfs: &ro,
		},
	}
	spec, err := buildSpawnSpec(agent, "/home/op/worktrees/sess-1", "sess-1", "claude-sandbox", "title", nil)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Image != "img:1" {
		t.Errorf("Image: %q", spec.Image)
	}
	if spec.Cwd != "/work" {
		t.Errorf("Cwd: %q", spec.Cwd)
	}
	if spec.Limits.CPUs != 2.0 || spec.Limits.Memory != 4<<30 || spec.Limits.PidsLimit != 256 {
		t.Errorf("Limits: %+v", spec.Limits)
	}
	if spec.Network.Mode != "none" {
		t.Errorf("Network: %q", spec.Network.Mode)
	}
	if !spec.ReadOnlyRootfs {
		t.Error("ReadOnlyRootfs expected true")
	}
	if len(spec.Secrets) != 1 || spec.Secrets[0] != "anthropic_api_key" {
		t.Errorf("Secrets: %v", spec.Secrets)
	}
	// Auto workspace mount
	if len(spec.Mounts) != 1 {
		t.Fatalf("want 1 auto mount, got %d: %+v", len(spec.Mounts), spec.Mounts)
	}
	m := spec.Mounts[0]
	if m.Src != "/home/op/worktrees/sess-1" || m.Dst != "/work" || m.Mode != "rw" {
		t.Errorf("auto mount: %+v", m)
	}
	// Labels for reaper
	if spec.Labels["gemba.session"] != "sess-1" || spec.Labels["gemba.agent"] != "claude-sandbox" {
		t.Errorf("Labels: %+v", spec.Labels)
	}
	// UserNS aligned with gemba process
	if spec.UserNS == "" || !strings.Contains(spec.UserNS, ":") {
		t.Errorf("UserNS: %q", spec.UserNS)
	}
}

func TestBuildSpawnSpec_OperatorMountCoversCwd(t *testing.T) {
	// When the operator explicitly declares a mount at the container
	// cwd, the auto workspace mount must NOT fire — operator intent
	// wins.
	agent := agents.AgentType{
		Name:     "c",
		Binary:   "claude",
		Preamble: agents.PreambleClaudeMD,
		Hooks:    agents.HookNone,
		Container: agents.ContainerSpec{
			Image: "img:1",
			Cwd:   "/code",
			Mounts: []agents.ContainerMount{
				{Src: "{{workspace}}", Dst: "/code", Mode: "ro"},
			},
		},
	}
	spec, err := buildSpawnSpec(agent, "/tmp/ws", "s", "c", "t", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Mounts) != 1 {
		t.Fatalf("operator mount must suppress auto mount, got %d", len(spec.Mounts))
	}
	if spec.Mounts[0].Mode != "ro" {
		t.Errorf("operator mount mode lost: %+v", spec.Mounts[0])
	}
	if spec.Mounts[0].Src != "/tmp/ws" {
		t.Errorf("{{workspace}} not expanded: %+v", spec.Mounts[0])
	}
}

func TestBuildSpawnSpec_TemplateExpansion(t *testing.T) {
	agent := agents.AgentType{
		Name:     "c",
		Binary:   "claude",
		Preamble: agents.PreambleClaudeMD,
		Hooks:    agents.HookNone,
		Container: agents.ContainerSpec{
			Image: "img:1",
			Cwd:   "/work",
			Mounts: []agents.ContainerMount{
				{Src: "{{workspace}}/sub", Dst: "/sub"},
				{Src: "/host/{{session_id}}", Dst: "/scratch"},
				{Src: "/host/{{agent_name}}", Dst: "/bridge"},
			},
		},
	}
	spec, err := buildSpawnSpec(agent, "/ws", "sess-42", "c", "t", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/ws/sub", "/host/sess-42", "/host/c"}
	for i, w := range want {
		if spec.Mounts[i].Src != w {
			t.Errorf("mount[%d].Src = %q, want %q", i, spec.Mounts[i].Src, w)
		}
	}
}

func TestBuildSpawnSpec_ReadOnlyRootfsExplicitFalse(t *testing.T) {
	ro := false
	agent := agents.AgentType{
		Name:     "c",
		Binary:   "claude",
		Preamble: agents.PreambleClaudeMD,
		Hooks:    agents.HookNone,
		Container: agents.ContainerSpec{
			Image:          "img:1",
			ReadOnlyRootfs: &ro,
		},
	}
	spec, err := buildSpawnSpec(agent, "/ws", "s", "c", "t", nil)
	if err != nil {
		t.Fatal(err)
	}
	if spec.ReadOnlyRootfs {
		t.Error("explicit false ReadOnlyRootfs must flip the default")
	}
}

func TestParseMemory(t *testing.T) {
	cases := map[string]int64{
		"":       0,
		"1024":   1024,
		"512k":   512 << 10,
		"256m":   256 << 20,
		"4g":     4 << 30,
		"  4g  ": 4 << 30,
	}
	for in, want := range cases {
		got, err := parseMemory(in)
		if err != nil {
			t.Errorf("parseMemory(%q) err = %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("parseMemory(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestParseMemory_Rejects(t *testing.T) {
	bad := []string{"4gb", "4gigs", "-1", "abc"}
	for _, s := range bad {
		if _, err := parseMemory(s); err == nil {
			t.Errorf("parseMemory(%q) should have failed", s)
		}
	}
}

// UserNS assertion helper to avoid brittle literal comparison.
func TestBuildSpawnSpec_UserNSMatchesProcess(t *testing.T) {
	agent := agents.AgentType{
		Name:      "c",
		Binary:    "claude",
		Preamble:  agents.PreambleClaudeMD,
		Hooks:     agents.HookNone,
		Container: agents.ContainerSpec{Image: "img"},
	}
	spec, err := buildSpawnSpec(agent, "/ws", "s", "c", "t", nil)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(spec.UserNS, ":")
	if len(parts) != 2 {
		t.Fatalf("UserNS malformed: %q", spec.UserNS)
	}
	if parts[0] != itoa(os.Getuid()) {
		t.Errorf("uid mismatch: %s vs %d", parts[0], os.Getuid())
	}
}

func itoa(i int) string {
	// Stub to avoid pulling strconv into the test file for a single
	// conversion.
	if i < 0 {
		return "-" + itoa(-i)
	}
	if i < 10 {
		return string(rune('0' + i))
	}
	return itoa(i/10) + itoa(i%10)
}

// gm-utik: when a Surface is attached to the prompt, buildSpawnSpec
// must compose its mounts on top of the operator's set per the
// agent's surface_mode.
func TestBuildSpawnSpec_SurfaceAdditive(t *testing.T) {
	t.Setenv("HOME", "/Users/op") // exercise tooling-path expansion

	agent := agents.AgentType{
		Name:     "c",
		Binary:   "claude",
		Preamble: agents.PreambleClaudeMD,
		Hooks:    agents.HookNone,
		Container: agents.ContainerSpec{
			Image: "img:1",
			Cwd:   "/work",
			Mounts: []agents.ContainerMount{
				{Src: "{{workspace}}/scratch", Dst: "/scratch", Mode: "rw"},
			},
			// SurfaceMode left empty == additive (default).
		},
	}
	// Use a Surface whose paths actually exist on the test host so
	// the path-existence filter doesn't drop them. /tmp is the safest
	// universally-present anchor; we point Cwd + ToolingPaths there.
	s := &persona.Surface{
		Cwd:          "/tmp",
		ToolingPaths: []string{"/tmp"},
	}
	spec, err := buildSpawnSpec(agent, "/tmp", "sess-1", "c", "t", s)
	if err != nil {
		t.Fatal(err)
	}
	// Operator scratch mount survives, surface /tmp lands as :rw.
	var sawScratch, sawSurfaceCwd bool
	for _, m := range spec.Mounts {
		if m.Dst == "/scratch" && m.Mode == "rw" {
			sawScratch = true
		}
		if m.Dst == "/tmp" && m.Mode == "rw" {
			sawSurfaceCwd = true
		}
	}
	if !sawScratch {
		t.Errorf("operator scratch mount lost in additive merge: %+v", spec.Mounts)
	}
	if !sawSurfaceCwd {
		t.Errorf("surface cwd mount missing: %+v", spec.Mounts)
	}
}

func TestBuildSpawnSpec_SurfaceExclusiveDropsOperatorMounts(t *testing.T) {
	agent := agents.AgentType{
		Name:     "c",
		Binary:   "claude",
		Preamble: agents.PreambleClaudeMD,
		Hooks:    agents.HookNone,
		Container: agents.ContainerSpec{
			Image: "img:1",
			Cwd:   "/work",
			Mounts: []agents.ContainerMount{
				{Src: "/host/scratch", Dst: "/scratch", Mode: "rw"},
			},
			SurfaceMode: "exclusive",
		},
	}
	s := &persona.Surface{Cwd: "/tmp"}
	spec, err := buildSpawnSpec(agent, "/tmp", "s", "c", "t", s)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range spec.Mounts {
		if m.Dst == "/scratch" {
			t.Errorf("exclusive mode must drop operator mount, got %+v", m)
		}
	}
}

func TestBuildSpawnSpec_NilSurfaceKeepsLegacyBehavior(t *testing.T) {
	// Pre-surface dispatch flows pass nil; the original mount list
	// (operator + auto cwd) should be unchanged.
	agent := agents.AgentType{
		Name:     "c",
		Binary:   "claude",
		Preamble: agents.PreambleClaudeMD,
		Hooks:    agents.HookNone,
		Container: agents.ContainerSpec{
			Image: "img:1",
			Cwd:   "/work",
		},
	}
	spec, err := buildSpawnSpec(agent, "/tmp", "s", "c", "t", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Mounts) != 1 {
		t.Fatalf("nil-surface path should produce just the auto cwd mount, got %d: %+v",
			len(spec.Mounts), spec.Mounts)
	}
	if spec.Mounts[0].Mode != "rw" || spec.Mounts[0].Dst != "/work" {
		t.Errorf("auto cwd mount lost: %+v", spec.Mounts[0])
	}
}
