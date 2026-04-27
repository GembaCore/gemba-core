package evidence

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/MikeBengtson/gemba/internal/core"
)

// CICollector synthesizes [core.EvidenceTestResult] records from
// GitHub Actions workflow runs. It searches for runs whose head
// branch or commit message references the bead's native id by
// invoking `gh run list --json ...` with branch / search filters.
//
// The implementation is GitHub-Actions-specific (matches the
// "GitHub" CI provider DD-13 calls out as the minimum); other
// providers can plug in via additional Collectors.
type CICollector struct {
	Runner Runner
	// Limit caps the number of runs returned (default 25).
	Limit int
}

// NewCICollector returns a collector with the default runner.
func NewCICollector() *CICollector {
	return &CICollector{Runner: DefaultRunner(), Limit: 25}
}

// Source implements [Collector].
func (c *CICollector) Source() Source { return SourceCI }

// ghRunRecord mirrors `gh run list --json ...`.
type ghRunRecord struct {
	DatabaseID   int64     `json:"databaseId"`
	Name         string    `json:"name"`
	DisplayTitle string    `json:"displayTitle"`
	Conclusion   string    `json:"conclusion"`
	Status       string    `json:"status"`
	URL          string    `json:"url"`
	WorkflowName string    `json:"workflowName"`
	HeadBranch   string    `json:"headBranch"`
	HeadSHA      string    `json:"headSha"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
	Event        string    `json:"event"`
}

// Collect implements [Collector].
func (c *CICollector) Collect(ctx context.Context, t Target) ([]core.Evidence, error) {
	if t.NativeID == "" || t.GitHubRepo == "" {
		return nil, nil
	}
	limit := c.Limit
	if limit <= 0 {
		limit = 25
	}
	runner := c.Runner
	if runner == nil {
		runner = DefaultRunner()
	}
	args := []string{
		"run", "list",
		"--repo", t.GitHubRepo,
		"--limit", fmt.Sprintf("%d", limit),
		"--json", "databaseId,name,displayTitle,conclusion,status,url,workflowName,headBranch,headSha,createdAt,updatedAt,event",
	}
	out, err := runner(ctx, "gh", args...)
	if err != nil {
		return nil, fmt.Errorf("gh run list: %w", err)
	}
	return parseGHRunList(out, t)
}

func parseGHRunList(out []byte, t Target) ([]core.Evidence, error) {
	if len(out) == 0 {
		return nil, nil
	}
	var runs []ghRunRecord
	if err := json.Unmarshal(out, &runs); err != nil {
		return nil, fmt.Errorf("decode gh run json: %w", err)
	}
	var ev []core.Evidence
	for _, r := range runs {
		// Filter to runs whose displayTitle or headBranch references
		// the bead. We do this client-side because `gh run list`
		// doesn't accept a content search; restricting --branch
		// would require knowing the branch name a priori.
		if !mentionsNativeID(r.DisplayTitle, t.NativeID) &&
			!mentionsNativeID(r.HeadBranch, t.NativeID) &&
			!mentionsNativeID(r.Name, t.NativeID) {
			continue
		}
		captured := r.UpdatedAt
		if captured.IsZero() {
			captured = r.CreatedAt
		}
		summary := fmt.Sprintf("%s: %s [%s/%s]", r.WorkflowName, r.DisplayTitle, r.Status, r.Conclusion)
		ev = append(ev, core.Evidence{
			ID:         fmt.Sprintf("ghrun:%s#%d", t.GitHubRepo, r.DatabaseID),
			Kind:       core.EvidenceTestResult,
			Source:     "evidence:" + string(SourceCI),
			Ref:        r.URL,
			Summary:    summary,
			CapturedAt: captured,
			Payload: markSynthesized(map[string]any{
				"run_id":        r.DatabaseID,
				"workflow":      r.WorkflowName,
				"display_title": r.DisplayTitle,
				"status":        r.Status,
				"conclusion":    r.Conclusion,
				"url":           r.URL,
				"head_branch":   r.HeadBranch,
				"head_sha":      r.HeadSHA,
				"event":         r.Event,
				"repo":          t.GitHubRepo,
				"native_id":     t.NativeID,
				"work_item":     string(t.ID),
			}, SourceCI),
		})
	}
	return ev, nil
}

// mentionsNativeID is a deliberately simple substring check so it
// matches both bare ids ("gm-e11.6") and embedded ones
// ("feat/gm-e11.6-evidence").
func mentionsNativeID(haystack, id string) bool {
	if id == "" || haystack == "" {
		return false
	}
	// fast path: case-insensitive contains.
	return containsFold(haystack, id)
}

// containsFold is strings.Contains with case-folding without pulling
// in unicode tables. Native ids are ASCII, so byte-wise lower works.
func containsFold(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	if len(sub) > len(s) {
		return false
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		match := true
		for j := 0; j < len(sub); j++ {
			a := s[i+j]
			b := sub[j]
			if a >= 'A' && a <= 'Z' {
				a += 'a' - 'A'
			}
			if b >= 'A' && b <= 'Z' {
				b += 'a' - 'A'
			}
			if a != b {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
