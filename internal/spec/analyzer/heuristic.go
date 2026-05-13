package analyzer

import (
	"fmt"
	"regexp"
	"strings"
)

// heuristicClassify runs the deterministic, offline first-pass.
//
// Rules:
//   - The first H1 line ("# …") OR any line starting with "Milestone:"
//     becomes the root milestone bead.
//   - Each H2 line ("## …") becomes an epic, parented to the milestone.
//   - Each markdown checkbox line (e.g. "- [ ] do thing", "* [x] done")
//     becomes a story, parented to the most recent epic — or to the
//     milestone if no epic has appeared yet.
//   - Lines beginning with "[P0]".."[P3]" inside a story title set the
//     bead's Priority and are stripped from the title.
//   - If no milestone is found but stories exist, a synthetic milestone
//     titled "Imported tasks" is created so the proposal is well-formed.
//
// The classifier is intentionally tolerant: trailing whitespace, mixed
// list markers, and noisy prose lines are ignored rather than fatal.
func heuristicClassify(text string) *ProposalDoc {
	prop := &ProposalDoc{}

	var (
		milestoneIdx = -1
		lastEpicIdx  = -1
	)

	add := func(b ProposedBead) int {
		b.LocalRef = fmt.Sprintf("#%d", len(prop.Beads))
		prop.Beads = append(prop.Beads, b)
		return len(prop.Beads) - 1
	}

	parentRef := func(idx int) string {
		if idx < 0 {
			return ""
		}
		return prop.Beads[idx].LocalRef
	}

	lines := strings.Split(text, "\n")
	for _, raw := range lines {
		line := strings.TrimRight(raw, " \t\r")
		trim := strings.TrimSpace(line)
		if trim == "" {
			continue
		}
		switch {
		case strings.HasPrefix(trim, "# ") && milestoneIdx < 0:
			milestoneIdx = add(ProposedBead{
				Kind:      KindMilestone,
				Title:     strings.TrimSpace(strings.TrimPrefix(trim, "#")),
				Rationale: "H1 heading",
			})
		case strings.HasPrefix(trim, "Milestone:") && milestoneIdx < 0:
			milestoneIdx = add(ProposedBead{
				Kind:      KindMilestone,
				Title:     strings.TrimSpace(strings.TrimPrefix(trim, "Milestone:")),
				Rationale: "explicit Milestone: prefix",
			})
		case strings.HasPrefix(trim, "## "):
			// Ensure a milestone exists so the epic has a parent.
			if milestoneIdx < 0 {
				milestoneIdx = add(ProposedBead{
					Kind:      KindMilestone,
					Title:     "Imported tasks",
					Rationale: "synthetic — payload had H2 sections but no H1/Milestone:",
				})
			}
			lastEpicIdx = add(ProposedBead{
				Kind:      KindEpic,
				Title:     strings.TrimSpace(strings.TrimPrefix(trim, "##")),
				Parent:    parentRef(milestoneIdx),
				Rationale: "H2 heading",
			})
		default:
			if title, prio, ok := parseCheckbox(trim); ok {
				if milestoneIdx < 0 {
					milestoneIdx = add(ProposedBead{
						Kind:      KindMilestone,
						Title:     "Imported tasks",
						Rationale: "synthetic — payload had stories but no H1/Milestone:",
					})
				}
				parent := lastEpicIdx
				if parent < 0 {
					parent = milestoneIdx
				}
				add(ProposedBead{
					Kind:      KindStory,
					Title:     title,
					Parent:    parentRef(parent),
					Priority:  prio,
					Rationale: "markdown checkbox",
				})
			}
		}
	}
	return prop
}

// checkboxRe matches `- [ ] text`, `- [x] text`, `* [ ] text`, etc.
var checkboxRe = regexp.MustCompile(`^[-*+]\s*\[[ xX]\]\s+(.*)$`)
var prioRe = regexp.MustCompile(`^\[(P[0-3])\]\s+(.*)$`)

// parseCheckbox returns (title, priority, true) when trim is a markdown
// checkbox line, else ("", "", false). Priority is "" unless a leading
// [P0]..[P3] tag is present in the title.
func parseCheckbox(trim string) (title, priority string, ok bool) {
	m := checkboxRe.FindStringSubmatch(trim)
	if m == nil {
		return "", "", false
	}
	title = strings.TrimSpace(m[1])
	if pm := prioRe.FindStringSubmatch(title); pm != nil {
		priority = pm[1]
		title = strings.TrimSpace(pm[2])
	}
	return title, priority, true
}
