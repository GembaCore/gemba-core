package gitops

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitAvailable skips a test when `git` is not installed in PATH.
func gitAvailable(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available in PATH")
	}
}

// initBare creates an empty bare git repo to serve as a remote.
func initBare(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cmd := exec.Command("git", "init", "--bare", "--initial-branch=main", dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("init bare: %v (%s)", err, out)
	}
	return dir
}

// seedClone clones bareURL into a fresh tempdir and seeds one commit on
// `main` so the bare has refs/heads/main.
func seedClone(t *testing.T, bareURL string) *Repo {
	t.Helper()
	dest := filepath.Join(t.TempDir(), "seed")
	r, err := Clone(context.Background(), bareURL, dest)
	if err != nil {
		t.Fatalf("seed clone: %v", err)
	}
	if err := os.WriteFile(filepath.Join(r.Path, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatalf("write seed: %v", err)
	}
	if _, err := r.CommitAll(context.Background(), "seed: initial"); err != nil {
		t.Fatalf("seed commit: %v", err)
	}
	if err := r.Push(context.Background(), "origin"); err != nil {
		t.Fatalf("seed push: %v", err)
	}
	return r
}

func TestCloneAndCommitPush(t *testing.T) {
	gitAvailable(t)
	bare := initBare(t)
	seedClone(t, bare)

	dest := filepath.Join(t.TempDir(), "agent-side")
	r, err := Clone(context.Background(), bare, dest)
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if got, _ := r.CurrentBranch(context.Background()); got != "main" {
		t.Fatalf("CurrentBranch=%q want main", got)
	}
	if dirty, _ := r.IsDirty(context.Background()); dirty {
		t.Fatalf("fresh clone should be clean")
	}
	// Agent edits a file.
	if err := os.WriteFile(filepath.Join(r.Path, "feature.md"), []byte("agent\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	dirty, _ := r.IsDirty(context.Background())
	if !dirty {
		t.Fatalf("expected dirty after write")
	}
	sha, err := r.CommitAll(context.Background(), "feat: add feature.md")
	if err != nil {
		t.Fatalf("CommitAll: %v", err)
	}
	if len(sha) != 40 {
		t.Fatalf("CommitAll sha looks wrong: %q", sha)
	}
	// Push back to the bare remote.
	if err := r.Push(context.Background(), "origin"); err != nil {
		t.Fatalf("Push: %v", err)
	}
	// Verify the bare now contains the commit.
	out, err := exec.Command("git", "--git-dir", bare, "log", "-1", "--pretty=%s", "main").CombinedOutput()
	if err != nil {
		t.Fatalf("bare log: %v (%s)", err, out)
	}
	if !strings.Contains(string(out), "feat: add feature.md") {
		t.Fatalf("bare missing commit: %s", out)
	}
	// Verify authorship.
	authOut, err := exec.Command("git", "--git-dir", bare, "log", "-1", "--pretty=%an <%ae>", "main").CombinedOutput()
	if err != nil {
		t.Fatalf("bare author: %v", err)
	}
	want := AgentSignature.Name + " <" + AgentSignature.Email + ">"
	if !strings.Contains(string(authOut), want) {
		t.Fatalf("agent attribution missing: %s (want %s)", authOut, want)
	}
}

func TestCommitAllErrNoChanges(t *testing.T) {
	gitAvailable(t)
	bare := initBare(t)
	r := seedClone(t, bare)
	if _, err := r.CommitAll(context.Background(), "no-op"); err != ErrNoChanges {
		t.Fatalf("CommitAll on clean: want ErrNoChanges, got %v", err)
	}
}

func TestCreateBranchAndPush(t *testing.T) {
	gitAvailable(t)
	bare := initBare(t)
	seedClone(t, bare)

	dest := filepath.Join(t.TempDir(), "feature")
	r, err := Clone(context.Background(), bare, dest)
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if err := r.CreateBranch(context.Background(), "feat/agent-1"); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	br, _ := r.CurrentBranch(context.Background())
	if br != "feat/agent-1" {
		t.Fatalf("CurrentBranch=%q want feat/agent-1", br)
	}
	if err := os.WriteFile(filepath.Join(r.Path, "f.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.CommitAll(context.Background(), "feat: branch work"); err != nil {
		t.Fatalf("CommitAll: %v", err)
	}
	if err := r.Push(context.Background(), "origin"); err != nil {
		t.Fatalf("Push (first push of new branch): %v", err)
	}
	// Bare should have feat/agent-1 ref.
	out, err := exec.Command("git", "--git-dir", bare, "branch", "-a").CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "feat/agent-1") {
		t.Fatalf("bare missing feat branch: %s", out)
	}
}

func TestPullFastForward(t *testing.T) {
	gitAvailable(t)
	bare := initBare(t)
	upstream := seedClone(t, bare)

	// Second clone — the agent side.
	dest := filepath.Join(t.TempDir(), "agent-pull")
	r, err := Clone(context.Background(), bare, dest)
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	// Upstream advances.
	if err := os.WriteFile(filepath.Join(upstream.Path, "upstream.md"), []byte("u\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := upstream.CommitAll(context.Background(), "upstream advance"); err != nil {
		t.Fatal(err)
	}
	if err := upstream.Push(context.Background(), "origin"); err != nil {
		t.Fatal(err)
	}
	// Agent pulls — should fast-forward cleanly.
	if err := r.Pull(context.Background()); err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if _, err := os.Stat(filepath.Join(r.Path, "upstream.md")); err != nil {
		t.Fatalf("expected upstream.md after pull: %v", err)
	}
}

func TestConfigureRemote(t *testing.T) {
	gitAvailable(t)
	dir := filepath.Join(t.TempDir(), "fresh")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v (%s)", err, out)
	}
	r, _ := Open(dir)
	if err := r.ConfigureRemote(context.Background(), "origin", "https://example.invalid/a.git"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := r.ConfigureRemote(context.Background(), "origin", "https://example.invalid/a.git"); err != nil {
		t.Fatalf("re-add same: %v", err)
	}
	if err := r.ConfigureRemote(context.Background(), "origin", "https://example.invalid/b.git"); err != nil {
		t.Fatalf("re-add changed: %v", err)
	}
	out, err := exec.Command("git", "-C", dir, "remote", "get-url", "origin").CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "/b.git") {
		t.Fatalf("expected /b.git, got %s", out)
	}
}

func TestOpenRequiresPath(t *testing.T) {
	if _, err := Open(""); err == nil {
		t.Fatal("Open(\"\"): want error")
	}
}
