// Package prompt: see doc.go for the overview.
package prompt

import (
	"sort"
	"strings"
)

// Layer names the composition slots in outer-to-inner order. The order is
// load-bearing: downstream LLMs weight earlier layers as the enduring
// frame and later layers as the specific task. Do not reorder without
// updating DefaultOrder and the gm-3d6 design note.
type Layer string

const (
	// LayerProjectValues — project-level values (innovation, transparency,
	// execution, empathy, and any user-added values). Outermost because it
	// is the frame that shapes every invocation in the project.
	LayerProjectValues Layer = "project_values"
	// LayerProjectGuardrails — values stated as explicit blockers ("never
	// ship without tests", "never exfiltrate secrets"). Outer because
	// guardrails must bind every downstream layer.
	LayerProjectGuardrails Layer = "project_guardrails"
	// LayerProjectGoals — project goals plus the active epic's goal.
	// Supplies the "where we're going" context that steers choices.
	LayerProjectGoals Layer = "project_goals"
	// LayerWorkspaceValues — overrides/adds that apply only in this
	// workspace (per-workspace mode in gm-518 family). Applied after
	// project-level layers so a workspace can tune but not erase them.
	LayerWorkspaceValues Layer = "workspace_values"
	// LayerPersonaSystem — the persona's role-specific system prompt.
	LayerPersonaSystem Layer = "persona_system"
	// LayerSkill — the skill-specific task shape and output schema.
	LayerSkill Layer = "skill"
	// LayerContext — data selected per-invocation by context providers
	// (project summary, state, decisions, issues, bead databases).
	LayerContext Layer = "context"
	// LayerUserInput — the specific request for this invocation.
	// Innermost and usually unlimited so the caller's intent is never
	// clipped.
	LayerUserInput Layer = "user_input"
)

// DefaultOrder is the canonical outer-to-inner layer order. Compose walks
// layers in this order; tests rely on the slice matching the Layer
// constants above.
var DefaultOrder = []Layer{
	LayerProjectValues,
	LayerProjectGuardrails,
	LayerProjectGoals,
	LayerWorkspaceValues,
	LayerPersonaSystem,
	LayerSkill,
	LayerContext,
	LayerUserInput,
}

// Envelope carries the pieces PromptEnvelope composes into a final
// prompt. Every field is independently nullable: a project without
// declared values still produces a valid prompt (the project_values
// layer is simply omitted from the output).
//
// Slice-valued layers are ordered oldest-first. When a layer exceeds its
// token budget, Compose drops items from the front until the remainder
// fits — i.e. the freshest items at the back are preserved.
type Envelope struct {
	ProjectValues     []string
	ProjectGuardrails []string
	ProjectGoals      []string
	WorkspaceValues   []string
	PersonaSystem     string
	Skill             string
	ContextChunks     []string
	UserInput         string
}

// DefaultBudget returns the gm-3d6 budget (per layer, in tokens). Zero
// means "no limit" for that layer. User input is unlimited on purpose:
// clipping the caller's request silently is worse than a long prompt.
func DefaultBudget() Budget {
	return Budget{
		LayerProjectValues:     500,
		LayerProjectGuardrails: 500,
		LayerProjectGoals:      1000,
		LayerWorkspaceValues:   500,
		LayerPersonaSystem:     2000,
		LayerSkill:             2000,
		LayerContext:           4000,
		LayerUserInput:         0,
	}
}

// Budget maps each Layer to its max token count. A zero value means the
// layer is unbounded. A missing key is treated the same as zero.
type Budget map[Layer]int

// TokenCounter counts tokens for a given string. Implementations may
// wrap tiktoken, Anthropic's tokenizer, or any heuristic. The default
// (Heuristic) keeps core self-contained and deterministic across
// platforms; callers that need accuracy inject a real tokenizer.
type TokenCounter interface {
	Count(s string) int
}

// Heuristic approximates token count as ceil(runeCount / 4). It is
// deterministic, allocation-free, and close enough for the budgeting
// decisions PromptEnvelope makes (which item to drop next). Callers
// needing model-accurate counts inject a real tokenizer via
// ComposeOptions.TokenCounter.
type Heuristic struct{}

// Count returns an approximate token count for s. Empty returns 0.
func (Heuristic) Count(s string) int {
	if s == "" {
		return 0
	}
	n := 0
	for range s {
		n++
	}
	// Round up so a 3-rune string counts as 1 token, not 0.
	return (n + 3) / 4
}

// ComposeOptions carries optional knobs for Compose. A zero-value
// ComposeOptions selects the defaults: DefaultBudget, Heuristic token
// counter, and DefaultOrder.
type ComposeOptions struct {
	// Budget overrides per-layer token caps. Merge semantics: any layer
	// present here replaces the DefaultBudget entry; layers absent fall
	// back to DefaultBudget. Pass an explicit zero in a layer entry to
	// mark that layer unbounded.
	Budget Budget

	// Counter is the token counter. Nil means Heuristic{}.
	Counter TokenCounter

	// Order overrides the layer order. Nil means DefaultOrder. Layers
	// repeated in Order render repeatedly — usually a bug, but allowed so
	// tests can validate ordering faithfully.
	Order []Layer
}

// LayerOutput reports what Compose emitted for one layer — the
// rendered text, its token count, and how much was dropped to fit
// the budget. Consumers (UI, tests, tracing) use this to diagnose
// truncation without re-parsing the final prompt.
type LayerOutput struct {
	Layer        Layer
	Text         string
	Tokens       int
	Budget       int
	Truncated    bool
	DroppedItems int
}

