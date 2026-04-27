package evidence

import (
	"context"
	"os/exec"

	"github.com/MikeBengtson/gemba/internal/core"
)

// Source labels the origin system a piece of synthesized Evidence
// came from. Stored verbatim in [core.Evidence].Source as
// "evidence:<source>".
type Source string

const (
	SourceGitLog   Source = "git_log"
	SourceGitHubPR Source = "github_pr"
	SourceCI       Source = "ci"
)

// PayloadKeySynthesized is the [core.Evidence].Payload key that flags
// a record as machine-synthesized (DD-13). Operator-attached evidence
// must NOT set this key.
const PayloadKeySynthesized = "synthesized"

// PayloadKeySource mirrors the [Source] in the payload so consumers
// can filter without parsing Evidence.Source.
const PayloadKeySource = "synth_source"

// Target is the input to a [Collector]. Carrying both the canonical
// WorkItemID and the bead's NativeID (e.g. "gm-e11.6") lets a
// collector grep for whichever shape is most likely to appear in the
// upstream system.
type Target struct {
	// ID is the workspace-qualified [core.WorkItemID]
	// (e.g. "gemba/gemba/gm-e11.6"). Always populated.
	ID core.WorkItemID
	// NativeID is the bare native id (e.g. "gm-e11.6"). Most VCS /
	// PR / CI references use this shape, not the workspace prefix.
	NativeID string
	// RepoPath is the absolute path to the working git repository
	// the collector should query. Empty disables the GitLog
	// collector (it can't run without a repo); the gh-backed
	// collectors use the working directory's repo context.
	RepoPath string
	// GitHubRepo is "owner/name" of the GitHub repo to query
	// (e.g. "MikeBengtson/gemba"). Empty disables the
	// GitHubPR / CI collectors.
	GitHubRepo string
}

// Collector is the abstraction over a single evidence source. The
// three concrete collectors in this package implement it; adaptors
// can supply more.
type Collector interface {
	// Source returns the canonical source identifier for tagging.
	Source() Source
	// Collect returns synthesized Evidence for the given target.
	// Implementations should respect ctx cancellation. Returning
	// (nil, nil) is valid — it means "no evidence found."
	Collect(ctx context.Context, t Target) ([]core.Evidence, error)
}

// Runner abstracts process execution so tests can substitute fakes
// for git / gh without shelling out. The default runner wraps
// os/exec.
//
// Implementations MUST honor ctx cancellation and return the combined
// stdout (any error output is the implementation's concern — the
// default attaches stderr to the returned error).
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// DefaultRunner shells out via os/exec.CommandContext. The working
// directory is left at the caller's cwd; collectors that need a
// specific cwd should wrap a closure around this.
func DefaultRunner() Runner {
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, name, args...).Output()
	}
}

// runnerInDir returns a Runner that runs every command with cwd set
// to dir. Used by the GitLogCollector.
func runnerInDir(base Runner, dir string) Runner {
	if dir == "" {
		return base
	}
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		// Inject -C <dir> for git; for other binaries we can't
		// portably set cwd through the Runner abstraction, so we
		// rely on git's own -C flag which all collectors here
		// happen to use only for git.
		if name == "git" {
			full := append([]string{"-C", dir}, args...)
			return base(ctx, name, full...)
		}
		return base(ctx, name, args...)
	}
}

// markSynthesized stamps the standard provenance keys on a freshly-
// constructed payload map. Helper used by every collector.
func markSynthesized(p map[string]any, src Source) map[string]any {
	if p == nil {
		p = map[string]any{}
	}
	p[PayloadKeySynthesized] = true
	p[PayloadKeySource] = string(src)
	return p
}
