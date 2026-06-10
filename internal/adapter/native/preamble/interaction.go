package preamble

import (
	"os"
	"strings"

	"github.com/GembaCore/gemba-core/internal/adapter/native/agents"
)

// SelectProfileSection reads the interaction_profile at path and
// returns the body of the section whose heading matches mode
// (case-insensitive, H2 only). Returns the empty string when:
//   - path is empty,
//   - the file is missing or unreadable,
//   - no section matches the mode.
//
// The empty-return behaviour is load-bearing: a missing profile
// MUST NOT fail StartSession. The operator can start a session
// before authoring the file; the session just runs without
// interaction guidance baked into the preamble.
//
// The section parser treats `## <name>` as the heading delimiter.
// Anything between two H2 lines (or between an H2 and EOF) is the
// section body. Headings at deeper levels (### …) are part of the
// current section, not new boundaries.
func SelectProfileSection(path string, mode agents.InteractionMode) string {
	if path == "" || mode == "" {
		return ""
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return pickSection(string(raw), string(mode))
}

// pickSection is the pure parser, split out for test-ability.
func pickSection(source, mode string) string {
	want := strings.ToLower(strings.TrimSpace(mode))
	if want == "" {
		return ""
	}
	// Normalize line endings so CRLF-authored files behave like LF.
	source = strings.ReplaceAll(source, "\r\n", "\n")
	lines := strings.Split(source, "\n")

	var (
		inTarget bool
		out      []string
	)
	for _, line := range lines {
		heading, ok := parseH2(line)
		if ok {
			if inTarget {
				// Reached the next section boundary — stop collecting.
				break
			}
			if strings.EqualFold(heading, want) {
				inTarget = true
			}
			continue
		}
		if inTarget {
			out = append(out, line)
		}
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// parseH2 returns (heading, true) if line is an H2 markdown heading
// (e.g. "## dangerous"). Deeper (###+) and shallower (#) lines are
// not H2 and return false.
func parseH2(line string) (string, bool) {
	trimmed := strings.TrimLeft(line, " ")
	if !strings.HasPrefix(trimmed, "## ") {
		return "", false
	}
	// Reject "### " and deeper.
	if strings.HasPrefix(trimmed, "### ") {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(trimmed, "##")), true
}
