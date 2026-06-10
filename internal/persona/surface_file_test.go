package persona

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

// withTempHome points os.UserHomeDir at a t.TempDir() for the
// duration of the test. Restored on cleanup. Production code uses
// $HOME on every OS; this lets the surface tests sandbox writes
// without touching the operator's real ~/.gemba.
func withTempHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	envKey := homeEnvVar()
	prev, hadPrev := os.LookupEnv(envKey)
	if err := os.Setenv(envKey, dir); err != nil {
		t.Fatalf("setenv %s: %v", envKey, err)
	}
	t.Cleanup(func() {
		if hadPrev {
			_ = os.Setenv(envKey, prev)
		} else {
			_ = os.Unsetenv(envKey)
		}
	})
	return dir
}

func homeEnvVar() string {
	if runtime.GOOS == "windows" {
		return "USERPROFILE"
	}
	return "HOME"
}

func TestSurfaceFilePath_EmptySessionIDIsRejected(t *testing.T) {
	withTempHome(t)
	if _, err := SurfaceFilePath(""); err == nil {
		t.Fatal("SurfaceFilePath(\"\") = nil err, want rejection")
	}
}

func TestWriteSurfaceFile_RoundTripsThroughJSON(t *testing.T) {
	home := withTempHome(t)
	want := Surface{
		Cwd:               "/work/repo-a",
		AdditionalWrites:  []string{"/var/log/gemba"},
		SiblingReads:      []string{"/work/repo-b", "/work/repo-c"},
		WorkspaceMetadata: "/work/.gemba",
		ToolingPaths:      []string{"$HOME/.gitconfig", "$HOME/.cargo"},
		AdditionalReads:   []string{"$HOME/.aws/credentials"},
	}
	if err := WriteSurfaceFile("native:gm-foo:1234", want); err != nil {
		t.Fatalf("WriteSurfaceFile: %v", err)
	}

	// File lands at the expected path.
	expected := filepath.Join(home, ".gemba", "surfaces", SafeSessionID("native:gm-foo:1234")+".json")
	if _, err := os.Stat(expected); err != nil {
		t.Fatalf("expected file at %s: %v", expected, err)
	}

	// Decode it the way cmd/gemba-bridge/preuse.go's loadSurface
	// does — limit reader, json.Unmarshal — to pin cross-package
	// compatibility without importing the bridge.
	fh, err := os.Open(expected)
	if err != nil {
		t.Fatal(err)
	}
	defer fh.Close()
	raw, err := io.ReadAll(io.LimitReader(fh, 1<<20))
	if err != nil {
		t.Fatal(err)
	}
	var got Surface
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("bridge-compatible decode failed: %v\nraw: %s", err, raw)
	}
	if got.Cwd != want.Cwd {
		t.Errorf("Cwd = %q, want %q", got.Cwd, want.Cwd)
	}
	if len(got.SiblingReads) != 2 {
		t.Errorf("SiblingReads = %v, want 2 entries", got.SiblingReads)
	}
	if len(got.AdditionalReads) != 1 || got.AdditionalReads[0] != "$HOME/.aws/credentials" {
		t.Errorf("AdditionalReads = %v, want [$HOME/.aws/credentials]", got.AdditionalReads)
	}
}

func TestWriteSurfaceFile_PermissionsAreOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission semantics don't apply on Windows")
	}
	withTempHome(t)
	if err := WriteSurfaceFile("s1", Surface{Cwd: "/work/r"}); err != nil {
		t.Fatal(err)
	}
	path, _ := SurfaceFilePath("s1")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	mode := info.Mode().Perm()
	if mode != SurfaceFileMode {
		t.Errorf("mode = %#o, want %#o (file may carry sensitive paths; never world-readable)", mode, SurfaceFileMode)
	}
}

func TestWriteSurfaceFile_OverwritesPriorAtomically(t *testing.T) {
	withTempHome(t)
	if err := WriteSurfaceFile("s1", Surface{Cwd: "/work/r1"}); err != nil {
		t.Fatal(err)
	}
	if err := WriteSurfaceFile("s1", Surface{Cwd: "/work/r2"}); err != nil {
		t.Fatal(err)
	}
	path, _ := SurfaceFilePath("s1")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got Surface
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Cwd != "/work/r2" {
		t.Errorf("Cwd = %q, want /work/r2 — overwrite did not land", got.Cwd)
	}
	// No leftover .tmp from the rename.
	if _, err := os.Stat(path + ".tmp"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf(".tmp leftover after successful rename: stat err = %v", err)
	}
}

func TestRemoveSurfaceFile_MissingFileIsNoOp(t *testing.T) {
	withTempHome(t)
	if err := RemoveSurfaceFile("never-written"); err != nil {
		t.Errorf("RemoveSurfaceFile on missing file = %v, want nil", err)
	}
}

func TestRemoveSurfaceFile_RemovesWrittenFile(t *testing.T) {
	withTempHome(t)
	if err := WriteSurfaceFile("s1", Surface{Cwd: "/x"}); err != nil {
		t.Fatal(err)
	}
	if err := RemoveSurfaceFile("s1"); err != nil {
		t.Fatal(err)
	}
	path, _ := SurfaceFilePath("s1")
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("file still exists after Remove: %v", err)
	}
}

func TestWriteSurfaceFile_ConcurrentDistinctSessionsDontRace(t *testing.T) {
	withTempHome(t)
	var wg sync.WaitGroup
	const n = 16
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := "session-" + string(rune('a'+i))
			if err := WriteSurfaceFile(id, Surface{Cwd: "/work/" + id}); err != nil {
				t.Errorf("WriteSurfaceFile(%s): %v", id, err)
			}
		}(i)
	}
	wg.Wait()
	for i := 0; i < n; i++ {
		id := "session-" + string(rune('a'+i))
		path, _ := SurfaceFilePath(id)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("missing file for %s: %v", id, err)
		}
	}
}

// TestSafeSessionIDMatchesBridgeRule pins the byte-for-byte equivalence
// between persona.SafeSessionID and the bridge's private
// safeSessionID copy (cmd/gemba-bridge/main.go:148). The rule below
// is a literal copy — if you change one, change both.
func TestSafeSessionIDMatchesBridgeRule(t *testing.T) {
	bridgeRule := func(id string) string {
		out := make([]byte, 0, len(id))
		for i := 0; i < len(id); i++ {
			c := id[i]
			switch {
			case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
				out = append(out, c)
			case c == '-', c == '.', c == '_':
				out = append(out, c)
			default:
				out = append(out, '_')
			}
		}
		return string(out)
	}
	cases := []string{
		"plain-id",
		"native:gm-foo:1234567890",
		"gemba/gemba/gm-e1",
		"with spaces and !chars",
		"under_score.dash-and",
		"",
	}
	for _, in := range cases {
		if got, want := SafeSessionID(in), bridgeRule(in); got != want {
			t.Errorf("SafeSessionID(%q) = %q, bridge rule = %q (must stay in sync)", in, got, want)
		}
	}
}
