package parser

import (
	"bufio"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Errors returned by Parse.
var (
	ErrMissingFrontmatter = errors.New("spec: missing YAML frontmatter (--- ... ---)")
	ErrDuplicateAnchor    = errors.New("spec: duplicate anchor")
	ErrMalformedAC        = errors.New("spec: malformed AC list")
	ErrMissingNonce       = errors.New("spec: frontmatter missing nonce")
	ErrMissingSpecID      = errors.New("spec: frontmatter missing spec_id")
)

// anchorRe matches an H3 heading whose title begins with a stable handle:
//   ### M-01 Title
//   ### E-02 Title
//   ### S-13 Title
var anchorRe = regexp.MustCompile(`^###\s+([MES])-(\d{2,})\s+(.+?)\s*$`)

// inlineFieldRe captures `Status:`, `Parent:`, `Priority:` lines.
var inlineFieldRe = regexp.MustCompile(`^(Status|Parent|Priority):\s*(.+?)\s*$`)

// acItemRe captures an AC bullet: `- AC: foo` or `* AC: foo`.
var acItemRe = regexp.MustCompile(`^[-*]\s+AC:\s*(.+?)\s*$`)

// acHeaderRe captures the start of an AC block: `AC:` on its own line.
var acHeaderRe = regexp.MustCompile(`^AC:\s*$`)

// Parse parses a spec.md document.
func Parse(src []byte) (*Spec, error) {
	fm, body, err := splitFrontmatter(src)
	if err != nil {
		return nil, err
	}
	spec := &Spec{Frontmatter: fm}

	if err := parseBody(body, spec); err != nil {
		return nil, err
	}
	return spec, nil
}

// splitFrontmatter peels the leading `--- ... ---` YAML block.
func splitFrontmatter(src []byte) (Frontmatter, string, error) {
	var fm Frontmatter
	s := string(src)
	// Tolerate a UTF-8 BOM and leading whitespace before the opening fence.
	s = strings.TrimPrefix(s, "\uFEFF")
	trimmed := strings.TrimLeft(s, " \t\r\n")
	if !strings.HasPrefix(trimmed, "---") {
		return fm, "", ErrMissingFrontmatter
	}
	rest := strings.TrimPrefix(trimmed, "---")
	// The opening fence may be followed by either \n or \r\n.
	rest = strings.TrimLeft(rest, "\r")
	if !strings.HasPrefix(rest, "\n") {
		return fm, "", ErrMissingFrontmatter
	}
	rest = rest[1:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return fm, "", ErrMissingFrontmatter
	}
	yamlBlock := rest[:end]
	body := rest[end+len("\n---"):]
	// Drop the newline immediately after the closing fence (if any).
	body = strings.TrimPrefix(body, "\r")
	body = strings.TrimPrefix(body, "\n")

	if err := yaml.Unmarshal([]byte(yamlBlock), &fm); err != nil {
		return fm, "", fmt.Errorf("spec: frontmatter yaml: %w", err)
	}
	if strings.TrimSpace(fm.Nonce) == "" {
		return fm, "", ErrMissingNonce
	}
	if strings.TrimSpace(fm.SpecID) == "" {
		return fm, "", ErrMissingSpecID
	}
	return fm, body, nil
}

// section is a slice of body lines under one H2 heading.
type section struct {
	name  string // "Goals", "Decisions", "Milestones", "Epics", "Stories"
	lines []string
}

func parseBody(body string, spec *Spec) error {
	sections := splitH2Sections(body)
	seenAnchors := map[string]struct{}{}

	for _, sec := range sections {
		switch sec.name {
		case "Goals":
			spec.Goals = strings.TrimSpace(strings.Join(sec.lines, "\n"))
		case "Decisions":
			spec.Decisions = strings.TrimSpace(strings.Join(sec.lines, "\n"))
		case "Milestones":
			ms, err := parseMilestones(sec.lines, seenAnchors)
			if err != nil {
				return err
			}
			spec.Milestones = ms
		case "Epics":
			es, err := parseEpics(sec.lines, seenAnchors)
			if err != nil {
				return err
			}
			spec.Epics = es
		case "Stories":
			ss, err := parseStories(sec.lines, seenAnchors)
			if err != nil {
				return err
			}
			spec.Stories = ss
		}
	}
	return nil
}

// splitH2Sections groups the body by `## Heading` lines.
func splitH2Sections(body string) []section {
	var (
		sections []section
		cur      *section
	)
	sc := bufio.NewScanner(strings.NewReader(body))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "## ") && !strings.HasPrefix(line, "### ") {
			name := strings.TrimSpace(strings.TrimPrefix(line, "##"))
			sections = append(sections, section{name: name})
			cur = &sections[len(sections)-1]
			continue
		}
		if cur != nil {
			cur.lines = append(cur.lines, line)
		}
	}
	return sections
}

