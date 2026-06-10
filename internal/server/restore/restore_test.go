package restore

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GembaCore/gemba-core/internal/server/gitops"
)

func gitAvailable(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
}

func doltAvailable(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("dolt"); err != nil {
		t.Skip("dolt not available")
	}
}

// seedBareGit creates a bare repo with one seeded commit on main and
// returns its path (usable as a file:// remote URL).
func seedBareGit(t *testing.T) string {
	t.Helper()
	bare := t.TempDir()
	if out, err := exec.Command("git", "init", "--bare", "--initial-branch=main", bare).CombinedOutput(); err != nil {
		t.Fatalf("git init bare: %v (%s)", err, out)
	}
	seed := filepath.Join(t.TempDir(), "seed")
	r, err := gitops.Clone(context.Background(), bare, seed)
	if err != nil {
		t.Fatalf("seed clone: %v", err)
	}
	if err := os.WriteFile(filepath.Join(r.Path, "README.md"), []byte("source code\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.CommitAll(context.Background(), "seed"); err != nil {
		t.Fatal(err)
	}
	if err := r.Push(context.Background(), "origin"); err != nil {
		t.Fatal(err)
	}
	return bare
}

// seedBareDolt creates a Dolt database, seeds one row, and pushes it to
// a file:// remote so the test can clone it back. Skips when dolt is
// not installed.
func seedBareDolt(t *testing.T) string {
	t.Helper()
	doltAvailable(t)

	// Working repo.
	src := t.TempDir()
	for _, cmd := range [][]string{
		{"dolt", "init", "--name=gemba-agent", "--email=agent@gemba"},
		{"dolt", "sql", "-q", "CREATE TABLE state (k TEXT PRIMARY KEY, v TEXT)"},
		{"dolt", "sql", "-q", "INSERT INTO state VALUES ('hello', 'world')"},
		{"dolt", "add", "-A"},
		{"dolt", "commit", "-m", "seed: state"},
	} {
		c := exec.Command(cmd[0], cmd[1:]...)
		c.Dir = src
		if out, err := c.CombinedOutput(); err != nil {
			t.Skipf("dolt seed step %v failed (env may not support it): %v (%s)", cmd, err, out)
		}
	}

	// Remote.
	bare := filepath.Join(t.TempDir(), "remote.fake")
	for _, cmd := range [][]string{
		{"dolt", "remote", "add", "origin", "file://" + bare},
		{"dolt", "push", "origin", "main"},
	} {
		c := exec.Command(cmd[0], cmd[1:]...)
		c.Dir = src
		if out, err := c.CombinedOutput(); err != nil {
			t.Skipf("dolt remote/push: %v (%s)", err, out)
		}
	}
	return "file://" + bare
}

func TestReconstructGitOnly(t *testing.T) {
	gitAvailable(t)
	bare := seedBareGit(t)
	dest := filepath.Join(t.TempDir(), "ws", "repo")
	res, err := Reconstruct(context.Background(), Options{
		GitRemoteURL: bare,
		GitDest:      dest,
	})
	if err != nil {
		t.Fatalf("Reconstruct: %v", err)
	}
	if !res.GitCloned {
		t.Fatal("GitCloned=false after success")
	}
	if res.DoltCloned {
		t.Fatal("DoltCloned=true with no remote configured")
	}
	if _, err := os.Stat(filepath.Join(dest, "README.md")); err != nil {
		t.Fatalf("expected README.md after reconstruct: %v", err)
	}
	if len(res.GitHeadSHA) != 40 {
		t.Fatalf("HeadSHA looks wrong: %q", res.GitHeadSHA)
	}
}

func TestReconstructGitDestMustNotExist(t *testing.T) {
	gitAvailable(t)
	dest := t.TempDir()
	_, err := Reconstruct(context.Background(), Options{
		GitRemoteURL: "https://example.invalid/nope.git",
		GitDest:      dest,
	})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("Reconstruct against existing dest: %v", err)
	}
}

func TestReconstructRequiresFields(t *testing.T) {
	cases := []Options{
		{},
		{GitRemoteURL: "x"},
		{GitDest: "x"},
		{GitRemoteURL: "x", GitDest: "x", DoltRemoteURL: "y"}, // missing DoltDest
	}
	for i, c := range cases {
		if _, err := Reconstruct(context.Background(), c); err == nil {
			t.Errorf("case %d: want error, got nil", i)
		}
	}
}

func TestReconstructFullKeystone(t *testing.T) {
	gitAvailable(t)
	doltAvailable(t)
	gitURL := seedBareGit(t)
	doltURL := seedBareDolt(t)

	wsRoot := filepath.Join(t.TempDir(), "ws-restored")
	res, err := Reconstruct(context.Background(), Options{
		GitRemoteURL:  gitURL,
		GitDest:       filepath.Join(wsRoot, "repo"),
		DoltRemoteURL: doltURL,
		DoltDest:      filepath.Join(wsRoot, "dolt-state"),
	})
	if err != nil {
		t.Fatalf("Reconstruct: %v", err)
	}
	if !res.GitCloned || !res.DoltCloned {
		t.Fatalf("expected both clones, got %+v", res)
	}
	// Source code present.
	if _, err := os.Stat(filepath.Join(wsRoot, "repo", "README.md")); err != nil {
		t.Fatalf("git side missing README.md: %v", err)
	}
	// Project state present — query the seeded row.
	cmd := exec.Command("dolt", "sql", "-q", "SELECT v FROM state WHERE k='hello'")
	cmd.Dir = filepath.Join(wsRoot, "dolt-state")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("query restored dolt: %v (%s)", err, out)
	}
	if !strings.Contains(string(out), "world") {
		t.Fatalf("seeded row missing from restored dolt:\n%s", out)
	}
}

func TestReconstructDoltMissingRollsBackGit(t *testing.T) {
	gitAvailable(t)
	// Force the dolt-missing path even if dolt is installed: point at an
	// invalid url and rely on dolt's clone failing fast.
	if _, err := exec.LookPath("dolt"); err != nil {
		t.Skip("dolt not available; this test verifies rollback when dolt itself errors")
	}
	bare := seedBareGit(t)
	wsRoot := filepath.Join(t.TempDir(), "ws-rollback")
	_, err := Reconstruct(context.Background(), Options{
		GitRemoteURL:  bare,
		GitDest:       filepath.Join(wsRoot, "repo"),
		DoltRemoteURL: "file:///nonexistent-dolt-remote-aaaaaaaaaaa",
		DoltDest:      filepath.Join(wsRoot, "dolt"),
	})
	if err == nil {
		t.Fatalf("expected dolt clone to fail")
	}
	if _, statErr := os.Stat(filepath.Join(wsRoot, "repo")); statErr == nil {
		t.Fatalf("rollback should have removed git clone")
	}
}
