// Package promote provides decision-promotion plumbing for ASDD: bd mail
// threads tagged spec-decision:<slug> can be surfaced to a human as a
// PR-style unified diff that appends a new entry under a spec's
// `## Decisions` section. Git remains the journal — we never mutate the
// spec file in place.
package promote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/GembaCore/gemba-core/internal/spec/parser"
)

// Candidate is a single bd-mail message that may be promoted to a spec
// decision. Fields are populated from `bd mail list ... --json`.
type Candidate struct {
	MailID    string `json:"mail_id"`
	SpecSlug  string `json:"spec_slug"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	Author    string `json:"author"`
	Timestamp string `json:"timestamp"`
}

// MailLister is the interface used by Discover to fetch candidate mails.
// In production this shells out to `bd mail list`; tests inject a fake.
type MailLister interface {
	List(ctx context.Context, label string) ([]Candidate, error)
}

// BDMailLister shells out to `bd mail list --label <label> --json`.
type BDMailLister struct {
	// Bin is the path to the bd binary; defaults to "bd" when empty.
	Bin string
}

// rawBDMail is a permissive shape for the bd mail json output. We only
// care about a handful of fields; everything else is ignored.
type rawBDMail struct {
	ID        string `json:"id"`
	MailID    string `json:"mail_id"`
	Subject   string `json:"subject"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	Author    string `json:"author"`
	From      string `json:"from"`
	Timestamp string `json:"timestamp"`
	CreatedAt string `json:"created_at"`
	Status    string `json:"status"`
	Labels    []string `json:"labels"`
}

// List runs `bd mail list --label <label> --json` and parses the result.
func (b BDMailLister) List(ctx context.Context, label string) ([]Candidate, error) {
	bin := b.Bin
	if bin == "" {
		bin = "bd"
	}
	cmd := exec.CommandContext(ctx, bin, "mail", "list", "--label", label, "--json")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("promote: bd mail list: %w", err)
	}
	var raw []rawBDMail
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("promote: parse bd mail json: %w", err)
	}
	slug := strings.TrimPrefix(label, "spec-decision:")
	cands := make([]Candidate, 0, len(raw))
	for _, m := range raw {
		// Status filter: only open or accepted.
		if m.Status != "" && m.Status != "open" && m.Status != "accepted" {
			continue
		}
		c := Candidate{
			MailID:    firstNonEmpty(m.MailID, m.ID),
			SpecSlug:  slug,
			Title:     firstNonEmpty(m.Subject, m.Title),
			Body:      m.Body,
			Author:    firstNonEmpty(m.Author, m.From),
			Timestamp: firstNonEmpty(m.Timestamp, m.CreatedAt),
		}
		cands = append(cands, c)
	}
	return cands, nil
}

// Discover lists candidates for a slug. It guarantees the label filter is
// exact: only mails tagged `spec-decision:<slug>` are returned.
func Discover(ctx context.Context, slug string, lister MailLister) ([]Candidate, error) {
	if slug == "" {
		return nil, errors.New("promote: empty slug")
	}
	if lister == nil {
		lister = BDMailLister{}
	}
	label := "spec-decision:" + slug
	cands, err := lister.List(ctx, label)
	if err != nil {
		return nil, err
	}
	// Defensive: drop anything whose slug doesn't match exactly.
	out := cands[:0]
	for _, c := range cands {
		if c.SpecSlug == "" || c.SpecSlug == slug {
			c.SpecSlug = slug
			out = append(out, c)
		}
	}
	return out, nil
}

// decisionAnchorRe matches `### D-NN <title>` H3 entries under `## Decisions`.
var decisionAnchorRe = regexp.MustCompile(`^###\s+D-(\d{2,})\b`)

// NextDecisionNumber returns max(existing D-NN) + 1, zero-padded to 2 digits.
// Returns "01" if no D-NN entries exist yet.
func NextDecisionNumber(decisionsSection string) string {
	max := 0
	for _, line := range strings.Split(decisionsSection, "\n") {
		if m := decisionAnchorRe.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			n, err := strconv.Atoi(m[1])
			if err == nil && n > max {
				max = n
			}
		}
	}
	return fmt.Sprintf("%02d", max+1)
}

