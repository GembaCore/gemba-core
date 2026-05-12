package gitops

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Repo is a server-side git working tree at Path.
// The zero value is unusable; obtain a Repo via Open.
type Repo struct {
	// Path is the absolute path to the working tree.
	Path string

	// AuthorName and AuthorEmail attribute commits made by the agent
	// harness. If empty, falls back to "gemba-agent" / "agent@gemba".
	AuthorName  string
	AuthorEmail string
}

// AgentSignature is the default author identity for agent-authored commits.
// Use SignedBy() to construct a Repo with overrides.
var AgentSignature = struct {
	Name  string
	Email string
}{
	Name:  "gemba-agent",
	Email: "agent@gemba",
}

// Open returns a Repo rooted at path. The directory must exist; if it
// does not contain a .git entry the caller should Clone first.
func Open(path string) (*Repo, error) {
	if path == "" {
		return nil, errors.New("gitops: path required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("gitops: abs path: %w", err)
	}
	return &Repo{
		Path:        abs,
		AuthorName:  AgentSignature.Name,
		AuthorEmail: AgentSignature.Email,
	}, nil
}

// Clone shallow-clones remoteURL into dest. The destination must not
// exist. Returns an opened Repo. Authentication is inherited from the
// environment (ssh-agent, GIT_ASKPASS, GITHUB_TOKEN-style env vars).
func Clone(ctx context.Context, remoteURL, dest string) (*Repo, error) {
	if remoteURL == "" || dest == "" {
		return nil, errors.New("gitops: remoteURL and dest required")
	}
	if _, err := exec.LookPath("git"); err != nil {
		return nil, fmt.Errorf("gitops: git not found in PATH: %w", err)
	}
	cmd := exec.CommandContext(ctx, "git", "clone", remoteURL, dest)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("gitops: clone %s: %w (%s)", remoteURL, err, strings.TrimSpace(string(out)))
	}
	return Open(dest)
}

// CurrentBranch returns the short name of HEAD's branch or empty string
// if HEAD is detached.
func (r *Repo) CurrentBranch(ctx context.Context) (string, error) {
	out, err := r.run(ctx, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		// Detached HEAD: symbolic-ref returns non-zero.
		return "", nil
	}
	return strings.TrimSpace(out), nil
}

