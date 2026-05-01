// Package agents loads the operator-authored .gemba/agents.toml,
// which lists the agent types the native adaptor can spawn (Claude
// Code, shell-only, future types like Codex). Adding a new agent
// type to a workspace is a config change — no Gemba recompile — as
// long as the type's hook profile is already built into gemba-bridge.
package agents

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// PreambleStrategy names how an agent type receives project / epic
// / work-item preamble context at session start.
type PreambleStrategy string

const (
	// PreambleClaudeMD appends a fenced block to CLAUDE.md bracketed
	// by sentinel comments; EndSession removes the block.
	PreambleClaudeMD PreambleStrategy = "claude_md"

	// PreambleFirstMessage writes the composed preamble as the first
	// user prompt the agent sees (for CLIs that don't have a CLAUDE.md
	// equivalent).
	PreambleFirstMessage PreambleStrategy = "first_message"

	// PreambleCodexExec writes the composed preamble to a prompt file
	// before spawn. A gemba-owned Codex driver then runs `codex exec`
	// non-interactively and reports lifecycle via gemba-state.
	PreambleCodexExec PreambleStrategy = "codex_exec"

	// PreambleStdoutBanner echoes the preamble to the terminal as a
	// markdown block; used by shell-only (no agent to read a file).
	PreambleStdoutBanner PreambleStrategy = "stdout_banner"
)

// HookProfile names which set of hooks gemba-bridge installs for an
// agent type. Each profile corresponds to a built-in template in
// cmd/gemba-bridge's install path.
type HookProfile string

const (
	// HookClaudeCode writes a .claude/settings.local.json stanza for
	// the Claude Code hook surface (SessionStart, PreToolUse, etc.).
	HookClaudeCode HookProfile = "claude_code"

	// HookPromptCommand installs a shellrc fragment that pipes the
	// user's $PROMPT_COMMAND through gemba-bridge — used by shell-only
	// so bd invocations still correlate.
	HookPromptCommand HookProfile = "prompt_command"

	// HookNone skips installation entirely.
	HookNone HookProfile = "none"
)

// InteractionMode selects which section of the interaction_profile.md
// gets injected into the session preamble (gm-bglh / gm-97w7). See
// .gemba/interaction_profile.md for the behavioural contract of each
// mode.
type InteractionMode string

const (
	// InteractionDangerous — never ask, never stop. Best for trusted
	// autonomous loops where human review happens post-hoc.
	InteractionDangerous InteractionMode = "dangerous"
	// InteractionBalanced — stop for questions AND blockers. Default.
	InteractionBalanced InteractionMode = "balanced"
	// InteractionCautious — surface questions inline; stop only for
	// blockers. Best when the operator is watching the session.
	InteractionCautious InteractionMode = "cautious"
)

// DefaultInteractionMode is the mode used when an agent's config
// doesn't declare one. Balanced is the safest default — the operator
// sees questions and blockers both, which matches unassisted
// autonomous development's bias toward "check in".
const DefaultInteractionMode = InteractionBalanced

// ContainerMount is one entry in an AgentType's container.mounts
// list. `src` is a host path (template-expanded at spawn time —
// `{{workspace}}`, `{{session_id}}`, `{{agent_name}}`); `dst` is the
// destination inside the container. `mode` is "rw" | "ro" | "tmpfs".
// See docs/design/containerized-sessions.md §7.
type ContainerMount struct {
	Src  string `toml:"src"`
	Dst  string `toml:"dst"`
	Mode string `toml:"mode"`
	// Size is a tmpfs size hint (e.g. "128m"). Ignored for bind-mounts.
	Size string `toml:"size"`
}