// FormatEntry renders the canonical markdown for a new decision entry.
//
//	### D-NN <title>
//	Author: <author>
//	Date: <yyyy-mm-dd>
//
//	<body>
func FormatEntry(num, title, author, ts, body string) string {
	date := ts
	if t, err := time.Parse(time.RFC3339, ts); err == nil {
		date = t.UTC().Format("2006-01-02")
	} else if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
		date = t.UTC().Format("2006-01-02")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "### D-%s %s\n", num, strings.TrimSpace(title))
	if author != "" {
		fmt.Fprintf(&b, "Author: %s\n", author)
	}
	if date != "" {
		fmt.Fprintf(&b, "Date: %s\n", date)
	}
	b.WriteString("\n")
	b.WriteString(strings.TrimRight(body, "\n"))
	b.WriteString("\n")
	return b.String()
}

// Propose builds a unified diff that, when applied to the spec's source
// file, appends a new `### D-NN` entry under `## Decisions`. The diff is
// relative to a virtual path `a/<specRelPath>` / `b/<specRelPath>` which
// the caller (CLI) substitutes with the real path. We do not write any
// files here.
//
// The returned diff is a minimal unified diff (one hunk) suitable for
// `git apply` / `patch -p1`.
func Propose(specPath string, specSrc []byte, spec *parser.Spec, c Candidate) (string, error) {
	if spec == nil {
		return "", errors.New("promote: nil spec")
	}
	num := NextDecisionNumber(spec.Decisions)
	entry := FormatEntry(num, c.Title, c.Author, c.Timestamp, c.Body)

	orig := string(specSrc)
	updated, err := appendToDecisions(orig, entry)
	if err != nil {
		return "", err
	}
	return unifiedDiff(specPath, orig, updated), nil
}

// appendToDecisions returns a new copy of src with `entry` appended just
// before the next `## ` heading after `## Decisions`, or at the end of
// the document if `## Decisions` is the last section. If `## Decisions`
// is missing, a new section is appended at the end.
func appendToDecisions(src, entry string) (string, error) {
	lines := splitKeepNL(src)
	decIdx := -1
	for i, l := range lines {
		if strings.TrimRight(l, "\r\n") == "## Decisions" {
			decIdx = i
			break
		}
	}
	if decIdx < 0 {
		// Append a fresh section. Ensure trailing newline.
		out := src
		if !strings.HasSuffix(out, "\n") {
			out += "\n"
		}
		if !strings.HasSuffix(out, "\n\n") {
			out += "\n"
		}
		out += "## Decisions\n\n" + entry
		return out, nil
	}
	// Find the next H2 after decIdx.
	nextH2 := len(lines)
	for i := decIdx + 1; i < len(lines); i++ {
		t := strings.TrimRight(lines[i], "\r\n")
		if strings.HasPrefix(t, "## ") && !strings.HasPrefix(t, "### ") {
			nextH2 = i
			break
		}
	}
	// Insert entry just before nextH2. Walk back to skip trailing blank
	// lines so the entry sits flush against existing decisions content.
	insertAt := nextH2
	for insertAt-1 > decIdx {
		t := strings.TrimSpace(lines[insertAt-1])
		if t != "" {
			break
		}
		insertAt--
	}
	// Build output.
	var b strings.Builder
	for i := 0; i < insertAt; i++ {
		b.WriteString(lines[i])
	}
	// Ensure exactly one blank line between previous content and new entry.
	tail := b.String()
	if !strings.HasSuffix(tail, "\n") {
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(entry)
	// Re-emit any trailing blank lines we skipped, then the remainder.
	if insertAt < nextH2 {
		// Preserve a single blank line before the next H2 if there was one.
		b.WriteString("\n")
	}
	for i := nextH2; i < len(lines); i++ {
		b.WriteString(lines[i])
	}
	return b.String(), nil
}

// splitKeepNL splits s into lines, keeping each line's trailing newline.
func splitKeepNL(s string) []string {
	var out []string
	for len(s) > 0 {
		idx := strings.IndexByte(s, '\n')
		if idx < 0 {
			out = append(out, s)
			break
		}
		out = append(out, s[:idx+1])
		s = s[idx+1:]
	}
	return out
}

// unifiedDiff produces a minimal single-hunk unified diff between a and b.
// It is intentionally simplistic: it emits the entire file as context plus
// the change, which is sufficient because spec files are small. For larger
// files this would be wasteful, but correctness is more important here than
// minimality — and `git apply` accepts it either way.
func unifiedDiff(path, a, b string) string {
	aLines := splitLines(a)
	bLines := splitLines(b)
	// Find common prefix.
	pre := 0
	for pre < len(aLines) && pre < len(bLines) && aLines[pre] == bLines[pre] {
		pre++
	}
	// Find common suffix.
	suf := 0
	for suf < len(aLines)-pre && suf < len(bLines)-pre &&
		aLines[len(aLines)-1-suf] == bLines[len(bLines)-1-suf] {
		suf++
	}
	// Add a couple of context lines around the change.
	ctxN := 3
	startA := pre - ctxN
	if startA < 0 {
		startA = 0
	}
	startB := pre - ctxN
	if startB < 0 {
		startB = 0
	}
	endA := len(aLines) - suf + ctxN
	if endA > len(aLines) {
		endA = len(aLines)
	}
	endB := len(bLines) - suf + ctxN
	if endB > len(bLines) {
		endB = len(bLines)
	}

	var out strings.Builder
	fmt.Fprintf(&out, "--- a/%s\n", path)
	fmt.Fprintf(&out, "+++ b/%s\n", path)
	fmt.Fprintf(&out, "@@ -%d,%d +%d,%d @@\n",
		startA+1, endA-startA, startB+1, endB-startB)
	// Leading context.
	for i := startA; i < pre; i++ {
		out.WriteString(" " + aLines[i] + "\n")
	}
	// Removed lines.
	for i := pre; i < len(aLines)-suf; i++ {
		out.WriteString("-" + aLines[i] + "\n")
	}
	// Added lines.
	for i := pre; i < len(bLines)-suf; i++ {
		out.WriteString("+" + bLines[i] + "\n")
	}
	// Trailing context.
	for i := len(aLines) - suf; i < endA; i++ {
		out.WriteString(" " + aLines[i] + "\n")
	}
	return out.String()
}

// splitLines splits s on '\n' but does NOT keep the trailing newline on
// each line, and does not produce a trailing empty element for a file
// ending in '\n'. This matches the per-line semantics used by unified
// diffs.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	end := len(s)
	trailingNL := false
	if s[end-1] == '\n' {
		trailingNL = true
		end--
	}
	parts := strings.Split(s[:end], "\n")
	if trailingNL {
		// no-op: we intentionally drop the empty trailing line.
		_ = trailingNL
	}
	return parts
}

