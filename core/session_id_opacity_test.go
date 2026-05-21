// Package core: session_id_opacity_test.go enforces CONTEXT.md
// decision C-21 — "sessionID stays opaque across the interface;
// adapters resolve to their own identity internally."
//
// Phase B's native tmux work adds the pane-id ↔ sessionID mapping
// inside internal/adapter/native/backend/tmux.go, which is the ONE
// allowlisted location where sessionID's tmux-pane shape may be
// assumed. Outside that directory no caller — transport handler,
// other adaptor, frontend codegen, conformance harness — may treat
// sessionID as a pane id. This test walks the tree on every
// `go test ./core/...` invocation and fails CI before merge if a
// future PR leaks a pane assumption to a caller.
package core

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// forbiddenOpacityPatterns are the regexes the walker fails on. Kept
// narrow so legitimate uses of `sessionID` and the word "pane" in
// surrounding code don't false-positive; the patterns target the
// specific co-occurrences that indicate a caller is conflating the
// two identifiers.
var forbiddenOpacityPatterns = []*regexp.Regexp{
	// Direct assignment of a pane id into a sessionID variable.
	regexp.MustCompile(`sessionID\s*[:=].*paneID`),
	// Pulling .PaneID off a struct keyed by sessionID.
	regexp.MustCompile(`sessionID.*\.PaneID\b`),
	// Converse: treating sessionID as if it IS a pane id.
	regexp.MustCompile(`paneID\s*[:=]\s*sessionID\b`),
	// sessionID interpolated into a tmux `-t` flag (that's a
	// target-pane flag and would be a leak).
	regexp.MustCompile(`tmux .*-t .*sessionID\b`),
}

// skippedDirs are directory names anywhere in the walk path that
// short-circuit descent. Includes vendor / generated / non-Go-source
// trees plus the one legitimate pane-id ↔ sessionID translation site
// (internal/adapter/native/backend) — see allowlist note below.
var skippedDirs = map[string]struct{}{
	"vendor":       {},
	"node_modules": {},
	"web":          {},
	".planning":    {},
	".beads":       {},
	"docs":         {},
	"docs-site":    {},
	".git":         {},
}

// allowlistSubstrings are path substrings that, when present anywhere
// in a candidate file's path, exempt the file. These are the named
// allowlist sites carved out by C-21:
//
//   - internal/adapter/native/backend/ : tmux backend, where the pane
//     id IS the implementation and the translation legitimately lives.
//   - cmd/gen-                        : codegen entry points whose
//     output is generated and not authored.
var allowlistSubstrings = []string{
	"internal/adapter/native/backend/",
	"cmd/gen-",
}

// TestSessionIDOpacity walks the repository from the module root and
// fails if any non-allowlisted .go file contains a code pattern
// suggesting a caller treats sessionID as a tmux pane id. Runs as
// part of `go test ./core/...` so opacity is enforced on every test
// invocation, not as a separate CI-only shell script.
func TestSessionIDOpacity(t *testing.T) {
	root, err := findModuleRoot()
	if err != nil {
		t.Fatalf("findModuleRoot: %v", err)
	}

	visited := 0
	violations := 0
	selfPath := filepath.Join(root, "core", "session_id_opacity_test.go")

	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if _, skip := skippedDirs[d.Name()]; skip {
				return filepath.SkipDir
			}
			return nil
		}

		// Only .go files.
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		// Skip the test file itself.
		if path == selfPath {
			return nil
		}
		// Skip *_test.go — tests legitimately stage pane ids in fakes
		// and would false-positive (e.g. mock plane's
		// reuse_pane_id ↔ session lookup test).
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		// Skip allowlisted directories (substring match on absolute
		// path; covers backend/ and codegen entry points).
		for _, sub := range allowlistSubstrings {
			if strings.Contains(path, sub) {
				return nil
			}
		}

		visited++
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()

		scanner := bufio.NewScanner(f)
		// Some generated .go files have very long lines.
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			line := scanner.Text()
			for _, re := range forbiddenOpacityPatterns {
				if re.MatchString(line) {
					t.Errorf("opacity violation in %s:%d: %s",
						path, lineNum, strings.TrimSpace(line))
					violations++
				}
			}
		}
		if err := scanner.Err(); err != nil {
			return err
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("WalkDir: %v", walkErr)
	}

	// Sanity assertion: a broken walker that silently skipped the
	// whole tree would let opacity violations through unnoticed. The
	// repo has hundreds of .go files; require at least 50 visited so
	// any future regression in skip logic surfaces here.
	if visited < 50 {
		t.Errorf("walker visited only %d files; expected >= 50. "+
			"Skip logic regressed or module root resolution is wrong.",
			visited)
	}
	if violations > 0 {
		t.Logf("found %d opacity violations across %d visited files", violations, visited)
	}
}

// findModuleRoot walks up from this file's directory looking for
// go.mod. Uses runtime.Caller so the answer is correct regardless of
// the cwd `go test` was invoked from.
func findModuleRoot() (string, error) {
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("runtime.Caller(0) failed")
	}
	dir := filepath.Dir(here)
	for i := 0; i < 20; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", errors.New("findModuleRoot: walked to filesystem root without finding go.mod")
}
