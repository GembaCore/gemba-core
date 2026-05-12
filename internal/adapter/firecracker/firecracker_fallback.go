//go:build !linux

package firecracker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"time"
)

// fallbackSupervisor is the non-Linux dev/CI shim. It pretends to be
// a Firecracker host by spawning a long-running `bash -c` subprocess
// per Spec. The subprocess inherits Spec.Secrets via env so the same
// "secrets reach the guest before the command runs" contract holds.
//
// This lets `internal/cli/agent_dispatch` and the rest of the remote
// dispatch chain compile, run, and be unit-tested on macOS/Windows
// without KVM. The real implementation lives in firecracker_linux.go.
//
// Process-management strategy:
//   - Each VM owns its own context.CancelFunc; Stop cancels first
//     (sending SIGKILL via context to the exec.Cmd is not automatic,
//     so we explicitly Signal SIGTERM then Kill on timeout).
//   - A single per-VM goroutine wraps cmd.Wait, sends the exit error
//     onto a buffered channel, then closes it. Zombie reaping is
//     therefore implicit — Wait is called exactly once per child.
//   - All bookkeeping is guarded by VM.mu; Stop is idempotent via
//     the VM.stopped flag and VM.once.
type fallbackSupervisor struct {
	log *slog.Logger
	mu  sync.Mutex

	// egress is plumbed for source-compatibility with the Linux
	// supervisor: AttachEgress works here too, but Start only logs
	// the plan rather than installing nftables rules (see
	// egress_other.go for rationale).
	egress EgressProvider
}

// AttachEgress is the fallback's pass-through. Stored but used only
// to log the rule plan during Start; nftables is unavailable here.
func (s *fallbackSupervisor) AttachEgress(p EgressProvider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.egress = p
}

// fallbackState is what we store in VM.state on the non-Linux path.
type fallbackState struct {
	cmd    *exec.Cmd
	cancel context.CancelFunc
}

// NewSupervisor returns a Supervisor wired for the current platform.
// On non-Linux this is always the subprocess fallback. Logger may be
// nil — slog.Default() is used in that case.
func NewSupervisor(log *slog.Logger) Supervisor {
	if log == nil {
		log = slog.Default()
	}
	return &fallbackSupervisor{log: log}
}

