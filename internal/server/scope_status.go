package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/planner"
)

const scopeStatusTimeout = 2 * time.Second

type gitNexusStatusMeta struct {
	LastCommit string    `json:"lastCommit"`
	IndexedAt  time.Time `json:"indexedAt"`
}

func enrichScopeStatuses(ctx context.Context, contexts []*planner.OperationalContext) {
	for _, opCtx := range contexts {
		if opCtx == nil {
			continue
		}
		path := scopeStatusPath(opCtx)
		if path == "" {
			continue
		}
		opCtx.ScopeStatus = inspectScopeStatus(ctx, path)
	}
}

func enrichScopeStatus(ctx context.Context, opCtx *planner.OperationalContext) {
	if opCtx == nil {
		return
	}
	path := scopeStatusPath(opCtx)
	if path == "" {
		return
	}
	opCtx.ScopeStatus = inspectScopeStatus(ctx, path)
}

func scopeStatusPath(opCtx *planner.OperationalContext) string {
	if opCtx == nil {
		return ""
	}
	if opCtx.Workspace != nil {
		if path := core.WorkspaceWorktreePath(*opCtx.Workspace); path != "" {
			return path
		}
	}
	if opCtx.Session != nil {
		if path, ok := opCtx.Session.ProviderMetadata["worktree"].(string); ok && path != "" {
			return path
		}
		if path, ok := opCtx.Session.ProviderMetadata["worktree_path"].(string); ok && path != "" {
			return path
		}
	}
	return ""
}

func inspectScopeStatus(ctx context.Context, path string) *planner.ScopeStatus {
	head, headErr := gitOutput(ctx, path, "rev-parse", "HEAD")
	git := inspectGitScopeStatus(ctx, path, head, headErr)
	analysis := inspectAnalysisScopeStatus(path, head, headErr)
	if git != nil && git.State == "dirty" && analysis != nil && analysis.State == "current" {
		analysis.State = "stale"
		analysis.Reason = "worktree has uncommitted changes not reflected in GitNexus index"
	}
	status := &planner.ScopeStatus{
		Git:      git,
		Analysis: analysis,
	}
	return status
}

func inspectGitScopeStatus(ctx context.Context, path, head string, headErr error) *planner.GitScopeStatus {
	out := &planner.GitScopeStatus{HeadSHA: head}
	if headErr != nil {
		out.State = "unavailable"
		out.Reason = headErr.Error()
		return out
	}
	porcelain, err := gitOutput(ctx, path, "status", "--porcelain")
	if err != nil {
		out.State = "unavailable"
		out.Reason = err.Error()
		return out
	}
	out.ChangedFiles = countStatusLines(porcelain)
	if out.ChangedFiles == 0 {
		out.State = "clean"
	} else {
		out.State = "dirty"
	}

	upstream, err := gitOutput(ctx, path, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	if err != nil || upstream == "" {
		return out
	}
	out.Upstream = upstream
	counts, err := gitOutput(ctx, path, "rev-list", "--left-right", "--count", "HEAD...@{upstream}")
	if err != nil {
		return out
	}
	ahead, behind, ok := parseAheadBehind(counts)
	if !ok {
		return out
	}
	out.Ahead = ahead
	out.Behind = behind
	return out
}

func inspectAnalysisScopeStatus(path, head string, headErr error) *planner.AnalysisScopeStatus {
	out := &planner.AnalysisScopeStatus{
		Backend: "gitnexus",
		HeadSHA: head,
	}
	if headErr != nil {
		out.State = "unavailable"
		out.Reason = "cannot resolve HEAD: " + headErr.Error()
		return out
	}
	metaPath := filepath.Join(path, ".gitnexus", "meta.json")
	f, err := os.Open(metaPath)
	if err != nil {
		if os.IsNotExist(err) {
			out.State = "missing"
			out.Reason = "no GitNexus index"
			return out
		}
		out.State = "unavailable"
		out.Reason = "meta.json unreadable: " + err.Error()
		return out
	}
	defer f.Close()

	var meta gitNexusStatusMeta
	if err := json.NewDecoder(f).Decode(&meta); err != nil {
		out.State = "unavailable"
		out.Reason = "meta.json parse: " + err.Error()
		return out
	}
	out.IndexedAt = meta.IndexedAt
	out.IndexedCommit = meta.LastCommit
	if meta.LastCommit == "" {
		out.State = "unavailable"
		out.Reason = "GitNexus index does not record lastCommit"
		return out
	}
	if sameCommit(meta.LastCommit, head) {
		out.State = "current"
	} else {
		out.State = "stale"
		out.Reason = "GitNexus index commit differs from HEAD"
	}
	return out
}

func gitOutput(ctx context.Context, path string, args ...string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("worktree path is empty")
	}
	cmdCtx, cancel := context.WithTimeout(ctx, scopeStatusTimeout)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, "git", append([]string{"-C", path}, args...)...)
	out, err := cmd.CombinedOutput()
	s := strings.TrimSpace(string(out))
	if cmdCtx.Err() != nil {
		return s, cmdCtx.Err()
	}
	if err != nil {
		if s != "" {
			return s, fmt.Errorf("%s", s)
		}
		return s, err
	}
	return s, nil
}

func countStatusLines(s string) int {
	if strings.TrimSpace(s) == "" {
		return 0
	}
	count := 0
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}

func parseAheadBehind(s string) (int, int, bool) {
	fields := strings.Fields(s)
	if len(fields) < 2 {
		return 0, 0, false
	}
	ahead, errA := strconv.Atoi(fields[0])
	behind, errB := strconv.Atoi(fields[1])
	if errA != nil || errB != nil {
		return 0, 0, false
	}
	return ahead, behind, true
}

func sameCommit(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" || b == "" {
		return false
	}
	return a == b || strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
}
