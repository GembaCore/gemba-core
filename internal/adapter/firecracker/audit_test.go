package firecracker

import (
	"context"
	"runtime"
	"sync"
	"testing"
	"time"
)

// recAuditor is a simple LifecycleAuditor that captures events for
// assertions. Independent of internal/server/audit so this package
// stays import-free of the server tree.
type recAuditor struct {
	mu     sync.Mutex
	events []recEvt
}

type recEvt struct {
	name    string
	payload map[string]any
}

func (r *recAuditor) VMEvent(_ context.Context, name string, payload map[string]any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, recEvt{name: name, payload: payload})
}

func (r *recAuditor) snapshot() []recEvt {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := make([]recEvt, len(r.events))
	copy(cp, r.events)
	return cp
}

// TestFallback_AuditEmitsSpawnAndDestroy covers gm-o9t8.3.2.7 — a
// Start success emits vm.spawn; a Stop emits vm.destroy with a
// non-zero duration_ms.
func TestFallback_AuditEmitsSpawnAndDestroy(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("fallback-only test")
	}

	a := &recAuditor{}
	sup := NewSupervisorWithOptions(Options{Auditor: a})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	vm, err := sup.Start(ctx, Spec{
		WorkspaceID:     "audit-ws",
		FallbackCommand: "exec cat",
		MemMB:           256,
		CPUCount:        2,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Give Stop something measurable.
	time.Sleep(20 * time.Millisecond)
	if err := sup.Stop(ctx, vm, time.Second); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	got := a.snapshot()
	if len(got) != 2 {
		t.Fatalf("expected 2 audit events, got %d (%+v)", len(got), got)
	}
	if got[0].name != "vm.spawn" {
		t.Fatalf("first event: got %q want vm.spawn", got[0].name)
	}
	if got[0].payload["vm_id"] != vm.ID {
		t.Fatalf("spawn vm_id mismatch: %v", got[0].payload["vm_id"])
	}
	if got[0].payload["wsid"] != "audit-ws" {
		t.Fatalf("spawn wsid: %v", got[0].payload["wsid"])
	}
	if got[0].payload["mem_mb"].(int) != 256 {
		t.Fatalf("spawn mem_mb: %v", got[0].payload["mem_mb"])
	}

	if got[1].name != "vm.destroy" {
		t.Fatalf("second event: got %q want vm.destroy", got[1].name)
	}
	dur, ok := got[1].payload["duration_ms"].(int64)
	if !ok || dur <= 0 {
		t.Fatalf("destroy duration_ms: got %v (type %T)", got[1].payload["duration_ms"], got[1].payload["duration_ms"])
	}
	if got[1].payload["exit_status"] == nil || got[1].payload["exit_status"] == "" {
		t.Fatalf("destroy exit_status missing")
	}
}
