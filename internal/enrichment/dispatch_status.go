// dispatch_status + estimated_size (gm-v5z2.1, work-planning.md §4
// Layer 0). The two soft-block fields the WP2.0 selection layer
// reads alongside targets[] + concepts[].

package enrichment

import (
	"fmt"
	"strings"
)

// DispatchStatus is the per-bead soft-block enum the planner's
// selection layer (Layer 5) consults. Orthogonal to bd's own
// `status` field — a bead can be `bd status: open` while
// `dispatch_status: awaiting-design` so it surfaces in `bd list`
// but not in the planner's "what's next" candidate set.
//
// Wire shape: lower-kebab-case strings. Stable — once the
// retrospective starts grading on these values they belong in the
// audit log.
type DispatchStatus string

const (
	// DispatchReady — the only value selection treats as a candidate.
	// Default for fresh beads.
	DispatchReady DispatchStatus = "ready"
	// DispatchAwaitingDesign — design work outstanding; cannot be
	// dispatched until that's resolved (operator clears).
	DispatchAwaitingDesign DispatchStatus = "awaiting-design"
	// DispatchAwaitingVendor — blocked on an external party.
	DispatchAwaitingVendor DispatchStatus = "awaiting-vendor"
	// DispatchAwaitingReview — implementation done, in review;
	// not a dispatch candidate.
	DispatchAwaitingReview DispatchStatus = "awaiting-review"
	// DispatchNotNow — explicitly deferred; visible but not
	// candidate.
	DispatchNotNow DispatchStatus = "not-now"
)

// allDispatchStatuses pins the closed set the validator accepts.
// Ordered so the error message lists them deterministically.
var allDispatchStatuses = []DispatchStatus{
	DispatchReady,
	DispatchAwaitingDesign,
	DispatchAwaitingVendor,
	DispatchAwaitingReview,
	DispatchNotNow,
}

// IsValid reports whether s is a recognised DispatchStatus value.
// The empty string is *not* valid — callers that mean "default to
// ready" should call DispatchStatusOrDefault instead so the
// distinction stays explicit.
func (s DispatchStatus) IsValid() bool {
	for _, v := range allDispatchStatuses {
		if s == v {
			return true
		}
	}
	return false
}

// IsReady is the most common predicate — the selection layer's
// hard filter. Equivalent to s == DispatchReady, but spelled out so
// call sites read like the spec.
func (s DispatchStatus) IsReady() bool { return s == DispatchReady }

// ParseDispatchStatus is the operator-facing parser. Trims +
// lower-cases the input and returns a typed error listing the
// closed set on mismatch. Empty input is rejected — callers that
// want the default supply DispatchReady explicitly.
func ParseDispatchStatus(s string) (DispatchStatus, error) {
	v := DispatchStatus(strings.ToLower(strings.TrimSpace(s)))
	if v == "" {
		return "", fmt.Errorf("enrichment: empty dispatch_status; expected one of %s", joinDispatchStatuses())
	}
	if !v.IsValid() {
		return "", fmt.Errorf("enrichment: %q is not a valid dispatch_status; want one of %s", s, joinDispatchStatuses())
	}
	return v, nil
}

// DispatchStatusOrDefault returns s when valid; falls back to
// DispatchReady on the empty string. Any other invalid value still
// returns the original — callers should validate first via
// ParseDispatchStatus.
func DispatchStatusOrDefault(s DispatchStatus) DispatchStatus {
	if s == "" {
		return DispatchReady
	}
	return s
}

func joinDispatchStatuses() string {
	parts := make([]string, len(allDispatchStatuses))
	for i, v := range allDispatchStatuses {
		parts[i] = string(v)
	}
	return strings.Join(parts, " | ")
}

// EstimatedSize is the per-bead size bucket the selection layer's
// runway gate compares against ctx.runway. Bootstrap from
// description-length × DoD-line-count via EstimateSize; the
// retrospective (§7.6) sharpens the bucket boundaries over time.
type EstimatedSize string

const (
	SizeSmall  EstimatedSize = "small"
	SizeMedium EstimatedSize = "medium"
	SizeLarge  EstimatedSize = "large"
)

var allEstimatedSizes = []EstimatedSize{SizeSmall, SizeMedium, SizeLarge}

// IsValid reports whether s is a recognised EstimatedSize value.
// Empty string is *not* valid.
func (s EstimatedSize) IsValid() bool {
	for _, v := range allEstimatedSizes {
		if s == v {
			return true
		}
	}
	return false
}

// Rank maps the bucket to a comparable integer (small=1, medium=2,
// large=3). Selection's runway gate compares ranks rather than
// strings so future buckets (xl, xxl) can slot in without
// rewriting predicates.
func (s EstimatedSize) Rank() int {
	switch s {
	case SizeSmall:
		return 1
	case SizeMedium:
		return 2
	case SizeLarge:
		return 3
	default:
		return 0
	}
}