// splitH3Blocks groups lines by `### <kind>-NN Title` heads. Lines before
// the first H3 become the leading body of the section.
func splitH3Blocks(lines []string) (leading []string, blocks []h3Block) {
	var cur *h3Block
	for _, l := range lines {
		if m := anchorRe.FindStringSubmatch(l); m != nil {
			blocks = append(blocks, h3Block{kind: m[1], num: m[2], title: m[3]})
			cur = &blocks[len(blocks)-1]
			continue
		}
		if cur == nil {
			leading = append(leading, l)
		} else {
			cur.lines = append(cur.lines, l)
		}
	}
	return leading, blocks
}

type h3Block struct {
	kind  string // M, E, S
	num   string
	title string
	lines []string
}

func (b h3Block) anchor() string { return b.kind + "-" + b.num }

func parseMilestones(lines []string, seen map[string]struct{}) ([]Milestone, error) {
	_, blocks := splitH3Blocks(lines)
	out := make([]Milestone, 0, len(blocks))
	for _, b := range blocks {
		if b.kind != "M" {
			continue
		}
		if err := claimAnchor(seen, b.anchor()); err != nil {
			return nil, err
		}
		out = append(out, Milestone{
			Anchor: b.anchor(),
			Title:  b.title,
			Body:   strings.TrimSpace(strings.Join(b.lines, "\n")),
		})
	}
	return out, nil
}

func parseEpics(lines []string, seen map[string]struct{}) ([]Epic, error) {
	_, blocks := splitH3Blocks(lines)
	out := make([]Epic, 0, len(blocks))
	for _, b := range blocks {
		if b.kind != "E" {
			continue
		}
		if err := claimAnchor(seen, b.anchor()); err != nil {
			return nil, err
		}
		out = append(out, Epic{
			Anchor: b.anchor(),
			Title:  b.title,
			Body:   strings.TrimSpace(strings.Join(b.lines, "\n")),
		})
	}
	return out, nil
}

func parseStories(lines []string, seen map[string]struct{}) ([]Story, error) {
	_, blocks := splitH3Blocks(lines)
	out := make([]Story, 0, len(blocks))
	for _, b := range blocks {
		if b.kind != "S" {
			continue
		}
		if err := claimAnchor(seen, b.anchor()); err != nil {
			return nil, err
		}
		st, err := buildStory(b)
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, nil
}

func buildStory(b h3Block) (Story, error) {
	st := Story{Anchor: b.anchor(), Title: b.title}
	var bodyLines []string
	inACBlock := false
	for _, l := range b.lines {
		trim := strings.TrimSpace(l)
		if inACBlock {
			// AC block ends on first non-list, non-blank line.
			if trim == "" {
				bodyLines = append(bodyLines, l)
				continue
			}
			// A bare `-` or `*` is malformed (empty list item).
			if trim == "-" || trim == "*" {
				return st, fmt.Errorf("%w: anchor=%s empty AC item", ErrMalformedAC, b.anchor())
			}
			if strings.HasPrefix(trim, "- ") || strings.HasPrefix(trim, "* ") {
				item := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(trim, "- "), "* "))
				if item == "" {
					return st, fmt.Errorf("%w: anchor=%s empty AC item", ErrMalformedAC, b.anchor())
				}
				st.AC = append(st.AC, item)
				continue
			}
			inACBlock = false
			// fall through to normal handling
		}
		if m := inlineFieldRe.FindStringSubmatch(trim); m != nil {
			switch m[1] {
			case "Status":
				st.Status = m[2]
			case "Parent":
				st.Parent = m[2]
			case "Priority":
				st.Priority = m[2]
			}
			continue
		}
		// Inline AC: `- AC: foo`
		if m := acItemRe.FindStringSubmatch(trim); m != nil {
			st.AC = append(st.AC, m[1])
			continue
		}
		// AC block header: `AC:` on its own line.
		if acHeaderRe.MatchString(trim) {
			inACBlock = true
			continue
		}
		bodyLines = append(bodyLines, l)
	}
	st.Body = strings.TrimSpace(strings.Join(bodyLines, "\n"))
	if inACBlock && len(st.AC) == 0 {
		return st, fmt.Errorf("%w: anchor=%s AC block has no items", ErrMalformedAC, b.anchor())
	}
	return st, nil
}

func claimAnchor(seen map[string]struct{}, a string) error {
	if _, ok := seen[a]; ok {
		return fmt.Errorf("%w: %s", ErrDuplicateAnchor, a)
	}
	seen[a] = struct{}{}
	return nil
}
