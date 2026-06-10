package dispatch

import (
	"context"
	"runtime"
	"strings"
	"testing"

	"github.com/GembaCore/gemba-core/internal/adapter/firecracker"
)

// TestSelectRuntime_DefaultIsNative covers gm-o9t8.3.2.6 — an empty
// runtime key falls back to native so existing dispatch is unaffected.
func TestSelectRuntime_DefaultIsNative(t *testing.T) {
	native := NewNativeRuntime(func(_ context.Context, _ Request) (Result, error) {
		return Result{RunID: "native-run"}, nil
	})

	rt, err := SelectRuntime("", native, nil)
	if err != nil {
		t.Fatalf("SelectRuntime(\"\"): %v", err)
	}
	if rt.Kind() != RuntimeNative {
		t.Fatalf("default Kind: got %s want native", rt.Kind())
	}
	res, err := rt.Dispatch(context.Background(), Request{BeadID: "b", WorkspaceID: "w"})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if res.RunID != "native-run" {
		t.Fatalf("RunID: got %q", res.RunID)
	}
}

// TestSelectRuntime_FirecrackerWhenConfigured covers the explicit
// strategy switch: runtime=firecracker boots a VM and returns its id
// as the run id.
func TestSelectRuntime_FirecrackerWhenConfigured(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("fallback-only test; Linux needs vmlinux+rootfs artifacts")
	}

	sup := firecracker.NewSupervisor(nil)
	fc, err := NewFirecrackerRuntime(FirecrackerOptions{Supervisor: sup})
	if err != nil {
		t.Fatalf("NewFirecrackerRuntime: %v", err)
	}
	native := NewNativeRuntime(func(_ context.Context, _ Request) (Result, error) {
		t.Fatalf("native dispatcher should not be called when runtime=firecracker")
		return Result{}, nil
	})

	rt, err := SelectRuntime("firecracker", native, fc)
	if err != nil {
		t.Fatalf("SelectRuntime: %v", err)
	}
	if rt.Kind() != RuntimeFirecracker {
		t.Fatalf("Kind: got %s want firecracker", rt.Kind())
	}

	res, err := rt.Dispatch(context.Background(), Request{
		WorkspaceID: "fcws",
		BeadID:      "gm-x.1",
		AgentType:   "claude",
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !strings.HasPrefix(res.RunID, "ws-fcws-") {
		t.Fatalf("RunID %q does not look like a VM id", res.RunID)
	}
}

// TestSelectRuntime_UnknownRejected covers the typo path — a bad
// runtime key surfaces as a clear error rather than silently
// defaulting.
func TestSelectRuntime_UnknownRejected(t *testing.T) {
	if _, err := SelectRuntime("zzz", nil, nil); err == nil {
		t.Fatalf("expected error for unknown runtime")
	}
}

// TestSelectRuntime_FirecrackerMissingWired covers the misconfig path:
// runtime=firecracker requested but no fc runtime supplied.
func TestSelectRuntime_FirecrackerMissingWired(t *testing.T) {
	if _, err := SelectRuntime("firecracker", nil, nil); err == nil {
		t.Fatalf("expected error when fc runtime missing")
	}
}
