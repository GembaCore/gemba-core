package bridge

import (
	"strings"

	"github.com/GembaCore/gemba-core/core"
)

// SkillSection is one extracted "## Questions" or "## Blockers" block
// from an assistant turn (gm-97w7.1). Items are the parsed
// ordered-list entries; whitespace-only lines and continuation
// indent are folded into their parent item.
type SkillSection struct {
	Kind  core.EscalationKind // EscalationQuestion | EscalationBlocker
	Items []string
}

// InteractionMode is the three-way knob from .gemba/agents.toml that
// governs whether a surfaced section blocks the agent. The scanner
// maps (Kind, InteractionMode) → core.EscalationUrgency per the
// contract in docs/design/skill-authoring-contract.md.
type InteractionMode string

const (
	ModeDangerous InteractionMode = "dangerous"
	ModeBalanced  InteractionMode = "balanced"
	ModeCautious  InteractionMode = "cautious"
)

// Classify returns the urgency a given section should carry under the
// active interaction mode, along with emit=false when the mode says
// the section shouldn't be surfaced at all (dangerous mode treats any
// surfaced section as a skill-authoring bug — the scanner logs and
// drops rather than opening an escalation).
//
// Matrix (mirrors the profile table):
//
//	mode       | Question          | Blocker
//	dangerous  | drop              | drop
//	balanced   | advisory          | blocking
//	cautious   | blocking          | blocking
func Classify(kind core.EscalationKind, mode InteractionMode) (core.EscalationUrgency, bool) {
	switch mode {
	case ModeDangerous:
		// Agent shouldn't have emitted anything; the profile said so.
		return "", false
	case ModeBalanced:
		if kind == core.EscalationBlocker {
			return core.UrgencyBlocking, true
		}
		return core.UrgencyAdvisory, true
	case ModeCautious:
		return core.UrgencyBlocking, true
	default:
		// Unknown mode — be conservative, treat as balanced.
		if kind == core.EscalationBlocker {
			return core.UrgencyBlocking, true
		}
		return core.UrgencyAdvisory, true
	}
}

// ExtractSections scans an assistant-message body for "## Questions"
// and "## Blockers" sections (gm-97w7.1). Only the final occurrence of
// each heading wins — earlier drafts in the same turn are ignored, so
// the agent can rewrite its mind mid-response without confusing the
// surface.
//
// Rules (mirrors docs/design/skill-authoring-contract.md):
//
//   - Heading match is exact: `## Questions` or `## Blockers`, no
//     trailing punctuation, case-sensitive. Sub-headings
//     (`### Questions`) and alternative words are ignored.
//   - Items are numbered list entries (`1. `, `2. `, …). Bullet lists
//     are ignored so narrative under a heading isn't misread.
//   - An item's continuation lines (leading whitespace) are folded
//     into the item, joined by a single space.
//   - A blank line inside a section does NOT end the section — the
//     scanner keeps consuming until the next heading (any level) or
//     EOF.
//   - Returns sections in the order Questions-first then Blockers,
//     regardless of which appeared first in the source.
func ExtractSections(markdown string) []SkillSection {
	qItems := extractFinalList(markdown, "## Questions")
	bItems := extractFinalList(markdown, "## Blockers")
	var out []SkillSection
	if len(qItems) > 0 {
		out = append(out, SkillSection{Kind: core.EscalationQuestion, Items: qItems})
	}
	if len(bItems) > 0 {
		out = append(out, SkillSection{Kind: core.EscalationBlocker, Items: bItems})
	}
	return out
}

// extractFinalList walks the markdown line-by-line and returns the
// numbered-list items under the last occurrence of heading. Returns
// nil when the heading is absent or followed by no numbered items.
func extractFinalList(markdown string, heading string) []string {
	lines := strings.Split(markdown, "\n")

	// Find every line index that opens the target heading. The final
	// index wins so a draft earlier in the response is harmless.
	var lastStart = -1
	for i, raw := range lines {
		if strings.TrimRight(raw, " \t\r") == heading {
			lastStart = i
		}
	}
	if lastStart == -1 {
		return nil
	}

	// Parse numbered items between `lastStart+1` and the next heading
	// (any level, `^#+ `) or EOF.
	var items []string
	var current strings.Builder
	flush := func() {
		s := strings.TrimSpace(current.String())
		current.Reset()
		if s != "" {
			items = append(items, s)
		}
	}
	for i := lastStart + 1; i < len(lines); i++ {
		raw := lines[i]
		trimmed := strings.TrimRight(raw, " \t\r")

		if isHeading(trimmed) {
			break
		}
		if isNumberedItem(trimmed) {
			// Starting a new item — flush the previous one.
			flush()
			// Strip the "1." / "12." prefix.
			rest := trimAfterNumber(trimmed)
			current.WriteString(rest)
			continue
		}
		// Continuation: an indented non-blank line extends the
		// current item. Blank lines are ignored (they don't end
		// the section). Un-indented prose at column 0 is treated
		// as ambient text — neither a continuation nor a section
		// terminator — so a closing paragraph after the last item
		// doesn't get folded into it.
		if current.Len() == 0 {
			continue
		}
		if raw == "" || strings.TrimSpace(raw) == "" {
			continue
		}
		if raw[0] != ' ' && raw[0] != '\t' {
			// Un-indented text after an item ends that item but
			// keeps the section open in case more numbered items
			// follow. Flush now so ambient prose doesn't glue on.
			flush()
			continue
		}
		continuation := strings.TrimSpace(raw)
		current.WriteString(" ")
		current.WriteString(continuation)
	}
	flush()
	return items
}

func isHeading(line string) bool {
	i := 0
	for i < len(line) && line[i] == '#' {
		i++
	}
	return i > 0 && i < len(line) && line[i] == ' '
}

// isNumberedItem matches "<digits>. " at the start of line. A leading
// indent (up to three spaces, per CommonMark) is tolerated so the
// skill author's rendering doesn't have to be flush-left.
func isNumberedItem(line string) bool {
	i := 0
	// Up to 3 leading spaces.
	for i < len(line) && i < 3 && line[i] == ' ' {
		i++
	}
	start := i
	for i < len(line) && line[i] >= '0' && line[i] <= '9' {
		i++
	}
	if i == start {
		return false
	}
	if i >= len(line) || line[i] != '.' {
		return false
	}
	i++
	return i < len(line) && line[i] == ' '
}

// trimAfterNumber returns the body of a numbered-list line, i.e. the
// text after "1. ". Caller MUST have already verified isNumberedItem.
func trimAfterNumber(line string) string {
	// Skip leading indent + digits + "." + " ".
	i := 0
	for i < len(line) && i < 3 && line[i] == ' ' {
		i++
	}
	for i < len(line) && line[i] >= '0' && line[i] <= '9' {
		i++
	}
	// '.' ' '
	i += 2
	if i > len(line) {
		return ""
	}
	return strings.TrimSpace(line[i:])
}