// ContainerSpec is the [agent.container] stanza from agents.toml —
// optional; when present an agent type spawns as a Docker container
// rather than a tmux pane. See gm-root.15.6 and
// docs/design/containerized-sessions.md §11.
type ContainerSpec struct {
	// Image is the container image reference. Required when the
	// stanza is present.
	Image string `toml:"image"`
	// Cwd is the working directory inside the container. Defaults
	// to "/work".
	Cwd string `toml:"cwd"`
	// CPUs is the fractional CPU count (e.g. 2.0 for two cores).
	// Required — zero means "no cap" which the design rejects for
	// sandboxed agents.
	CPUs float64 `toml:"cpus"`
	// Memory is parsed by the backend from a string like "4g" into
	// bytes; we keep it as a string here so validation errors are
	// legible ("invalid memory spec '4gigs'" rather than a parse
	// error from strconv).
	Memory string `toml:"memory"`
	// PidsLimit caps the number of processes. 0 → backend default
	// (512).
	PidsLimit int64 `toml:"pids_limit"`
	// Mounts declares bind-mounts and tmpfs for the container.
	Mounts []ContainerMount `toml:"mounts"`
	// Secrets are named entries the backend resolves and mounts at
	// /run/secrets/<name>. Never carry secret material as env.
	Secrets []string `toml:"secrets"`
	// Network picks an egress policy. "none" (default), "bridge:<name>",
	// or "host" (requires Unsafe=true).
	Network string `toml:"network"`
	// ReadOnlyRootfs defaults to true when the stanza is present.
	// Pointer-to-bool so absent and explicit-false differ.
	ReadOnlyRootfs *bool `toml:"read_only_rootfs"`
	// Unsafe unlocks --privileged / --network host / other envelope
	// escapes. Required to accept any of them. Loud in the banner;
	// logged on every spawn.
	Unsafe bool `toml:"unsafe"`
	// SurfaceMode controls how the surface-derived mount set
	// (gm-utik) composes with the operator's Mounts list above.
	// "additive" (default) layers operator extras on top of the
	// policy-enforced surface; "exclusive" drops them entirely.
	// Empty string = "additive". Unknown values fall back to
	// additive with a logged warning at spawn time.
	SurfaceMode string `toml:"surface_mode"`
}

// Present reports whether the operator declared a container stanza
// at all. The zero-value sentinel is "no Image" — every present
// spec requires one.
func (c ContainerSpec) Present() bool {
	return c.Image != ""
}

// AgentType is one entry in .gemba/agents.toml.
type AgentType struct {
	// Name is the operator-chosen identifier — must be unique within
	// the registry, lower-kebab-case by convention.
	Name string `toml:"name"`
	// Binary is the command the adaptor exec's when SpawnPane'ing a
	// session of this type. Looked up via exec.LookPath at boot; a
	// missing binary makes the type unavailable (logged, not fatal —
	// other types may still work).
	Binary string `toml:"binary"`
	// Args is a fixed argv prefix passed before any caller-supplied
	// arguments.
	Args []string `toml:"args"`
	// Model is the model identifier passed to the agent binary when
	// the agent supports a --model / -m flag. Optional — leaving blank
	// means the agent's own default wins.
	Model string `toml:"model"`
	// Preamble selects how the session's preamble (project + epic +
	// work-item layers) gets to the agent.
	Preamble PreambleStrategy `toml:"preamble"`
	// Hooks selects which gemba-bridge profile this type uses.
	Hooks HookProfile `toml:"hooks"`
	// InteractionMode selects which section of the
	// interaction_profile.md gets composed into the session preamble.
	// Empty string falls back to DefaultInteractionMode (balanced).
	InteractionMode InteractionMode `toml:"interaction_mode"`
	// InteractionProfile optionally overrides the default
	// .gemba/interaction_profile.md path. Relative paths resolve
	// against the workspace dir. Empty string uses the default.
	InteractionProfile string `toml:"interaction_profile"`
	// Container is the optional [agent.container] stanza. When
	// Present(), the agent spawns as a Docker container instead of
	// a tmux pane. See gm-root.15.6 /
	// docs/design/containerized-sessions.md §11.
	Container ContainerSpec `toml:"container"`
	// IntraParallel declares whether a single session of this agent
	// type can carry multiple concurrent beads (i.e. the prompt orders
	// the agent to fan work out internally). Default false — one bead
	// per session, parallelism only across sessions. See gm-root.16
	// and docs/design/parallelism-boundary.md.
	IntraParallel bool `toml:"intra_parallel"`
	// MaxParallel is the hard cap on concurrent beads per session of
	// this type. Required when IntraParallel is true. Ignored when
	// false (effective cap is always 1). The dispatcher MUST refuse a
	// new bead to a session at cap and fan to another session instead;
	// deconfliction rules apply uniformly regardless of which axis the
	// next bead lands on.
	MaxParallel int `toml:"max_parallel"`
}

