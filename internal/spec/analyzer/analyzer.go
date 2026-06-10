// Package analyzer converts a rejected tasks.md / todo.md payload into a
// structured Proposal of beads (milestones / epics / stories) that the
// caller can review and optionally materialize via `bd create`.
//
// The analyzer is the consumer side of the hook seam described in
// internal/spec/hook: when the PreToolUse hook rejects a Write/Edit to
// tasks.md, it preserves the raw payload and exposes it via the
// GEMBA_REJECTED_PAYLOAD env var. `gemba spec analyze` then feeds that
// payload here.
//
// Two layers:
//
//  1. heuristic.go — deterministic, offline classifier. This is the
//     baseline and always runs. Rules in short:
//     * H1 line OR a line starting with "Milestone:" → milestone
//     * H2 sections (## …) → epics
//     * markdown checkbox lines (- [ ] / - [x] / * [ ] …) → stories
//
//  2. llm.go — opt-in refinement. If GEMBA_ANALYZER_LLM is set, the
//     heuristic proposal + raw payload are POSTed to a configurable
//     endpoint and the response replaces (or augments) the proposal.
//     With the env var unset, llm.go is a no-op and the heuristic
//     proposal is returned verbatim.
package analyzer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Kind is the bead kind a ProposedBead would materialize as.
type Kind string

const (
	KindMilestone Kind = "milestone"
	KindEpic      Kind = "epic"
	KindStory     Kind = "story"
)

// ProposedBead is one bead the analyzer suggests creating.
type Proposal = ProposalDoc

// ProposalDoc is the top-level result of analysis. It is intentionally a
// flat slice of ProposedBead with `Parent` cross-references rather than a
// nested tree so that JSON/table rendering and bd-create shellouts are
// straightforward.
type ProposalDoc struct {
	Beads  []ProposedBead `json:"beads"`
	Source string         `json:"source"` // "heuristic" or "heuristic+llm"
}

// ProposedBead is one suggested bead.
type ProposedBead struct {
	Kind      Kind     `json:"kind"`
	Title     string   `json:"title"`
	Parent    string   `json:"parent,omitempty"`    // local index reference like "#0", or external bead id once applied
	Labels    []string `json:"labels,omitempty"`    // additional topical labels
	Priority  string   `json:"priority,omitempty"`  // "P0"..."P3"; empty if heuristic could not infer
	Rationale string   `json:"rationale,omitempty"` // why the analyzer thinks this
	// LocalRef is a stable index ("#0", "#1", …) used by Parent before any
	// bead is actually created. It is preserved through LLM refinement.
	LocalRef string `json:"local_ref,omitempty"`
}

// Options controls Analyze behavior.
type Options struct {
	// LLMEndpoint, if non-empty, enables LLM refinement. Normally set
	// from the GEMBA_ANALYZER_LLM env var.
	LLMEndpoint string
	// LLMTimeoutMS is the per-call timeout for LLM refinement.
	LLMTimeoutMS int
}

// Analyze parses payload (raw stdin JSON from a rejected hook OR plain
// markdown text — both are accepted) and returns a Proposal.
func Analyze(ctx context.Context, payload string, opts Options) (*Proposal, error) {
	text := extractMarkdown(payload)
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("analyzer: empty payload")
	}
	prop := heuristicClassify(text)
	prop.Source = "heuristic"
	if opts.LLMEndpoint != "" {
		refined, err := llmRefine(ctx, opts.LLMEndpoint, opts.LLMTimeoutMS, text, prop)
		if err == nil && refined != nil {
			refined.Source = "heuristic+llm"
			return refined, nil
		}
		// LLM failure is non-fatal — surface as rationale on the
		// heuristic proposal so the operator sees we tried.
		if err != nil && len(prop.Beads) > 0 {
			prop.Beads[0].Rationale = appendRationale(prop.Beads[0].Rationale,
				fmt.Sprintf("(llm refinement skipped: %v)", err))
		}
	}
	return prop, nil
}

// extractMarkdown unwraps Claude Code's PreToolUse JSON payload when the
// raw bytes look like one ({"tool_name":..,"tool_input":{"content":..}}).
// Otherwise the payload is treated as the markdown body itself.
func extractMarkdown(raw string) string {
	trim := strings.TrimSpace(raw)
	if !strings.HasPrefix(trim, "{") {
		return raw
	}
	var env struct {
		ToolInput struct {
			Content  string `json:"content"`
			NewText  string `json:"new_string"`
			FilePath string `json:"file_path"`
		} `json:"tool_input"`
	}
	if err := json.Unmarshal([]byte(trim), &env); err != nil {
		return raw
	}
	if env.ToolInput.Content != "" {
		return env.ToolInput.Content
	}
	if env.ToolInput.NewText != "" {
		return env.ToolInput.NewText
	}
	return raw
}

func appendRationale(existing, add string) string {
	if existing == "" {
		return add
	}
	return existing + " " + add
}
