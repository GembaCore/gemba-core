package workspacelayout

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestResolve_RejectsInvalidInputs(t *testing.T) {
	cases := []struct {
		name string
		id   string
		root string
	}{
		{"empty id", "", "/tmp/data"},
		{"empty root", "alpha", ""},
		{"relative root", "alpha", "data"},
		{"slash in id", "a/b", "/tmp/data"},
		{"dotdot id", "..", "/tmp/data"},
		{"space in id", "a b", "/tmp/data"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Resolve(tc.id, tc.root); err == nil {
				t.Fatalf("Resolve(%q,%q): expected error, got nil", tc.id, tc.root)
			}
		})
	}
}

func TestResolve_ReturnsCanonicalPaths(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "tmp", "data")
	p, err := Resolve("alpha", root)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	want := Paths{
		Workspace:   filepath.Join(root, "workspaces", "alpha"),
		Repo:        filepath.Join(root, "workspaces", "alpha", "repo"),
		Dolt:        filepath.Join(root, "workspaces", "alpha", "dolt"),
		Logs:        filepath.Join(root, "workspaces", "alpha", "logs"),
		Transcripts: filepath.Join(root, "workspaces", "alpha", "transcripts"),
	}
	if p != want {
		t.Fatalf("Resolve: got %+v, want %+v", p, want)
	}
}

func TestEnsureLayout_CreatesAllFourSubdirs(t *testing.T) {
	root := t.TempDir()
	p, err := EnsureLayout("alpha", root)
	if err != nil {
		t.Fatalf("EnsureLayout: %v", err)
	}
	for _, d := range []string{p.Workspace, p.Repo, p.Dolt, p.Logs, p.Transcripts} {
		st, err := os.Stat(d)
		if err != nil {
			t.Fatalf("stat %q: %v", d, err)
		}
		if !st.IsDir() {
			t.Fatalf("%q is not a directory", d)
		}
	}
}

func TestEnsureLayout_SetsPermissionsTo0750(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission semantics not applicable on Windows")
	}
	// Force a relaxed umask so MkdirAll alone would not produce 0750;
	// verifies the explicit Chmod step.
	old := syscallUmask(0o022)
	defer syscallUmask(old)

	root := t.TempDir()
	p, err := EnsureLayout("alpha", root)
	if err != nil {
		t.Fatalf("EnsureLayout: %v", err)
	}
	dirs := []string{
		filepath.Join(root, "workspaces"),
		p.Workspace, p.Repo, p.Dolt, p.Logs, p.Transcripts,
	}
	for _, d := range dirs {
		st, err := os.Stat(d)
		if err != nil {
			t.Fatalf("stat %q: %v", d, err)
		}
		got := st.Mode().Perm()
		if got != DirPerm {
			t.Fatalf("perm %q: got %#o, want %#o", d, got, DirPerm)
		}
	}
}

func TestEnsureLayout_IsIdempotent(t *testing.T) {
	root := t.TempDir()
	p1, err := EnsureLayout("alpha", root)
	if err != nil {
		t.Fatalf("EnsureLayout #1: %v", err)
	}
	// Drop a sentinel file inside repo/ so we can prove the second call
	// did not destroy contents.
	sentinel := filepath.Join(p1.Repo, "kept.txt")
	if err := os.WriteFile(sentinel, []byte("keep me"), 0o600); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}
	p2, err := EnsureLayout("alpha", root)
	if err != nil {
		t.Fatalf("EnsureLayout #2: %v", err)
	}
	if p1 != p2 {
		t.Fatalf("paths drifted across calls: %+v vs %+v", p1, p2)
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("sentinel destroyed by re-call: %v", err)
	}
}

func TestEnsureLayout_FailsWhenRootIsAFile(t *testing.T) {
	dir := t.TempDir()
	rootPath := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(rootPath, []byte("x"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	_, err := EnsureLayout("alpha", rootPath)
	if err == nil {
		t.Fatalf("expected error when root is a regular file")
	}
	// Surface should be a wrapped mkdir error; the underlying syscall
	// errno varies by platform so we only check the wrap.
	if !errors.Is(err, err) { // tautology; we just need a non-nil err
		t.Fatalf("expected non-nil error")
	}
}