// ResolvedInteractionMode returns the agent's configured mode with
// the default applied when the field is blank.
func (a AgentType) ResolvedInteractionMode() InteractionMode {
	if a.InteractionMode == "" {
		return DefaultInteractionMode
	}
	return a.InteractionMode
}

// ResolvedMaxParallel returns the effective per-session bead cap. For
// agents without IntraParallel the cap is always 1 — config drift
// (e.g. MaxParallel=3 with IntraParallel=false) is logged as a warning
// at load time but does not change the runtime cap.
func (a AgentType) ResolvedMaxParallel() int {
	if !a.IntraParallel {
		return 1
	}
	return a.MaxParallel
}

// Registry is the parsed agents.toml.
type Registry struct {
	Agents []AgentType `toml:"agent"`
}

// Get returns the agent type with the given name; second return is
// false when not found.
func (r Registry) Get(name string) (AgentType, bool) {
	for _, a := range r.Agents {
		if a.Name == name {
			return a, true
		}
	}
	return AgentType{}, false
}

// Names returns every registered agent type name, in registry order.
// Used by the SPA's agent-type picker.
func (r Registry) Names() []string {
	out := make([]string, len(r.Agents))
	for i, a := range r.Agents {
		out[i] = a.Name
	}
	return out
}

// Load parses the TOML file at path and validates the registry.
// Returns the path of the file that was actually loaded so callers
// can log which registry the operator's running against.
func Load(path string) (Registry, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Registry{}, fmt.Errorf("agents: read %s: %w", path, err)
	}
	var r Registry
	if err := toml.Unmarshal(b, &r); err != nil {
		return Registry{}, fmt.Errorf("agents: parse %s: %w", path, err)
	}
	if err := r.Validate(); err != nil {
		return Registry{}, fmt.Errorf("agents: validate %s: %w", path, err)
	}
	return r, nil
}

// Resolve returns the registry path to load, relative to workspaceDir,
// or empty string when no file is present. Callers treat empty as
// "operator hasn't opted in yet" and surface a friendly error.
func Resolve(workspaceDir string) string {
	p := filepath.Join(workspaceDir, ".gemba", "agents.toml")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return ""
}

