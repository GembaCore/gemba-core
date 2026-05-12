package firecracker

import (
	"context"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// requireKVM gates real-Firecracker tests behind KVMAvailable(). On
// macOS / Windows / KVM-less Linux CI runners we skip; on a Linux box
// with /dev/kvm we'd actually exercise the SDK path. The gating is
// runtime, not build-tag, so the test file compiles on all platforms.
func requireKVM(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("real Firecracker requires Linux")
	}
	if !KVMAvailable() {
		t.Skip("/dev/kvm not accessible; skipping real Firecracker test")
	}
}

// TestFallback_Lifecycle exercises Start -> Wait-for-exit -> Stop on
// the subprocess fallback. It runs on every platform via the
// NewSupervisor constructor — on Linux this is the real path (which
// will fail without KVM, hence requireKVM gating below), elsewhere
// it's the bash -c shim.
//
// We force the fallback shape by skipping when the host is Linux —
// the real implementation needs kernel/rootfs artifacts that aren't
// in this repo yet.
func TestFallback_Lifecycle(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("fallback tests target non-Linux dev/CI; Linux uses the real Firecracker path which needs vmlinux+rootfs artifacts")
	}

	sup := NewSupervisor(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	spec := Spec{
		WorkspaceID:     "test-ws-1",
		FallbackCommand: "exec sleep 30",
	}
	vm, err := sup.Start(ctx, spec)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if vm.ID == "" || !strings.HasPrefix(vm.ID, "ws-test-ws-1-") {
		t.Fatalf("unexpected VM id: %q", vm.ID)
	}
	if vm.StartedAt.IsZero() {
		t.Fatalf("StartedAt unset")
	}

	// Stop should be prompt — sleep 30 won't exit on its own.
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	if err := sup.Stop(stopCtx, vm, 2*time.Second); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Stop is idempotent.
	if err := sup.Stop(stopCtx, vm, 2*time.Second); err != nil {
		t.Fatalf("Stop (second call): %v", err)
	}

	// Wait should yield nil for the clean-Stop case.
	select {
	case waitErr, ok := <-sup.Wait(ctx, vm):
		if !ok {
			// Channel closed without a send — acceptable; the send
			// happened on a previous receive in Stop's drain path.
			break
		}
		if waitErr != nil {
			t.Fatalf("Wait returned non-nil after clean Stop: %v", waitErr)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("Wait did not fire after Stop")
	}
}

// TestFallback_SecretEnvInjection verifies that Spec.Secrets reach
// the subprocess as environment variables. We use `printenv` with a
// known key and capture its exit code via Wait (printenv returns 0
// when the variable is set, 1 otherwise — but bash -c wraps it so we
// instead use `bash -c 'test "$GEMBA_TEST_SECRET" = "topkek"'` which
// exits 0 on match.
func TestFallback_SecretEnvInjection(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("fallback-only test")
	}
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}

	sup := NewSupervisor(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	spec := Spec{
		WorkspaceID: "secret-ws",
		Secrets: map[string]string{
			"GEMBA_TEST_SECRET": "topkek",
		},
		// A self-exiting check: 0 if the secret is set as expected,
		// 1 otherwise. We then assert exit-status via Wait.
		FallbackCommand: `test "$GEMBA_TEST_SECRET" = "topkek"`,
	}
	vm, err := sup.Start(ctx, spec)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// The process exits on its own. Wait should yield nil (exit 0).
	select {
	case waitErr, ok := <-sup.Wait(ctx, vm):
		if ok && waitErr != nil {
			t.Fatalf("subprocess saw wrong secret env: %v", waitErr)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("Wait did not fire; subprocess never exited")
	}

	// Stop after natural exit is still idempotent.
	if err := sup.Stop(ctx, vm, 1*time.Second); err != nil {
		t.Fatalf("Stop after natural exit: %v", err)
	}
}

// TestFallback_ConcurrentVMsDoNotCollide spawns N VMs with the same
// WorkspaceID and asserts the generated IDs are unique. Also asserts
// each Stop is independent — stopping one doesn't tear down the
// others.
func TestFallback_ConcurrentVMsDoNotCollide(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("fallback-only test")
	}

	sup := NewSupervisor(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	const n = 5
	vms := make([]*VM, n)
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			vm, err := sup.Start(ctx, Spec{
				WorkspaceID:     "shared-ws",
				FallbackCommand: "exec sleep 30",
			})
			if err != nil {
				errs <- err
				return
			}
			vms[idx] = vm
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Fatalf("concurrent Start: %v", e)
	}

	seen := make(map[string]struct{})
	for _, vm := range vms {
		if vm == nil {
			t.Fatalf("got nil VM")
		}
		if _, dup := seen[vm.ID]; dup {
			t.Fatalf("duplicate VM id: %s", vm.ID)
		}
		seen[vm.ID] = struct{}{}
	}

	// Stop one; the rest must still be alive (their waitCh has no
	// send yet).
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer stopCancel()
	if err := sup.Stop(stopCtx, vms[0], 2*time.Second); err != nil {
		t.Fatalf("Stop[0]: %v", err)
	}

	for i := 1; i < n; i++ {
		select {
		case <-sup.Wait(ctx, vms[i]):
			t.Fatalf("VM %d exited prematurely after stopping VM 0", i)
		case <-time.After(50 * time.Millisecond):
			// Good — still running.
		}
	}

	// Clean up the rest.
	for i := 1; i < n; i++ {
		if err := sup.Stop(stopCtx, vms[i], 2*time.Second); err != nil {
			t.Fatalf("Stop[%d]: %v", i, err)
		}
	}
}

// TestKVMAvailable_NonLinux is a sanity check that the platform-
// gated helper returns false off-Linux. On Linux the answer depends
// on the host so we can only assert the function doesn't panic.
func TestKVMAvailable(t *testing.T) {
	got := KVMAvailable()
	if runtime.GOOS != "linux" && got {
		t.Fatalf("KVMAvailable() = true on non-Linux platform %s", runtime.GOOS)
	}
}

// TestReal_FirecrackerSmoke is the placeholder for a real-VM test.
// It always gates behind requireKVM, so on every CI runner we have
// today it skips. Wired in here so the real implementation has a
// place to grow into once gm-o9t8.3.2.2 lands the kernel + rootfs.
func TestReal_FirecrackerSmoke(t *testing.T) {
	requireKVM(t)
	t.Skip("real Firecracker smoke pending kernel+rootfs artifacts (gm-o9t8.3.2.2)")
}
