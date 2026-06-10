package newproject

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

// systemPromptRaw is the role-framing + output-schema fragment.
// Embedded so the binary ships self-contained -- no runtime
// dependency on a prompts directory at the install path.
//
//go:embed prompts/system.md
var systemPromptRaw string

// fewshotsRaw spans four project shapes (web app, library, ops
// tooling, research). Inlined into the system prompt so the model
// sees them as part of its instructions, not as a prior
// conversation turn the operator could overwrite.
//
//go:embed prompts/fewshots.md
var fewshotsRaw string

// SystemPrompt returns the full LLM system-prompt text the
// Onboarder persona splices into its prompt envelope. The host is
// free to wrap this in additional layers (workspace context,
// version banner, etc.) but MUST splice it verbatim -- the prompt
// pins the output JSON contract that ValidateState enforces, so
// any rewrite risks decoupling the prompt from the validator.
func SystemPrompt() string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(systemPromptRaw))
	b.WriteString("\n\n# Examples\n\n")
	b.WriteString(strings.TrimSpace(fewshotsRaw))
	b.WriteString("\n")
	return b.String()
}

// UserPrompt renders one conversational turn for the LLM. The
// host calls this with the prior state (already advanced through
// previous turns) plus the operator's new message; the result is
// fed verbatim as the user-role content.
//
// Layout choice: the prior state is rendered as JSON in a fenced
// block so the model can ground its update against it, and the
// operator message is the trailing free text. Keeping the state
// at the top (rather than the bottom) lets the model reason about
// it before reading the new instruction -- in practice this
// produces fewer "the model regenerated the tree from scratch"
// regressions on small edits.
func UserPrompt(prior NewProjectState, operatorMessage string) (string, error) {
	priorJSON, err := json.MarshalIndent(prior, "", "  ")
	if err != nil {
		// json.Marshal of NewProjectState should never fail with
		// the standard struct shape; treat any failure as a host
		// bug rather than user-actionable.
		return "", fmt.Errorf("newproject: marshal prior state: %w", err)
	}
	var b strings.Builder
	b.WriteString("## Prior NewProjectState\n\n")
	b.WriteString("```json\n")
	b.Write(priorJSON)
	b.WriteString("\n```\n\n")
	b.WriteString("## Operator message\n\n")
	if strings.TrimSpace(operatorMessage) == "" {
		// Empty messages happen on the very first turn (the host
		// kicks off with a generic greeting). Hint the model so it
		// produces an opener rather than asking what changed.
		b.WriteString("(no message yet -- this is the first turn; greet the operator and ask the focused clarifying question that gets the conversation moving)\n")
	} else {
		b.WriteString(operatorMessage)
		b.WriteString("\n")
	}
	b.WriteString("\nReturn the next state and reply now, in the JSON envelope described in the system prompt. No prose outside the JSON.")
	return b.String(), nil
}

// turnEnvelope is the wire shape the model returns. Kept private
// because it only exists to bridge the LLM JSON to the typed
// Run() return values; callers see (NewProjectState, string).
type turnEnvelope struct {
	State NewProjectState `json:"state"`
	Reply string          `json:"reply"`
}