// ParseEstimatedSize is the operator-facing parser. Empty input is
// rejected — callers that want the default supply SizeMedium
// explicitly.
func ParseEstimatedSize(s string) (EstimatedSize, error) {
	v := EstimatedSize(strings.ToLower(strings.TrimSpace(s)))
	if v == "" {
		return "", fmt.Errorf("enrichment: empty estimated_size; expected one of %s", joinEstimatedSizes())
	}
	if !v.IsValid() {
		return "", fmt.Errorf("enrichment: %q is not a valid estimated_size; want one of %s", s, joinEstimatedSizes())
	}
	return v, nil
}

func joinEstimatedSizes() string {
	parts := make([]string, len(allEstimatedSizes))
	for i, v := range allEstimatedSizes {
		parts[i] = string(v)
	}
	return strings.Join(parts, " | ")
}

// SizeHeuristicThresholds parametrises EstimateSize. The default
// values match the spec note: a bead's size = description length
// (chars) × number of acceptance / DoD lines. Below SmallMax → small,
// below MediumMax → medium, otherwise → large. Operators tune via
// rig settings once the calibration loop in §7.6 lands.
type SizeHeuristicThresholds struct {
	SmallMax  int
	MediumMax int
}

// DefaultSizeThresholds is the v1 calibration. Numbers chosen so a
// typical 200-char body with 1 DoD line lands as "small", and a
// 1500-char body with 4 DoD lines lands as "large". The
// retrospective sharpens these per-rig over time.
var DefaultSizeThresholds = SizeHeuristicThresholds{
	SmallMax:  500,
	MediumMax: 4000,
}

// EstimateSize applies the spec heuristic: descLen × dodLines.
// A bead with no DoD lines still gets a size from descLen alone
// (treated as 1 DoD line so the multiplier is non-zero).
//
// The thresholds parameter is optional — pass nil for
// DefaultSizeThresholds. Empty body → SizeSmall (no signal to
// suggest anything else).
func EstimateSize(body string, thresholds *SizeHeuristicThresholds) EstimatedSize {
	t := DefaultSizeThresholds
	if thresholds != nil {
		t = *thresholds
	}
	descLen := len(strings.TrimSpace(body))
	if descLen == 0 {
		return SizeSmall
	}
	dod := countDoDLines(body)
	if dod == 0 {
		dod = 1
	}
	score := descLen * dod
	switch {
	case score < t.SmallMax:
		return SizeSmall
	case score < t.MediumMax:
		return SizeMedium
	default:
		return SizeLarge
	}
}

// countDoDLines counts lines that look like acceptance criteria or
// definition-of-done bullets. Heuristic: any line whose trimmed
// prefix is a checkbox marker ("- [ ]", "* [ ]", "- [x]"), a
// numbered DoD ("1.", "2)"), or a bullet under a "DoD" / "Done
// when" / "Acceptance" heading.
//
// Pure: no I/O, no allocations beyond split. Cheap to call from
// the bead-create hot path.
func countDoDLines(body string) int {
	if body == "" {
		return 0
	}
	lines := strings.Split(body, "\n")
	inDoDSection := false
	count := 0
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		// Section toggling — common headings flip us into "every
		// bullet here is a DoD line" mode until the next blank or
		// new heading.
		lower := strings.ToLower(line)
		if isDoDHeading(lower) {
			inDoDSection = true
			continue
		}
		if strings.HasPrefix(line, "#") || strings.HasPrefix(line, "## ") {
			inDoDSection = false
			continue
		}

		if isCheckboxLine(line) {
			count++
			continue
		}
		if inDoDSection && isBulletLine(line) {
			count++
		}
	}
	return count
}

func isDoDHeading(lowerLine string) bool {
	if !strings.HasPrefix(lowerLine, "#") && !strings.HasPrefix(lowerLine, "**") &&
		!strings.HasSuffix(lowerLine, ":") {
		return false
	}
	stripped := strings.TrimLeft(lowerLine, "# *")
	stripped = strings.TrimRight(stripped, "* :")
	switch stripped {
	case "dod", "definition of done", "done when", "acceptance", "acceptance criteria",
		"criteria", "exit criteria":
		return true
	}
	return false
}

func isCheckboxLine(line string) bool {
	// Common shapes:
	//   - [ ] foo
	//   - [x] bar
	//   * [ ] baz
	if len(line) < 5 {
		return false
	}
	bullet := line[0]
	if bullet != '-' && bullet != '*' {
		return false
	}
	rest := strings.TrimSpace(line[1:])
	return strings.HasPrefix(rest, "[ ]") || strings.HasPrefix(rest, "[x]") ||
		strings.HasPrefix(rest, "[X]")
}

func isBulletLine(line string) bool {
	if line == "" {
		return false
	}
	switch line[0] {
	case '-', '*':
		return len(line) > 1 && line[1] == ' '
	}
	// Numbered list — "1." or "1)".
	if line[0] >= '0' && line[0] <= '9' {
		for i := 1; i < len(line); i++ {
			if line[i] >= '0' && line[i] <= '9' {
				continue
			}
			if line[i] == '.' || line[i] == ')' {
				return i+1 < len(line) && line[i+1] == ' '
			}
			return false
		}
	}
	return false
}
