//go:build integration

// Package native: testhelpers_tmux_test.go provides shared helpers for
// the Phase B real-tmux integration suite (Plan B-04, bead gm-v01.3.6).
//
// Build-tag-guarded — without `-tags integration` these helpers are
// invisible to `go test ./...` so CI without tmux stays green. Inside
// the integration suite, every helper centralizes one decision:
//
//   - requireTmux: PATH gate + Skipf — mirrors the convention in
//     peek_test.go and is the ONLY place we shell to exec.LookPath
//     across the suite.
//
//   - newIntegrationTmux: hands back a *backend.Tmux bound to an
//     ISOLATED tmux server. Isolation strategy: set TMUX_TMPDIR to a
//     per-test t.TempDir() BEFORE constructing the backend, so every
//     tmux invocation it forks inherits the env and writes its socket
//     under that temp dir. No collision with a developer's already-
//     running tmux server, and t.Cleanup unconditionally tears down.
//     We chose this over adding a NewTmuxWithSocket constructor because
//     it requires zero changes to production code — the env-var path is
//     a standard tmux convention and Phase F isolation tests can adopt
//     the same helper unchanged.
//
//   - newIntegrationPlane: builds an OrchestrationPlane wired to a real
//     tmux backend, spawns a pane running the caller's command, and
//     injects a synthetic core.Session into the plane's session map.
//     We bypass the full StartSession path (worktree provisioning,
//     bridge install, registry lookups) — none of that is on the
//     critical path for SessionIO acceptance and dragging it in would
//     turn these tests into wide-spectrum integration tests instead of
//     the focused SessionIO-trio coverage Plan B-04 calls for.
package native

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/adapter/native/backend"
)

// requireTmux asserts tmux is on $PATH and returns the resolved binary
// path. On miss it Skipf's so the test reports SKIP (not FAIL) on hosts
// without tmux — same pattern peek_test.go uses for backend-prerequisite
// tests.
func requireTmux(t *testing.T) string {
	t.Helper()
	p, err := exec.LookPath("tmux")
	if err != nil {
		t.Skipf("tmux not on PATH: %v", err)
	}
	return p
}

// integrationTmux wraps a *backend.Tmux with the resolved binary path
// and the isolated TMUX_TMPDIR the helper provisioned. Held by tests
// only for cleanup; functional access goes through the embedded Tmux.
type integrationTmux struct {
	*backend.Tmux
	binary  string
	tmpDir  string
	sockTag string
}

// newIntegrationTmux constructs a Tmux backend bound to a per-test
// isolated tmux server. The server lives under t.TempDir() (set via
// TMUX_TMPDIR before NewTmux is called) and is killed on t.Cleanup
// regardless of test outcome. Skipf's if tmux is not installed.
func newIntegrationTmux(t *testing.T) *integrationTmux {
	t.Helper()
	binary := requireTmux(t)

	// Per-test isolation: TMUX_TMPDIR controls the parent dir tmux
	// uses for its default socket. Setting it BEFORE NewTmux means
	// every tmux command the backend forks inherits the env and
	// writes its socket under our temp dir. t.Setenv handles restore.
	tmpDir := t.TempDir()
	t.Setenv("TMUX_TMPDIR", tmpDir)

	// Generate a short random suffix to use as the session name so
	// concurrent tests in the same process never collide on session
	// "gemba" (the default NewTmux constructor uses).
	tag := randHex(t, 4)

	tx, err := backend.NewTmux()
	if err != nil {
		t.Fatalf("NewTmux: %v", err)
	}

	t.Cleanup(func() {
		// Best-effort kill of the isolated server. Use a fresh
		// context with a 3s deadline so a wedged tmux can't hang
		// teardown indefinitely.
		killCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		cmd := exec.CommandContext(killCtx, binary, "kill-server")
		cmd.Env = append(os.Environ(), "TMUX_TMPDIR="+tmpDir)
		_ = cmd.Run()
	})

	return &integrationTmux{Tmux: tx, binary: binary, tmpDir: tmpDir, sockTag: tag}
}

// integrationPlane bundles an OrchestrationPlane with the tmux backend
// it was wired to + the pane id it spawned, so test cleanup can kill
// the pane explicitly (defense in depth — kill-server should handle it,
// but the pane-level kill keeps the FIFO cleanup test honest).
type integrationPlane struct {
	*OrchestrationPlane
	backend   *integrationTmux
	sessionID string
	paneID    string
}

// newIntegrationPlane spawns a pane running `cmd` (a sh -c string), then
// inserts a synthetic core.Session into the plane's session map so the
// hub's resolver can translate sessionID -> paneID. Returns the plane,
// the synthetic sessionID, and the underlying paneID for cleanup. Cleans
// up via Close() + Kill on t.Cleanup.
func newIntegrationPlane(t *testing.T, cmd string) *integrationPlane {
	t.Helper()
	be := newIntegrationTmux(t)

	// Spawn a pane. SpawnSpec.Cwd MUST be set; use t.TempDir so the
	// pane lives outside the repo. Command runs under sh -c so callers
	// can pass shell snippets (cat, trap, while-read).
	pane, err := be.SpawnPane(context.Background(), backend.SpawnSpec{
		Cwd:     t.TempDir(),
		Command: []string{"sh", "-c", cmd},
		Title:   "gemba-int-" + be.sockTag,
	})
	if err != nil {
		t.Fatalf("SpawnPane: %v", err)
	}

	// Give the pane a moment to enter its read loop before the test
	// starts sending input. Without this the first SendInput races
	// against the shell's startup and bytes can be lost.
	time.Sleep(150 * time.Millisecond)

	plane := NewWithConfig(Config{
		Backend:          be.Tmux,
		Registry:         defaultRegistry(),
		RepoRoot:         t.TempDir(),
		DisableReaper:    true,
		DisableReconcile: true,
	})

	sessionID := "test-" + randHex(t, 6)
	plane.mu.Lock()
	plane.sessions[sessionID] = &core.Session{
		ID:     sessionID,
		Status: core.SessionReady,
		ProviderMetadata: map[string]any{
			"pane_id": pane.ID,
		},
	}
	plane.paneSessions[pane.ID] = []string{sessionID}
	plane.mu.Unlock()

	t.Cleanup(func() {
		plane.Close()
		killCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = be.Tmux.Kill(killCtx, pane.ID)
	})

	return &integrationPlane{
		OrchestrationPlane: plane,
		backend:            be,
		sessionID:          sessionID,
		paneID:             pane.ID,
	}
}

// randHex returns a 2*n character hex string. Used for unique pane
// titles, session ids, and socket tags so concurrent tests never
// collide. Fails the test on RNG failure (effectively impossible).
func randHex(t *testing.T, n int) string {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	return hex.EncodeToString(b)
}
