package evidence

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/GembaCore/gemba-core/core"
)

// GitLogCollector synthesizes [core.EvidenceCommit] records by
// scanning `git log` for commit messages that mention the bead's
// native id. The id matches anywhere in the message — subject line,
// body, or trailers — so commits using `Refs: gm-e11.6` and
// `feat(evidence): ... (gm-e11.6)` both match.
type GitLogCollector struct {
	Runner Runner
	// Limit caps the number of commits scanned (default 100).
	Limit int
}

// NewGitLogCollector returns a collector with the default runner.
func NewGitLogCollector() *GitLogCollector {
	return &GitLogCollector{Runner: DefaultRunner(), Limit: 100}
}

// Source implements [Collector].
func (c *GitLogCollector) Source() Source { return SourceGitLog }

// Collect implements [Collector].
func (c *GitLogCollector) Collect(ctx context.Context, t Target) ([]core.Evidence, error) {
	if t.NativeID == "" {
		return nil, nil
	}
	if t.RepoPath == "" {
		// No repo to scan; not an error — caller deliberately
		// omitted git context.
		return nil, nil
	}
	limit := c.Limit
	if limit <= 0 {
		limit = 100
	}
	runner := c.Runner
	if runner == nil {
		runner = DefaultRunner()
	}
	runner = runnerInDir(runner, t.RepoPath)

	// %H<TAB>%cI<TAB>%s — sha, committer ISO date, subject. We
	// pipe through --grep so git filters server-side and we don't
	// haul the entire history.
	args := []string{
		"log",
		fmt.Sprintf("--grep=%s", t.NativeID),
		"--regexp-ignore-case",
		fmt.Sprintf("-n%d", limit),
		"--pretty=format:%H%x09%cI%x09%s",
	}
	out, err := runner(ctx, "git", args...)
	if err != nil {
		return nil, fmt.Errorf("git log: %w", err)
	}
	return parseGitLog(bytes.TrimSpace(out), t)
}

func parseGitLog(out []byte, t Target) ([]core.Evidence, error) {
	if len(out) == 0 {
		return nil, nil
	}
	var ev []core.Evidence
	scanner := bufio.NewScanner(bytes.NewReader(out))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 3 {
			continue
		}
		sha, dateStr, subject := parts[0], parts[1], parts[2]
		captured, err := time.Parse(time.RFC3339, dateStr)
		if err != nil {
			captured = time.Time{}
		}
		ev = append(ev, core.Evidence{
			ID:         "gitlog:" + sha,
			Kind:       core.EvidenceCommit,
			Source:     "evidence:" + string(SourceGitLog),
			Ref:        sha,
			Summary:    subject,
			CapturedAt: captured,
			Payload: markSynthesized(map[string]any{
				"sha":       sha,
				"subject":   subject,
				"native_id": t.NativeID,
				"repo_path": t.RepoPath,
				"work_item": string(t.ID),
				"committed": dateStr,
			}, SourceGitLog),
		})
	}
	if err := scanner.Err(); err != nil {
		return ev, fmt.Errorf("scan git log: %w", err)
	}
	return ev, nil
}
