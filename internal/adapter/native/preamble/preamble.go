// Package preamble composes the project / workspace / work-item
// context layers (gm-native.10) into the format the operator's
// chosen agent type expects. Project + workspace sources are
// filesystem-only for MVP; bead content is passed in by the
// adaptor. The package has no dependency on the WorkPlane — that
// keeps it trivially testable without a fake.
package preamble

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MikeBengtson/gemba/internal/adapter/native/agents"
	"github.com/MikeBengtson/gemba/internal/adapter/native/dod"
	"github.com/MikeBengtson/gemba/internal/core"
	"github.com/MikeBengtson/gemba/internal/core/prompt"
)

// Sentinel pair brackets the gemba-managed block in CLAUDE.md. Only
// content between these exact lines is ever touched — the operator's
// hand-authored sections stay intact.
const (
	sentinelBegin = "<!-- gemba:preamble:begin -->"
	sentinelEnd   = "<!-- gemba:preamble:end -->"
)

// Sources is the file layer input. Paths are resolved relative to
// RepoRoot and WorkspaceDir as appropriate; missing files are
// treated as empty layers (not errors).
type Sources struct {
	// RepoRoot contains the .gemba/goals.md, .gemba/values.md, and
	// .gemba/guardrails.md project-level files.
	RepoRoot string
	// WorkspaceDir is the worktree the session runs in; .gemba/
	// workspace.md here provides workspace-scoped overrides.
	WorkspaceDir string
	// InteractionProfilePath is the absolute path to the
	// interaction_profile.md whose section matching InteractionMode
	// gets composed as the PersonaSystem layer. Empty string →
	// no interaction guidance injected (gm-bglh / gm-97w7).
	InteractionProfilePath string
	// InteractionMode picks which section of the profile above is
	// injected (dangerous | balanced | cautious). Empty string →
	// no injection.
	InteractionMode agents.InteractionMode
}

// Build reads the sources + work item and returns a composed prompt
// envelope rendering. The render is what goes into CLAUDE.md or the
// first-message, depending on agent strategy.
func Build(s Sources, item core.WorkItem) prompt.Composed {
	env := prompt.Envelope{
		ProjectGoals:      readLines(filepath.Join(s.RepoRoot, ".gemba", "goals.md")),
		ProjectValues:     readLines(filepath.Join(s.RepoRoot, ".gemba", "values.md")),
		ProjectGuardrails: readLines(filepath.Join(s.RepoRoot, ".gemba", "guardrails.md")),
		WorkspaceValues:   readLines(filepath.Join(s.WorkspaceDir, ".gemba", "workspace.md")),
		PersonaSystem:     SelectProfileSection(s.InteractionProfilePath, s.InteractionMode),
		UserInput:         renderWorkItem(item),
	}
	return env.Compose(prompt.ComposeOptions{})
}

// ResolveProfilePath picks the absolute path to the
// interaction_profile.md to use for a session. Agent-level override
// wins over the project default. Empty return means no profile.
func ResolveProfilePath(repoRoot string, agentProfile string) string {
	if agentProfile != "" {
		if filepath.IsAbs(agentProfile) {
			return agentProfile
		}
		return filepath.Join(repoRoot, agentProfile)
	}
	p := filepath.Join(repoRoot, ".gemba", "interaction_profile.md")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return ""
}

// renderWorkItem formats the bead as the user-input layer of the
// envelope. Headings mirror ui-spec §7 (Work item drawer) so agents
// see the same structure the operator does.
func renderWorkItem(item core.WorkItem) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Assigned bead: %s\n\n", item.ID)
	if item.Title != "" {
		fmt.Fprintf(&b, "**Title**: %s\n\n", item.Title)
	}
	if strings.TrimSpace(item.Description) != "" {
		b.WriteString("## Description\n\n")
		b.WriteString(strings.TrimSpace(item.Description))
		b.WriteString("\n\n")
	}
	renderDoD(&b, item)
	if len(item.Labels) > 0 {
		fmt.Fprintf(&b, "**Labels**: %s\n\n", strings.Join(item.Labels, ", "))
	}
	b.WriteString("## Your task\n\nWork this bead. Push when the DoD is met. Escalate (permission prompt) if blocked.\n")
	return b.String()
}

// renderDoD emits either the operator-authored DoD verbatim or the
// synthesized default with a clear "synthesized" banner. The banner
// lets the agent decide how much weight to give the criteria.
func renderDoD(b *strings.Builder, item core.WorkItem) {
	if dod.NeedsDefault(item) {
		synth := dod.Synthesize(item)
		b.WriteString("## Definition of Done (synthesized — edit the bead to override)\n\n")
		for _, c := range synth.AcceptanceCriteria {
			fmt.Fprintf(b, "- %s\n", c)
		}
		if synth.Notes != "" {
			fmt.Fprintf(b, "\n%s\n", synth.Notes)
		}
		b.WriteString("\n")
		return
	}
	b.WriteString("## Definition of Done\n\n")
	for _, c := range item.DoD.AcceptanceCriteria {
		fmt.Fprintf(b, "- %s\n", c)
	}
	if strings.TrimSpace(item.DoD.Notes) != "" {
		fmt.Fprintf(b, "\n%s\n", strings.TrimSpace(item.DoD.Notes))
	}
	b.WriteString("\n")
}

