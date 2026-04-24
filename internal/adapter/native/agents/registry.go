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
}

// ResolvedInteractionMode returns the agent's configured mode with
// the default applied when the field is blank.
func (a AgentType) ResolvedInteractionMode() InteractionMode {
	if a.InteractionMode == "" {
		return DefaultInteractionMode
	}
	return a.InteractionMode
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
	}
	if len(problems) > 0 {
		return fmt.Errorf("%s", strings.Join(problems, "; "))
	}
	return nil
}

func validPreamble(p PreambleStrategy) bool {
	switch p {
	case PreambleClaudeMD, PreambleFirstMessage, PreambleStdoutBanner:
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
