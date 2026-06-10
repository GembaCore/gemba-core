package restore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/GembaCore/gemba-core/internal/server/gitops"
)

// Options drives Reconstruct.
type Options struct {
	// GitRemoteURL is the source-code remote (e.g. github.com/user/repo).
	// Required.
	GitRemoteURL string

	// GitDest is the directory the source code is cloned into.
	// Must not already exist. Required.
	GitDest string

	// DoltRemoteURL is the optional Dolt project-state remote. When
	// empty, the workspace is rehydrated from git alone; the user can
	// configure DoltHub later.
	DoltRemoteURL string

	// DoltDest is the destination directory for the dolt clone.
	// Must not already exist when DoltRemoteURL is set. The basename
	// becomes the local Dolt database name.
	DoltDest string
}

// Result captures the outcome of a Reconstruct, including which of the
// two remotes were actually exercised.
type Result struct {
	GitCloned  bool
	GitHeadSHA string

	DoltCloned bool
	DoltBranch string
}

// Reconstruct clones the configured remotes into their destinations.
// Failures are surfaced eagerly — a partial restore is considered a
// failed restore, since the user expects to either get a working
// workspace or a clear error.
func Reconstruct(ctx context.Context, opts Options) (*Result, error) {
	if opts.GitRemoteURL == "" {
		return nil, errors.New("restore: GitRemoteURL required")
	}
	if opts.GitDest == "" {
		return nil, errors.New("restore: GitDest required")
	}
	if _, err := os.Stat(opts.GitDest); err == nil {
		return nil, fmt.Errorf("restore: GitDest already exists: %s", opts.GitDest)
	}
	if opts.DoltRemoteURL != "" {
		if opts.DoltDest == "" {
			return nil, errors.New("restore: DoltDest required when DoltRemoteURL is set")
		}
		if _, err := os.Stat(opts.DoltDest); err == nil {
			return nil, fmt.Errorf("restore: DoltDest already exists: %s", opts.DoltDest)
		}
	}

	out := &Result{}

	// 1. Git clone.
	if err := os.MkdirAll(filepath.Dir(opts.GitDest), 0o755); err != nil {
		return out, fmt.Errorf("restore: mkdir GitDest parent: %w", err)
	}
	repo, err := gitops.Clone(ctx, opts.GitRemoteURL, opts.GitDest)
	if err != nil {
		return out, fmt.Errorf("restore: git clone: %w", err)
	}
	out.GitCloned = true
	if sha, err := repo.HeadSHA(ctx); err == nil {
		out.GitHeadSHA = sha
	}

	// 2. Dolt clone (optional).
	if opts.DoltRemoteURL == "" {
		return out, nil
	}
	if _, err := exec.LookPath("dolt"); err != nil {
		// Rollback the git clone so caller sees a clean failure.
		_ = os.RemoveAll(opts.GitDest)
		out.GitCloned = false
		return out, fmt.Errorf("restore: dolt binary not in PATH: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(opts.DoltDest), 0o755); err != nil {
		_ = os.RemoveAll(opts.GitDest)
		out.GitCloned = false
		return out, fmt.Errorf("restore: mkdir DoltDest parent: %w", err)
	}
	parentDir := filepath.Dir(opts.DoltDest)
	dbName := filepath.Base(opts.DoltDest)
	cmd := exec.CommandContext(ctx, "dolt", "clone", opts.DoltRemoteURL, dbName)
	cmd.Dir = parentDir
	if cmdOut, err := cmd.CombinedOutput(); err != nil {
		_ = os.RemoveAll(opts.GitDest)
		_ = os.RemoveAll(opts.DoltDest)
		out.GitCloned = false
		return out, fmt.Errorf("restore: dolt clone: %w (%s)", err, strings.TrimSpace(string(cmdOut)))
	}
	out.DoltCloned = true

	// Try to surface the active branch for the result. Not fatal if it fails.
	bcmd := exec.CommandContext(ctx, "dolt", "branch", "--show-current")
	bcmd.Dir = opts.DoltDest
	if bout, berr := bcmd.Output(); berr == nil {
		out.DoltBranch = strings.TrimSpace(string(bout))
	}
	return out, nil
}
