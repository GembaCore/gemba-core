package bd

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestProbeUsesConfiguredProbeDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake bd shell script is POSIX-only")
	}

	bin := t.TempDir()
	fakeBD := filepath.Join(bin, "bd")
	if err := os.WriteFile(fakeBD, []byte("#!/bin/sh\nprintf '[]\\n'\n"), 0o755); err != nil {
		t.Fatalf("write fake bd: %v", err)
	}
	t.Setenv("PATH", bin)

	worktree := t.TempDir()
	if err := os.Mkdir(filepath.Join(worktree, ".beads"), 0o755); err != nil {
		t.Fatalf("mkdir .beads: %v", err)
	}

	SetProbeDir(worktree)
	t.Cleanup(func() { SetProbeDir("") })

	if got := detect(); !got.Ok {
		t.Fatalf("detect() healthy = false, reason %q", got.Reason)
	}
	if got := probe(); !got.Ok {
		t.Fatalf("probe() healthy = false, reason %q", got.Reason)
	}
}

func TestDetectWithoutProbeDirUsesCwd(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake bd shell script is POSIX-only")
	}

	bin := t.TempDir()
	fakeBD := filepath.Join(bin, "bd")
	if err := os.WriteFile(fakeBD, []byte("#!/bin/sh\nprintf '[]\\n'\n"), 0o755); err != nil {
		t.Fatalf("write fake bd: %v", err)
	}
	t.Setenv("PATH", bin)
	SetProbeDir("")

	wd := t.TempDir()
	t.Chdir(wd)
	if got := detect(); got.Ok || got.Reason != "no .beads/ directory in cwd or any ancestor" {
		t.Fatalf("detect() = {Ok:%v Reason:%q}, want cwd missing .beads reason", got.Ok, got.Reason)
	}
}