// Composed is the result of Compose: the final prompt text, the
// per-layer diagnostics, and the overall token count. Text is
// deterministic across runs with identical inputs.
type Composed struct {
	Text        string
	Layers      []LayerOutput
	TotalTokens int
}

// Compose assembles the final prompt from e. It walks the configured
// Order, renders each layer's items, enforces per-layer token budgets
// by dropping oldest items first, and concatenates the surviving
// layer texts in order. Layers that produce no content (nil input or
// empty after truncation) are skipped so a project without declared
// values simply omits that section.
//
// Compose is pure: no I/O, no randomness, no goroutines. Given the
// same Envelope, ComposeOptions, and TokenCounter, the returned
// Composed value is byte-identical.
func (e Envelope) Compose(opts ComposeOptions) Composed {
	order := opts.Order
	if order == nil {
		order = DefaultOrder
	}
	counter := opts.Counter
	if counter == nil {
		counter = Heuristic{}
	}
	budget := mergedBudget(opts.Budget)

	out := Composed{Layers: make([]LayerOutput, 0, len(order))}
	sections := make([]string, 0, len(order))

	for _, layer := range order {
		items := e.layerItems(layer)
		if len(items) == 0 {
			continue
		}
		lo := renderLayer(layer, items, budget[layer], counter)
		if lo.Text == "" {
			// Whole layer was truncated away; skip emission but surface
			// the diagnostic so callers see what happened.
			if lo.DroppedItems > 0 || lo.Truncated {
				out.Layers = append(out.Layers, lo)
			}
			continue
		}
		out.Layers = append(out.Layers, lo)
		sections = append(sections, lo.Text)
		out.TotalTokens += lo.Tokens
	}

	out.Text = strings.Join(sections, "\n\n")
	return out
}

// mergedBudget overlays the caller's budget on top of DefaultBudget.
// Keys the caller sets win; keys the caller omits fall through to the
// default. Returns a fresh map — never mutates the input.
func mergedBudget(b Budget) Budget {
	merged := DefaultBudget()
	for k, v := range b {
		merged[k] = v
	}
	return merged
}

// layerItems returns the items for a layer. Unknown layers return nil,
// which makes Compose skip them — this keeps the function
// forward-compatible with user-defined layers in Order.
func (e Envelope) layerItems(l Layer) []string {
	switch l {
	case LayerProjectValues:
		return cleanItems(e.ProjectValues)
	case LayerProjectGuardrails:
		return cleanItems(e.ProjectGuardrails)
	case LayerProjectGoals:
		return cleanItems(e.ProjectGoals)
	case LayerWorkspaceValues:
		return cleanItems(e.WorkspaceValues)
	case LayerPersonaSystem:
		return cleanSingle(e.PersonaSystem)
	case LayerSkill:
		return cleanSingle(e.Skill)
	case LayerContext:
		return cleanItems(e.ContextChunks)
	case LayerUserInput:
		return cleanSingle(e.UserInput)
	default:
		return nil
	}
}

// cleanItems strips purely-whitespace entries while preserving order.
// An all-empty slice returns nil so the layer is skipped.
func cleanItems(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if strings.TrimSpace(s) == "" {
			continue
		}
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// cleanSingle wraps a possibly-empty string into the items shape used
// by every other layer.
func cleanSingle(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return []string{s}
}

// renderLayer enforces the layer's token budget by dropping oldest
// items first, then assembles the final layer text. The rendered form
// wraps the surviving items in an XML-like tag (`<project-values>
// ...</project-values>`) so downstream LLMs can anchor on the section
// boundaries without ambiguity.
//
// When the budget is zero the layer is unbounded. When a single item
// alone exceeds the budget, renderLayer still emits it — truncating
// mid-item would mangle the content more than the overrun hurts.
// Truncated + DroppedItems surface that decision to callers.
func renderLayer(layer Layer, items []string, budget int, counter TokenCounter) LayerOutput {
	out := LayerOutput{Layer: layer, Budget: budget}

	kept := items
	if budget > 0 {
		kept = dropOldestUntilFits(items, budget, counter)
		out.DroppedItems = len(items) - len(kept)
		out.Truncated = out.DroppedItems > 0
	}

	if len(kept) == 0 {
		return out
	}

	body := strings.Join(kept, "\n")
	tag := layerTag(layer)
	text := "<" + tag + ">\n" + body + "\n</" + tag + ">"

	out.Text = text
	out.Tokens = counter.Count(text)
	return out
}

// dropOldestUntilFits drops items from the front until the remaining
// items' total token count plus the fixed wrapper overhead fits within
// budget. The wrapper overhead is approximated as 8 tokens (open tag +
// close tag + newlines), a conservative constant that keeps the
// function pure. If every item would need to be dropped to fit,
// dropOldestUntilFits returns the single newest item — emitting a
// minimal layer beats dropping it entirely when the caller explicitly
// provided content.
func dropOldestUntilFits(items []string, budget int, counter TokenCounter) []string {
	const wrapperOverhead = 8

	total := wrapperOverhead
	for _, it := range items {
		total += counter.Count(it)
	}
	if total <= budget {
		return items
	}

	i := 0
	for i < len(items)-1 && total > budget {
		total -= counter.Count(items[i])
		i++
	}
	return items[i:]
}

// layerTag returns the XML-like tag used to wrap a layer's content.
// Tags use kebab-case to match common LLM prompt conventions.
func layerTag(l Layer) string {
	return strings.ReplaceAll(string(l), "_", "-")
}

// SortedLayers returns a stable, sorted copy of a Budget's keys.
// Exposed for tests and tooling that need to enumerate a Budget
// deterministically without pulling in sort at the call site.
func SortedLayers(b Budget) []Layer {
	out := make([]Layer, 0, len(b))
	for k := range b {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
