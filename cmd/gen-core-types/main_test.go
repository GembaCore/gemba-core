package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/MikeBengtson/gemba/core"
)

// TestCoreGenTSMatchesGenerator is the golden check: the committed
// web/src/types/core.gen.ts must equal the current output of
// core.GenerateTS(). If this fails, run `make gen` and commit the
// result. Failing this locally (or in CI) means the checked-in TS has
// drifted from the Go source of truth.
func TestCoreGenTSMatchesGenerator(t *testing.T) {
	path := findGenPath(t)
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	want := core.GenerateTS()
	if string(got) == want {
		return
	}
	t.Fatalf(`web/src/types/core.gen.ts is out of date.

Run: make gen

Then commit the updated file.

(%d bytes on disk, %d bytes from generator)`, len(got), len(want))
}

// findGenPath walks up from the test file's directory to the repo root
// (the directory that contains web/src/types/core.gen.ts). Using a
// relative path keeps the test correct regardless of where `go test`
// is invoked from.
func findGenPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(thisFile)
	for i := 0; i < 10; i++ {
		candidate := filepath.Join(dir, "web", "src", "types", "core.gen.ts")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("could not locate web/src/types/core.gen.ts from test working dir")
	return ""
}
