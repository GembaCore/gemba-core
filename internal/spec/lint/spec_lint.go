package lint

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/GembaCore/gemba-core/internal/spec/constitution"
)

// Lint validates a single spec.md against the rules expressed in the
// constitution at constitutionPath. If constitutionPath is empty, the function
// returns an empty slice (no rules enabled).
//
// Each finding pins an Anchor of the form "<specPath>:<line>" or
// "<specPath>:<section>:<line>" to make CI surfaces clickable.
func Lint(specPath, constitutionPath string) ([]Finding, error) {
	data, err := os.ReadFile(specPath)
	if err != nil {
		return nil, err
	}
	var c constitution.Constitution
	if constitutionPath != "" {
		cb, err := os.ReadFile(constitutionPath)
		if err != nil {
			return nil, err
		}
		c = constitution.Parse(cb)
	}

	fm, fmEndLine := parseFrontmatter(data)
	stories := parseStories(data, fmEndLine)

	var findings []Finding

	// Rule: require_decision_parent
	if c.RequireDecisionParentEffective() {
		if _, ok := fm["decision_parent"]; !ok {
			findings = append(findings, Finding{
				Anchor:   fmt.Sprintf("%s:1", specPath),
				Rule:     "require_decision_parent",
				Severity: "error",
				Message:  "spec frontmatter is missing required key 'decision_parent'",
			})
		}
	}

	// Rule: require_priority — every Story has Priority:
	if c.RequirePriorityEffective() {
		for _, st := range stories {
			if !st.HasPriority {
				findings = append(findings, Finding{
					Anchor:   fmt.Sprintf("%s:%d", specPath, st.StartLine),
					Rule:     "require_priority",
					Severity: "error",
					Message:  fmt.Sprintf("Story %q is missing 'Priority:'", st.Title),
				})
			}
		}
	}

	// Rule: min_ac_count
	if n := c.MinACCountEffective(); n > 0 {
		for _, st := range stories {
			if st.ACCount < n {
				findings = append(findings, Finding{
					Anchor:   fmt.Sprintf("%s:%d", specPath, st.StartLine),
					Rule:     "min_ac_count",
					Severity: "error",
					Message:  fmt.Sprintf("Story %q has %d AC items; constitution requires >=%d", st.Title, st.ACCount, n),
				})
			}
		}
	}

	// Rule: forbid_orphan_beads — checked only when enabled AND a decision_parent
	// is present. The actual bead lookup requires a server connection, so we
	// emit a deferred-check finding describing what must be verified by the
	// reconciler. This keeps the linter offline-safe and CI-deterministic.
	if c.ForbidOrphanBeadsEffective() {
		dp, ok := fm["decision_parent"]
		if !ok {
			// require_decision_parent already reports the missing key when on;
			// add a separate finding so this rule fires independently.
			findings = append(findings, Finding{
				Anchor:   fmt.Sprintf("%s:1", specPath),
				Rule:     "forbid_orphan_beads",
				Severity: "error",
				Message:  "forbid_orphan_beads requires a decision_parent in frontmatter",
			})
		} else {
			findings = append(findings, Finding{
				Anchor:   fmt.Sprintf("%s:1", specPath),
				Rule:     "forbid_orphan_beads",
				Severity: "warn",
				Message:  fmt.Sprintf("deferred: verify every story bead under %s carries spec=%s", dp, specPath),
			})
		}
	}

	return findings, nil
}

// --- frontmatter ---

// parseFrontmatter extracts a leading YAML frontmatter block (--- … ---) into
// a flat string→string map. Only first-level key:value pairs are honored.
// Returns the map and the line number (1-based) immediately after the closing
// "---" (or 0 if no frontmatter).
func parseFrontmatter(data []byte) (map[string]string, int) {
	out := map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	line := 0
	if !sc.Scan() {
		return out, 0
	}
	line++
	if strings.TrimSpace(sc.Text()) != "---" {
		return out, 0
	}
	kv := regexp.MustCompile(`^\s*([A-Za-z_][A-Za-z0-9_]*)\s*:\s*(.*?)\s*$`)
	for sc.Scan() {
		line++
		t := sc.Text()
		if strings.TrimSpace(t) == "---" {
			return out, line
		}
		m := kv.FindStringSubmatch(t)
		if m == nil {
			continue
		}
		key := strings.ToLower(m[1])
		val := strings.Trim(m[2], "\"' ")
		out[key] = val
	}
	// EOF without closing fence: treat as no frontmatter.
	return map[string]string{}, 0
}

// --- stories ---

type story struct {
	Title       string
	StartLine   int
	HasPriority bool
	ACCount     int
}

var (
	storyHeadRe = regexp.MustCompile(`(?i)^##+\s+(?:Story\b|User\s+Story\b)[^\n]*`)
	priorityRe  = regexp.MustCompile(`(?i)Priority\s*:`)
	acLineRe    = regexp.MustCompile(`(?i)^\s*AC\s*:\s*$`)
	bulletRe    = regexp.MustCompile(`^\s*(?:[-*+]|\d+\.)\s+\S`)
)

// parseStories scans for "## Story …" or "## User Story …" sections starting
// after fmEndLine (line numbers 1-based). The section ends at the next "##"
// header at the same or shallower depth.
func parseStories(data []byte, fmEndLine int) []story {
	var stories []story
	lines := strings.Split(string(data), "\n")

	// Identify story header indices.
	var heads []int
	for i := fmEndLine; i < len(lines); i++ {
		l := lines[i]
		if storyHeadRe.MatchString(l) {
			heads = append(heads, i)
		}
	}
	heads = append(heads, len(lines)) // sentinel

	for h := 0; h+1 < len(heads); h++ {
		start := heads[h]
		end := heads[h+1]
		// Also stop at the next top-level "## " section, if any earlier.
		for j := start + 1; j < end; j++ {
			if strings.HasPrefix(lines[j], "## ") {
				end = j
				break
			}
		}
		st := story{
			Title:     strings.TrimSpace(strings.TrimLeft(lines[start], "# ")),
			StartLine: start + 1,
		}
		// Look for inline "(Priority: P?)" on the header itself OR a Priority:
		// declaration anywhere in the section body.
		if priorityRe.MatchString(lines[start]) {
			st.HasPriority = true
		}
		// Count AC items: AC: line followed by bullet list, OR an "Acceptance
		// Scenarios" / "Acceptance Criteria" block with numbered/bullet items.
		inAC := false
		for j := start + 1; j < end; j++ {
			l := lines[j]
			if priorityRe.MatchString(l) {
				st.HasPriority = true
			}
			if acLineRe.MatchString(l) {
				inAC = true
				continue
			}
			lt := strings.TrimSpace(l)
			if strings.HasPrefix(lt, "**Acceptance Scenarios") ||
				strings.HasPrefix(lt, "Acceptance Scenarios") ||
				strings.HasPrefix(lt, "**Acceptance Criteria") ||
				strings.HasPrefix(lt, "Acceptance Criteria") {
				inAC = true
				continue
			}
			if inAC {
				if lt == "" {
					// Blank line is allowed inside the AC block; only an empty
					// line followed by a new section header breaks it. Loop
					// continues and we let bulletRe filter.
					continue
				}
				if bulletRe.MatchString(l) {
					st.ACCount++
					continue
				}
				// A non-bullet, non-blank, non-AC line ends the AC block.
				if !acLineRe.MatchString(l) {
					inAC = false
				}
			}
		}
		stories = append(stories, st)
	}
	return stories
}
