package audit_test

import (
	"context"
	"encoding/json"
	"runtime"
	"testing"
	"time"

	"github.com/GembaCore/gemba-core/internal/adapter/firecracker"
	"github.com/GembaCore/gemba-core/internal/server/audit"
)

// TestMemAuditor_RecordsVMLifecycle covers gm-o9t8.3.2.7 end-to-end:
// the firecracker supervisor (fallback) drives the VMLifecycleAdapter
// which writes a KindVMLifecycle record carrying event=vm.spawn into
// MemAuditor. After Stop, vm.destroy lands as a second record with a
// non-empty duration_ms.
func TestMemAuditor_RecordsVMLifecycle(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("fallback-only test")
	}

	mem := audit.NewMemAuditor()
	adapter := audit.NewVMLifecycleAdapter(mem)

	sup := firecracker.NewSupervisorWithOptions(firecracker.Options{Auditor: adapter})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	vm, err := sup.Start(ctx, firecracker.Spec{
		WorkspaceID:     "audit-ws",
		FallbackCommand: "exec cat",
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	time.Sleep(15 * time.Millisecond)
	if err := sup.Stop(ctx, vm, time.Second); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	recs, err := mem.Query(ctx, 0, audit.KindVMLifecycle)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("expected 2 records, got %d", len(recs))
	}

	// Decode payloads to confirm event names.
	var p0 map[string]any
	if err := json.Unmarshal(recs[0].Payload, &p0); err != nil {
		t.Fatalf("decode payload 0: %v", err)
	}
	if p0["event"] != string(audit.EventVMSpawn) {
		t.Fatalf("record 0 event: got %v want %s", p0["event"], audit.EventVMSpawn)
	}
	if recs[0].Workspace != "audit-ws" {
		t.Fatalf("record 0 Workspace: %q", recs[0].Workspace)
	}
	if recs[0].RunID != vm.ID {
		t.Fatalf("record 0 RunID: got %q want %q", recs[0].RunID, vm.ID)
	}

	var p1 map[string]any
	if err := json.Unmarshal(recs[1].Payload, &p1); err != nil {
		t.Fatalf("decode payload 1: %v", err)
	}
	if p1["event"] != string(audit.EventVMDestroy) {
		t.Fatalf("record 1 event: got %v want %s", p1["event"], audit.EventVMDestroy)
	}
	dur, ok := p1["duration_ms"].(float64) // JSON-decoded back as float64
	if !ok || dur <= 0 {
		t.Fatalf("destroy duration_ms: got %v", p1["duration_ms"])
	}
}

// TestEventVMEgressDeniedConstant ensures the egress-denied event
// constant is declared so gm-o9t8.3.6.2 can reference it without a
// new round of audit-package edits.
func TestEventVMEgressDeniedConstant(t *testing.T) {
	if audit.EventVMEgressDenied == "" {
		t.Fatalf("EventVMEgressDenied not declared")
	}
}
