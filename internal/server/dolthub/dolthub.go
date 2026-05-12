package dolthub

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/GembaCore/gemba-core/internal/server/secrets"
)

// Vault is the subset of secrets.Vault that this package needs. Modeled
// as an interface so tests can inject a fake without standing up the
// real AES-GCM store.
type Vault interface {
	Inject(name string) ([]byte, error)
}

// Compile-time assertion that *secrets.Vault satisfies the interface.
var _ Vault = (*secrets.Vault)(nil)

// Standard secret names used by gemba-remote DoltHub integration.
const (
	SecretRemoteURL    = "DOLTHUB_REMOTE_URL"
	SecretAuthToken    = "DOLTHUB_AUTH_TOKEN"
)

// Remote is a per-workspace DoltHub remote handle. RepoPath is the
// workspace's .dolt-data/<id>/ directory or any path containing a .dolt/
// subdirectory.
type Remote struct {
	RepoPath   string
	RemoteName string // defaults to "origin"
	Branch     string // defaults to "main"

	// Vault is read at push/pull time to obtain the URL + token. May be
	// nil; in that case Push/Pull use whatever remote the local repo
	// already has configured.
	Vault Vault
}

// Open constructs a Remote for the workspace at repoPath. vault may be
// nil for local-only operation.
func Open(repoPath string, vault Vault) *Remote {
	return &Remote{
		RepoPath:   repoPath,
		RemoteName: "origin",
		Branch:     "main",
		Vault:      vault,
	}
}

// Configure (re)writes the named remote URL from the vault. No-op when
// the URL in the vault matches the currently configured URL. Returns
// nil + no work performed when no vault is attached or no URL is set.
func (r *Remote) Configure(ctx context.Context) error {
	if r.Vault == nil {
		return nil
	}
	raw, err := r.Vault.Inject(SecretRemoteURL)
	if err != nil {
		if errors.Is(err, secrets.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("dolthub: read remote URL: %w", err)
	}
	defer secrets.Zero(raw)
	url := strings.TrimSpace(string(raw))
	if url == "" {
		return nil
	}
	cur, err := r.dolt(ctx, "remote", "-v")
	if err == nil && strings.Contains(cur, r.RemoteName+" "+url) {
		return nil
	}
	// Remove if it exists, then add. Easier than parsing.
	_, _ = r.dolt(ctx, "remote", "remove", r.RemoteName)
	_, err = r.dolt(ctx, "remote", "add", r.RemoteName, url)
	return err
}

// Push pushes the configured branch to the remote.
func (r *Remote) Push(ctx context.Context) error {
	if err := r.Configure(ctx); err != nil {
		return err
	}
	_, err := r.dolt(ctx, "push", r.RemoteName, r.Branch)
	return err
}

// Pull fetches and merges from the remote.
func (r *Remote) Pull(ctx context.Context) error {
	if err := r.Configure(ctx); err != nil {
		return err
	}
	_, err := r.dolt(ctx, "pull", r.RemoteName, r.Branch)
	return err
}

// Sync is a Pull followed by a Push. Most rigs run this on session start
// and session end; the helper exists so callers don't have to chain two
// commands and handle two error sites.
func (r *Remote) Sync(ctx context.Context) error {
	if err := r.Pull(ctx); err != nil {
		return fmt.Errorf("dolthub: pull: %w", err)
	}
	if err := r.Push(ctx); err != nil {
		return fmt.Errorf("dolthub: push: %w", err)
	}
	return nil
}

// Status returns the output of `dolt status` for inspection / SSE events.
func (r *Remote) Status(ctx context.Context) (string, error) {
	return r.dolt(ctx, "status")
}

// --- internal --------------------------------------------------------

func (r *Remote) dolt(ctx context.Context, args ...string) (string, error) {
	if _, err := exec.LookPath("dolt"); err != nil {
		return "", fmt.Errorf("dolthub: dolt binary not found in PATH: %w", err)
	}
	cmd := exec.CommandContext(ctx, "dolt", args...)
	cmd.Dir = r.RepoPath
	// Auth token plumbed via env so dolt picks it up via its existing
	// DOLTHUB-aware credential resolution.
	if r.Vault != nil {
		raw, err := r.Vault.Inject(SecretAuthToken)
		if err == nil {
			defer secrets.Zero(raw)
			cmd.Env = append(cmd.Environ(), "DOLTHUB_AUTH_TOKEN="+string(raw))
		}
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("dolthub: dolt %s: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}