// Validate enforces non-empty names, unique names, known preamble /
// hook profile enums, and non-empty binary. Validation errors are
// aggregated so the operator sees every problem at once.
func (r Registry) Validate() error {
	if len(r.Agents) == 0 {
		return fmt.Errorf("registry must declare at least one [[agent]]")
	}
	var problems []string
	seen := make(map[string]int)
	for i, a := range r.Agents {
		prefix := fmt.Sprintf("agent[%d] (%q)", i, a.Name)
		if strings.TrimSpace(a.Name) == "" {
			problems = append(problems, fmt.Sprintf("agent[%d]: name required", i))
		}
		if strings.TrimSpace(a.Binary) == "" {
			problems = append(problems, fmt.Sprintf("%s: binary required", prefix))
		}
		if !validPreamble(a.Preamble) {
			problems = append(problems, fmt.Sprintf("%s: unknown preamble %q", prefix, a.Preamble))
		}
		if !validHook(a.Hooks) {
			problems = append(problems, fmt.Sprintf("%s: unknown hook profile %q", prefix, a.Hooks))
		}
		// Interaction mode is optional; empty string falls back to the
		// default at dispatch time. Any non-empty value must be known.
		if a.InteractionMode != "" && !validInteractionMode(a.InteractionMode) {
			problems = append(problems, fmt.Sprintf("%s: unknown interaction_mode %q (want dangerous | balanced | cautious)", prefix, a.InteractionMode))
		}
		if first, dup := seen[a.Name]; dup {
			problems = append(problems, fmt.Sprintf("%s: duplicate name (first at agent[%d])", prefix, first))
		}
		seen[a.Name] = i
		if a.Container.Present() {
			problems = append(problems, validateContainer(prefix, a.Container)...)
		}
		if a.IntraParallel && a.MaxParallel <= 0 {
			problems = append(problems, fmt.Sprintf(
				"%s: intra_parallel=true requires max_parallel >= 1 (got %d)",
				prefix, a.MaxParallel))
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("%s", strings.Join(problems, "; "))
	}
	return nil
}

// Warnings returns soft-failure messages the operator should see but
// that don't block load. Currently flags max_parallel set without
// intra_parallel — config drift that would silently have no effect.
// Callers (Load's wrapper, future config-doctor command) decide how to
// surface them.
func (r Registry) Warnings() []string {
	var out []string
	for i, a := range r.Agents {
		if !a.IntraParallel && a.MaxParallel > 0 {
			out = append(out, fmt.Sprintf(
				"agent[%d] (%q): max_parallel=%d set but intra_parallel=false — value is ignored (effective cap is 1)",
				i, a.Name, a.MaxParallel))
		}
	}
	return out
}

// validateContainer enforces the ContainerSpec invariants the rest of
// the stack depends on. Aggregated rather than failing-fast so the
// operator sees every problem at once.
func validateContainer(prefix string, c ContainerSpec) []string {
	var problems []string
	if strings.TrimSpace(c.Image) == "" {
		problems = append(problems, fmt.Sprintf("%s.container: image required when stanza is present", prefix))
	}
	if c.CPUs < 0 {
		problems = append(problems, fmt.Sprintf("%s.container: cpus must be >= 0 (got %v)", prefix, c.CPUs))
	}
	if c.PidsLimit < 0 {
		problems = append(problems, fmt.Sprintf("%s.container: pids_limit must be >= 0 (got %d)", prefix, c.PidsLimit))
	}
	switch c.Network {
	case "", "none", "host":
		// "host" also needs Unsafe=true; checked below.
	default:
		if !strings.HasPrefix(c.Network, "bridge:") {
			problems = append(problems, fmt.Sprintf(
				"%s.container: unknown network %q (want 'none', 'host', or 'bridge:<name>')",
				prefix, c.Network))
		}
	}
	if c.Network == "host" && !c.Unsafe {
		problems = append(problems, fmt.Sprintf(
			"%s.container: network='host' requires unsafe=true (escape hatch; see docs/design/containerized-sessions.md §4)",
			prefix))
	}
	// Mount dst uniqueness — two mounts on the same path silently
	// overwrite each other in the final docker run, which is a
	// config bug we can catch cheaply.
	seenDst := map[string]int{}
	for i, m := range c.Mounts {
		if strings.TrimSpace(m.Dst) == "" {
			problems = append(problems, fmt.Sprintf("%s.container.mounts[%d]: dst required", prefix, i))
			continue
		}
		if first, dup := seenDst[m.Dst]; dup {
			problems = append(problems, fmt.Sprintf(
				"%s.container.mounts[%d]: duplicate dst %q (first at mounts[%d])",
				prefix, i, m.Dst, first))
		}
		seenDst[m.Dst] = i
		switch strings.ToLower(m.Mode) {
		case "", "rw", "ro", "tmpfs":
		default:
			problems = append(problems, fmt.Sprintf(
				"%s.container.mounts[%d]: unknown mode %q (want 'rw', 'ro', or 'tmpfs')",
				prefix, i, m.Mode))
		}
		if strings.ToLower(m.Mode) != "tmpfs" && strings.TrimSpace(m.Src) == "" {
			problems = append(problems, fmt.Sprintf(
				"%s.container.mounts[%d]: src required for bind-mount", prefix, i))
		}
	}
	return problems
}

func validPreamble(p PreambleStrategy) bool {
	switch p {
	case PreambleClaudeMD, PreambleFirstMessage, PreambleCodexExec, PreambleStdoutBanner:
		return true
	}
	return false
}

func validHook(h HookProfile) bool {
	switch h {
	case HookClaudeCode, HookPromptCommand, HookNone:
		return true
	}
	return false
}

func validInteractionMode(m InteractionMode) bool {
	switch m {
	case InteractionDangerous, InteractionBalanced, InteractionCautious:
		return true
	}
	return false
}