// Start spawns the dispatch subprocess and returns immediately. There
// is no "wait for SSH" / "wait for MMDS" boot phase on the fallback;
// the equivalent is "process exists and didn't immediately fail."
func (s *fallbackSupervisor) Start(ctx context.Context, spec Spec) (*VM, error) {
	if err := spec.validate(); err != nil {
		return nil, err
	}
	spec = spec.defaults()

	id, err := newVMID(spec.WorkspaceID)
	if err != nil {
		return nil, fmt.Errorf("firecracker: generate vm id: %w", err)
	}

	cmdStr := spec.FallbackCommand
	if cmdStr == "" {
		// Default: an idle process we can Stop without it racing to
		// exit on its own. `sleep infinity` is not portable to macOS
		// — use a read on stdin that never returns, since the parent
		// closes stdin by default.
		cmdStr = "exec cat"
	}

	// Detach from the caller's ctx for the long-running process —
	// Stop is the owner of teardown. If the caller cancels ctx while
	// Start is still mid-spawn we honor it, but once we've returned
	// the VM the only valid termination path is Stop.
	procCtx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(procCtx, "bash", "-c", cmdStr)

	// Inject secrets as env on top of the parent env. We do NOT strip
	// the parent env — dev workflows need PATH, HOME, etc. for the
	// bash -c command to function.
	cmd.Env = os.Environ()
	for k, v := range spec.Secrets {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	// Apply unix process-group isolation when available. The helper
	// lives in fallback_unix.go / fallback_windows.go.
	cmd.SysProcAttr = newFallbackProcAttr()

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("firecracker: start subprocess: %w", err)
	}

	vm := &VM{
		ID:        id,
		IPAddr:    "127.0.0.1",
		StartedAt: time.Now(),
		state: &fallbackState{
			cmd:    cmd,
			cancel: cancel,
		},
		waitCh: make(chan error, 1),
	}

	// Single-shot waiter goroutine. cmd.Wait is called exactly once,
	// preventing zombies. Result lands on vm.waitCh and the channel
	// closes immediately after — readers see the same error every
	// receive thanks to the buffered send.
	go func() {
		err := cmd.Wait()
		// Translate "killed by signal" into nil for the clean-Stop
		// case — Stop sets vm.stopped before signalling, so we can
		// distinguish operator-initiated termination from a crash.
		vm.mu.Lock()
		stopped := vm.stopped
		vm.mu.Unlock()
		if stopped {
			err = nil
		}
		vm.waitCh <- err
		close(vm.waitCh)
	}()

	// Fallback egress: source-compatible with the Linux path. We
	// fetch and translate the rules so dispatch + CLI tooling can
	// inspect the plan, but the actual nftables install is a no-op
	// on non-Linux. See egress_other.go for why.
	s.mu.Lock()
	provider := s.egress
	s.mu.Unlock()
	if provider != nil {
		rules, eerr := provider.Effective(ctx, spec.WorkspaceID)
		if eerr == nil {
			_, _ = applyEgressRules(ctx, s.log, vm.ID, "" /* no tap on fallback */, rules)
		} else {
			s.log.Warn("firecracker fallback: egress lookup failed; proceeding without plan",
				"vm_id", vm.ID, "err", eerr)
		}
	}

	s.log.Info("firecracker fallback VM started",
		"vm_id", vm.ID,
		"workspace_id", spec.WorkspaceID,
		"pid", cmd.Process.Pid)

	return vm, nil
}

// Stop terminates the subprocess. Sends SIGTERM, waits up to timeout
// for a clean exit, then SIGKILLs via cancel(). Idempotent.
func (s *fallbackSupervisor) Stop(ctx context.Context, vm *VM, timeout time.Duration) error {
	if vm == nil {
		return errors.New("firecracker: Stop called with nil VM")
	}
	var firstStop bool
	vm.once.Do(func() { firstStop = true })
	if !firstStop {
		return nil
	}

	st, ok := vm.state.(*fallbackState)
	if !ok || st == nil {
		return errors.New("firecracker: VM has no fallback state")
	}

	vm.mu.Lock()
	vm.stopped = true
	vm.mu.Unlock()

	// Polite SIGTERM first.
	if st.cmd != nil && st.cmd.Process != nil {
		_ = terminateFallback(st.cmd)
	}

	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	select {
	case <-vm.waitCh:
		// Already exited.
		return nil
	case <-time.After(timeout):
		// Force-kill via cancel().
		st.cancel()
	case <-ctx.Done():
		st.cancel()
		return ctx.Err()
	}

	// After cancel() the OS kills the process; drain waitCh so the
	// caller doesn't see a stale "running" view on subsequent Wait.
	select {
	case <-vm.waitCh:
	case <-time.After(2 * time.Second):
		// Give up — the OS should have reaped by now. Log and return
		// without erroring; the goroutine will close waitCh eventually.
		s.log.Warn("firecracker fallback: waitCh did not drain after kill",
			"vm_id", vm.ID)
	}
	return nil
}

// Wait returns the per-VM waitCh. Callers receive a single error
// then a closed channel. Multiple Wait calls on the same VM return
// the same channel — safe to call concurrently.
func (s *fallbackSupervisor) Wait(ctx context.Context, vm *VM) <-chan error {
	if vm == nil {
		ch := make(chan error, 1)
		ch <- errors.New("firecracker: Wait called with nil VM")
		close(ch)
		return ch
	}
	return vm.waitCh
}

// guard against an unused-import lint in the fallback path.
var _ = sync.Mutex{}
