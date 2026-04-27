package evidence

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/MikeBengtson/gemba/internal/core"
)

// GitHubPRCollector synthesizes [core.EvidenceURL] records from the
// GitHub PR API. The collector shells out to `gh pr list` with a
// search query that matches the bead's native id in title or body.
type GitHubPRCollector struct {
	Runner Runner
	// Limit caps the number of PRs returned (default 25).
	Limit int
}

// NewGitHubPRCollector returns a collector with the default runner.
func NewGitHubPRCollector() *GitHubPRCollector {
	return &GitHubPRCollector{Runner: DefaultRunner(), Limit: 25}
}

// Source implements [Collector].
func (c *GitHubPRCollector) Source() Source { return SourceGitHubPR }

// ghPRRecord mirrors the JSON shape of `gh pr list --json ...`.
type ghPRRecord struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	URL       string    `json:"url"`
	State     string    `json:"state"`
	Author    ghAuthor  `json:"author"`
	HeadRef   string    `json:"headRefName"`
	BaseRef   string    `json:"baseRefName"`
	UpdatedAt time.Time `json:"updatedAt"`
	CreatedAt time.Time `json:"createdAt"`
	IsDraft   bool      `json:"isDraft"`
}

type ghAuthor struct {
	Login string `json:"login"`
}

// Collect implements [Collector].
func (c *GitHubPRCollector) Collect(ctx context.Context, t Target) ([]core.Evidence, error) {
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
		"pr", "list",
		"--repo", t.GitHubRepo,
		"--state", "all",
		"--search", t.NativeID + " in:title in:body",
		"--limit", fmt.Sprintf("%d", limit),
		"--json", "number,title,url,state,author,headRefName,baseRefName,updatedAt,createdAt,isDraft",
	}
	out, err := runner(ctx, "gh", args...)
	if err != nil {
		return nil, fmt.Errorf("gh pr list: %w", err)
	}
	return parseGHPRList(out, t)
}

func parseGHPRList(out []byte, t Target) ([]core.Evidence, error) {
	if len(out) == 0 {
		return nil, nil
	}
	var prs []ghPRRecord
	if err := json.Unmarshal(out, &prs); err != nil {
		return nil, fmt.Errorf("decode gh json: %w", err)
	}
	ev := make([]core.Evidence, 0, len(prs))
	for _, pr := range prs {
		captured := pr.UpdatedAt
		if captured.IsZero() {
			captured = pr.CreatedAt
		}
		ev = append(ev, core.Evidence{
			ID:         fmt.Sprintf("ghpr:%s#%d", t.GitHubRepo, pr.Number),
			Kind:       core.EvidenceURL,
			Source:     "evidence:" + string(SourceGitHubPR),
			Ref:        pr.URL,
			Summary:    fmt.Sprintf("PR #%d: %s [%s]", pr.Number, pr.Title, pr.State),
			CapturedAt: captured,
			Payload: markSynthesized(map[string]any{
				"number":    pr.Number,
				"title":     pr.Title,
				"url":       pr.URL,
				"state":     pr.State,
				"author":    pr.Author.Login,
				"head_ref":  pr.HeadRef,
				"base_ref":  pr.BaseRef,
				"is_draft":  pr.IsDraft,
				"repo":      t.GitHubRepo,
				"native_id": t.NativeID,
				"work_item": string(t.ID),
			}, SourceGitHubPR),
		})
	}
	return ev, nil
}
