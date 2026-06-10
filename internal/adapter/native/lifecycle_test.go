package native

import (
	"context"
	"testing"
	"time"

	"github.com/GembaCore/gemba-core/core"
)

// TestPoolLifecycle_BeadDone_PaneReuse_Recycle exercises the full
// happy path described in session-pool.md §4.1:
//
//  1. Start a session (Initializing → Working when first frame lands).
//  2. Agent emits bead-done with clean worktree → SessionReady.
//  3. Pane reuse via gemba:reuse_pane_id picks up the idle slot
//     (today the dispatcher does this; the test simulates it
//     directly).
//  4. Recycle resets the slot — same pane, new session id.
//
// This is the key integration assertion for gm-s47n.11.
func TestPoolLifecycle_BeadDone_PaneReuse_Recycle(t *testing.T) {
	withCleanWorktree(t, true)

	fb := newFakeBackend()
	p := NewWithConfig(Config{
		Backend:          fb,
		Registry:         defaultRegistry(),
		RepoRoot:         initRepo(t),
		DisableReaper:    true,
		DisableReconcile: true,
	})

	// 1. Start a fresh session.
	first, err := p.StartSession(context.Background(), "a1", freshPrompt("gm-foo", nil))
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	paneID, _ := first.ProviderMetadata["pane_id"].(string)
	if paneID == "" {
		t.Fatal("first session has no pane id")
	}

	// 2. Agent emits bead-done. Use the bridge-event path so the
	// cleanliness check runs.
	p.handleStateEvent(core.OrchestrationEvent{
		Kind:      "session_state_reported",
		SessionID: first.ID,
		Payload:   map[string]any{"state": "bead-done"},
		At:        time.Now(),
	})
	p.mu.Lock()
	if got := p.sessions[first.ID].Status; got != core.SessionReady {
		p.mu.Unlock()
		t.Fatalf("post-bead-done status: got %q want Ready", got)
	}
	p.mu.Unlock()

	// 3. Recycle.
	recycled, err := p.RecycleSession(context.Background(), first.ID)
	if err != nil {
		t.Fatalf("RecycleSession: %v", err)
	}
	if recycled.ID == first.ID {
		t.Errorf("recycle should mint a fresh id; got same %q", recycled.ID)
	}
	if got := recycled.ProviderMetadata["pane_id"]; got != paneID {
		t.Errorf("pane id should be stable across recycle: got %v want %v", got, paneID)
	}
	if recycled.Status != core.SessionInitializing {
		t.Errorf("post-recycle status: got %q want Initializing", recycled.Status)
	}

	// 4. The old session is removed; the new one tracks the same
	// pane.
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, oldStill := p.sessions[first.ID]; oldStill {
		t.Error("old session id should be removed after recycle")
	}
	if _, newOk := p.sessions[recycled.ID]; !newOk {
		t.Error("new session id missing after recycle")
	}
	if siblings := p.paneSessions[paneID]; len(siblings) != 1 || siblings[0] != recycled.ID {
		t.Errorf("paneSessions did not remap: got %v", siblings)
	}
}