// readLines loads a markdown file as a slice of bullet items. Every
// non-empty line (after trimming bullet prefix) becomes one Envelope
// entry. Missing file → empty slice.
func readLines(path string) []string {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []string
	for _, raw := range strings.Split(string(b), "\n") {
		line := strings.TrimSpace(raw)
		line = strings.TrimPrefix(line, "- ")
		line = strings.TrimPrefix(line, "* ")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out
}

// ApplyToClaudeMD installs the composed text as a sentinel-bracketed
// block in <workspace>/CLAUDE.md, preserving everything outside the
// sentinels. Idempotent: calling twice with the same text leaves the
// file byte-identical.
func ApplyToClaudeMD(workspace string, composed prompt.Composed) error {
	path := filepath.Join(workspace, "CLAUDE.md")
	existing, _ := os.ReadFile(path) // missing file -> empty, which is fine
	updated := replaceSentinelBlock(string(existing), composed.Text)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("preamble: mkdir %s: %w", filepath.Dir(path), err)
	}
	return os.WriteFile(path, []byte(updated), 0o644)
}

// RemoveFromClaudeMD strips the sentinel-bracketed block on session
// end so the operator's CLAUDE.md returns to its pre-session shape.
// Missing file or missing sentinels are no-ops.
func RemoveFromClaudeMD(workspace string) error {
	path := filepath.Join(workspace, "CLAUDE.md")
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("preamble: read %s: %w", path, err)
	}
	cleaned := stripSentinelBlock(string(b))
	if cleaned == string(b) {
		return nil
	}
	if strings.TrimSpace(cleaned) == "" {
		return os.Remove(path)
	}
	return os.WriteFile(path, []byte(cleaned), 0o644)
}

func replaceSentinelBlock(existing, body string) string {
	block := sentinelBegin + "\n" + strings.TrimRight(body, "\n") + "\n" + sentinelEnd + "\n"
	if !strings.Contains(existing, sentinelBegin) {
		if existing != "" && !strings.HasSuffix(existing, "\n") {
			existing += "\n"
		}
		return existing + block
	}
	return stripSentinelBlock(existing) + block
}

func stripSentinelBlock(s string) string {
	start := strings.Index(s, sentinelBegin)
	if start < 0 {
		return s
	}
	end := strings.Index(s, sentinelEnd)
	if end < 0 {
		return s
	}
	end += len(sentinelEnd)
	if end < len(s) && s[end] == '\n' {
		end++
	}
	return s[:start] + s[end:]
}

// ApplyStrategy picks the right injection for the agent type. Called
// from the adaptor's StartSession after the pane is spawned and
// before the first-message dispatch.
type ApplyStrategy struct {
	// FirstMessage is the text the caller should SendKeys into the
	// session's terminal after the agent CLI is running. Empty when
	// the strategy relies on CLAUDE.md only.
	FirstMessage string
}

// Apply runs the strategy for the agent. Claude gets CLAUDE.md +
// first-message (per operator direction, gm-native discussion).
// Shell-only gets a stdout banner as the first-message.
func Apply(workspace string, agent agents.AgentType, composed prompt.Composed) (ApplyStrategy, error) {
	switch agent.Preamble {
	case agents.PreambleClaudeMD:
		if err := ApplyToClaudeMD(workspace, composed); err != nil {
			return ApplyStrategy{}, err
		}
		// First-message is a pointer back to CLAUDE.md so the
		// agent's attention starts on the right bead. Keep it
		// short — the composed preamble is already in CLAUDE.md.
		return ApplyStrategy{
			FirstMessage: "Your session context (bead + DoD + project values) has been appended to CLAUDE.md. Please read it and begin.",
		}, nil
	case agents.PreambleFirstMessage:
		return ApplyStrategy{FirstMessage: composed.Text}, nil
	case agents.PreambleStdoutBanner:
		// shell-only: echo a short banner. The full text goes to
		// stdout via a `cat <<EOF` style message the backend will
		// execute as a shell command.
		return ApplyStrategy{
			FirstMessage: stdoutBanner(composed.Text),
		}, nil
	}
	return ApplyStrategy{}, fmt.Errorf("preamble: unknown strategy %q", agent.Preamble)
}

// stdoutBanner wraps the composed text so `SendKeys` can pipe it
// through the shell safely as a heredoc. Using a stable, uncommon
// delimiter avoids collisions with the preamble content.
func stdoutBanner(text string) string {
	delim := "GEMBA_PREAMBLE_EOF"
	return fmt.Sprintf("cat <<'%s'\n%s\n%s", delim, text, delim)
}