// ApplyDiff is a test helper that simulates `git apply` for the simple
// single-hunk diffs Propose produces. It is intentionally not exported
// for production use — operators apply diffs via `git apply` themselves.
//
//nolint:unused
func applyDiffForTest(orig, diff string) (string, error) {
	// Parse a single hunk and reconstruct the file. Tolerant of CRLF.
	lines := strings.Split(diff, "\n")
	// Find @@ line.
	hunkIdx := -1
	for i, l := range lines {
		if strings.HasPrefix(l, "@@") {
			hunkIdx = i
			break
		}
	}
	if hunkIdx < 0 {
		return "", errors.New("applyDiff: no hunk")
	}
	var oldStart, oldLen int
	_, err := fmt.Sscanf(lines[hunkIdx], "@@ -%d,%d", &oldStart, &oldLen)
	if err != nil {
		return "", fmt.Errorf("applyDiff: bad hunk header: %w", err)
	}

	origLines := splitLines(orig)
	// Walk through the hunk, emitting the new file.
	var out []string
	out = append(out, origLines[:oldStart-1]...)
	cursor := oldStart - 1
	for i := hunkIdx + 1; i < len(lines); i++ {
		l := lines[i]
		if l == "" && i == len(lines)-1 {
			break
		}
		switch {
		case strings.HasPrefix(l, " "):
			if cursor >= len(origLines) {
				return "", errors.New("applyDiff: context past EOF")
			}
			if origLines[cursor] != l[1:] {
				return "", fmt.Errorf("applyDiff: context mismatch at %d: %q vs %q", cursor, origLines[cursor], l[1:])
			}
			out = append(out, origLines[cursor])
			cursor++
		case strings.HasPrefix(l, "-"):
			if cursor >= len(origLines) || origLines[cursor] != l[1:] {
				return "", fmt.Errorf("applyDiff: remove mismatch at %d", cursor)
			}
			cursor++
		case strings.HasPrefix(l, "+"):
			out = append(out, l[1:])
		}
	}
	// Tail.
	if cursor < len(origLines) {
		out = append(out, origLines[cursor:]...)
	}
	res := strings.Join(out, "\n")
	if strings.HasSuffix(orig, "\n") {
		res += "\n"
	}
	return res, nil
}

// SortCandidates returns candidates in stable timestamp-then-mailid order.
func SortCandidates(cs []Candidate) {
	sort.SliceStable(cs, func(i, j int) bool {
		if cs[i].Timestamp != cs[j].Timestamp {
			return cs[i].Timestamp < cs[j].Timestamp
		}
		return cs[i].MailID < cs[j].MailID
	})
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