// HeadSHA returns the commit hash that HEAD points at.
func (r *Repo) HeadSHA(ctx context.Context) (string, error) {
	out, err := r.run(ctx, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// IsDirty returns true if the working tree has untracked or modified
// files relative to HEAD.
func (r *Repo) IsDirty(ctx context.Context) (bool, error) {
	out, err := r.run(ctx, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// DiffStat returns inserted-line / deleted-line counts and the number of
// files changed in the working tree vs HEAD. Untracked files are not
// included unless they have been `git add`-ed (matches git's standard
// behavior for --stat).
type DiffStat struct {
	FilesChanged int
	Insertions   int
	Deletions    int
}

// Diff returns the working-tree diff vs HEAD, with optional path filter.
// Returns empty string when the tree is clean.
func (r *Repo) Diff(ctx context.Context, paths ...string) (string, error) {
	args := []string{"diff", "HEAD"}
	if len(paths) > 0 {
		args = append(args, "--")
		args = append(args, paths...)
	}
	return r.run(ctx, args...)
}

// Pull fetches and fast-forward-merges the configured upstream of the
// current branch. Fails (without modifying the tree) if a non-FF merge
// would be required — the caller decides whether to rebase or surface a
// merge-conflict event.
func (r *Repo) Pull(ctx context.Context) error {
	_, err := r.run(ctx, "pull", "--ff-only")
	return err
}

// Checkout switches to branchName, creating it if create is true.
func (r *Repo) Checkout(ctx context.Context, branchName string, create bool) error {
	args := []string{"checkout"}
	if create {
		args = append(args, "-b")
	}
	args = append(args, branchName)
	_, err := r.run(ctx, args...)
	return err
}

// CreateBranch is a convenience for Checkout(name, true).
func (r *Repo) CreateBranch(ctx context.Context, name string) error {
	return r.Checkout(ctx, name, true)
}

// CommitAll stages every tracked + untracked change and commits with
// message and the configured author. Returns ErrNoChanges if the tree
// is clean.
func (r *Repo) CommitAll(ctx context.Context, message string) (string, error) {
	if message == "" {
		return "", errors.New("gitops: empty commit message")
	}
	dirty, err := r.IsDirty(ctx)
	if err != nil {
		return "", err
	}
	if !dirty {
		return "", ErrNoChanges
	}
	if _, err := r.run(ctx, "add", "-A"); err != nil {
		return "", err
	}
	// Author + committer = the agent (single identity for both).
	env := r.authorEnv()
	cmd := exec.CommandContext(ctx, "git",
		"-c", "commit.gpgsign=false",
		"commit", "-m", message)
	cmd.Dir = r.Path
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("gitops: commit: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return r.HeadSHA(ctx)
}

// Push pushes the current branch to its tracked upstream. If no upstream
// is set and remoteName is non-empty, sets the upstream to
// remoteName/<current-branch>.
func (r *Repo) Push(ctx context.Context, remoteName string) error {
	br, err := r.CurrentBranch(ctx)
	if err != nil || br == "" {
		return errors.New("gitops: cannot push detached HEAD")
	}
	// Check whether the branch has an upstream.
	_, upErr := r.run(ctx, "rev-parse", "--abbrev-ref", br+"@{upstream}")
	if upErr != nil && remoteName != "" {
		if _, err := r.run(ctx, "push", "--set-upstream", remoteName, br); err != nil {
			return err
		}
		return nil
	}
	_, err = r.run(ctx, "push")
	return err
}

// ConfigureRemote sets remote origin to remoteURL, creating it if absent
// or rewriting it if it differs.
func (r *Repo) ConfigureRemote(ctx context.Context, name, remoteURL string) error {
	if name == "" || remoteURL == "" {
		return errors.New("gitops: name and url required")
	}
	cur, err := r.run(ctx, "remote", "get-url", name)
	if err == nil {
		if strings.TrimSpace(cur) == remoteURL {
			return nil
		}
		_, err = r.run(ctx, "remote", "set-url", name, remoteURL)
		return err
	}
	_, err = r.run(ctx, "remote", "add", name, remoteURL)
	return err
}

// ErrNoChanges signals that CommitAll was called on a clean tree.
var ErrNoChanges = errors.New("gitops: no changes to commit")

// --- internal --------------------------------------------------------

func (r *Repo) run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = r.Path
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("gitops: git %s: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

func (r *Repo) authorEnv() []string {
	env := []string{
		"GIT_AUTHOR_NAME=" + r.name(),
		"GIT_AUTHOR_EMAIL=" + r.email(),
		"GIT_COMMITTER_NAME=" + r.name(),
		"GIT_COMMITTER_EMAIL=" + r.email(),
	}
	// Preserve a minimal environment: PATH so git can find helpers, HOME
	// so it can find ~/.gitconfig + .ssh, plus any auth env vars.
	for _, k := range []string{"PATH", "HOME", "SSH_AUTH_SOCK", "GIT_ASKPASS", "GIT_SSH_COMMAND", "GIT_CONFIG_GLOBAL"} {
		if v := getenv(k); v != "" {
			env = append(env, k+"="+v)
		}
	}
	return env
}

func (r *Repo) name() string {
	if r.AuthorName == "" {
		return AgentSignature.Name
	}
	return r.AuthorName
}

func (r *Repo) email() string {
	if r.AuthorEmail == "" {
		return AgentSignature.Email
	}
	return r.AuthorEmail
}

func getenv(k string) string { return strings.TrimSpace(os.Getenv(k)) }
