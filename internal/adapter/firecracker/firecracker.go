// Package firecracker is the VM Supervisor abstraction for Gemba's
// remote workspace dispatch path (gm-o9t8.3.2.3 / gm-o9t8.3.2.8).
//
// On Linux with KVM available, the build-tagged `firecracker_linux.go`
// hooks up `firecracker-go-sdk` to boot a real Firecracker microVM with
// a vmlinux kernel + ext4 rootfs, tap-per-VM networking on the `gemba0`
// bridge, and MMDS for boot-time secret injection.
//
// On non-Linux hosts (macOS/Windows dev/CI), the build-tagged
// `firecracker_fallback.go` provides a subprocess shim that satisfies
// the same lifecycle interface using `bash -c` so the rest of the
// dispatch path compiles and runs without KVM.
//
// The actual kernel + rootfs image artifacts (gm-o9t8.3.2.2), the
// VM-aware dispatch path (gm-o9t8.3.2.6), egress nftables rules
// (gm-o9t8.3.6.2), volume-mount semantics beyond the stub Spec field
// (gm-o9t8.3.2.4), and boot-time vault.Inject integration
// (gm-o9t8.3.2.5) all live in their own beads.
package firecracker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Supervisor is the lifecycle interface for a Firecracker-style VM
// host. It mirrors `internal/adapter/dolt/supervisor.Supervisor` in
// spirit — Start launches, Stop terminates, Wait surfaces termination
// — but is a *factory* for many independent VMs rather than a wrapper
// around a single long-lived child like dolt-sql-server.
type Supervisor interface {
	// Start launches a new VM described by spec. It blocks until the
	// VM is reachable / dispatch-ready or ctx is cancelled. Each call
	// returns a distinct *VM; callers are expected to track their own
	// VM handles.
	Start(ctx context.Context, spec Spec) (*VM, error)

	// Stop terminates a previously-started VM gracefully, then force-
	// kills anything still alive after timeout elapses. Idempotent —
	// calling Stop twice on the same *VM returns nil the second time.
	Stop(ctx context.Context, vm *VM, timeout time.Duration) error

	// Wait returns a channel that emits a single error when the
	// underlying VM process terminates. A clean Stop results in a nil
	// send. Unexpected exits surface the wait error (e.g. exit-status
	// non-zero). The channel is closed after the single send.
	Wait(ctx context.Context, vm *VM) <-chan error
}

// VolumeMount is a stub for virtio-fs mount semantics. The real
// host-path / guest-path / read-only / cache-mode shape lands in
// gm-o9t8.3.2.4; for now the field exists so callers can compile
// against the final Spec shape.
type VolumeMount struct {
	HostPath  string
	GuestPath string
	ReadOnly  bool
}

// Spec describes a single VM to start. WorkspaceID identifies the
// workspace this VM belongs to and feeds into the generated VM ID for
// observability. KernelPath / RootfsPath are the host paths to the
// vmlinux + ext4 image artifacts (gm-o9t8.3.2.2). Secrets are
// injected via MMDS on Linux and via subprocess env on the fallback
// — both paths land them in the guest before the dispatch command
// runs. MemMB and CPUCount default to 512MB / 1 vCPU when zero.
//
// FallbackCommand is the bash -c command the non-Linux fallback uses
// as the VM stand-in. Production Linux callers do not set this. Empty
// on the fallback means "sleep until killed" — i.e. a passive VM that
// outlives a Start/Stop pair but does nothing useful.
type Spec struct {
	WorkspaceID     string
	KernelPath      string
	RootfsPath      string
	VolumeMounts    []VolumeMount
	Secrets         map[string]string
	MemMB           int
	CPUCount        int
	FallbackCommand string
}

// VM is the opaque handle returned by Start. Fields are read-only
// after Start returns; mutation is the Supervisor's job.
//
// state is exported via getter methods so the fallback and Linux
// implementations share the bookkeeping without leaking platform-
// specific fields into the public surface.
type VM struct {
	ID        string
	IPAddr    string
	StartedAt time.Time

	// platform-specific runtime state. Linux: firecracker.Machine +
	// tap interface; fallback: *exec.Cmd + cancel func. Stored as
	// `any` so the interface file has zero platform imports.
	mu      sync.Mutex
	state   any
	stopped bool
	waitCh  chan error
	once    sync.Once
}

// validate enforces the minimal Spec contract shared by every backend.
// gm-o9t8.3.2.4: also validates each VolumeMount — HostPath must exist
// on disk (we check at Start time so subprocesses or microVMs never
// boot with a phantom mount), and GuestPath must be an absolute path
// so it lands at a deterministic location inside the guest.
func (s Spec) validate() error {
	if s.WorkspaceID == "" {
		return errors.New("firecracker: Spec.WorkspaceID is required")
	}
	for i, vm := range s.VolumeMounts {
		if vm.HostPath == "" {
			return fmt.Errorf("firecracker: VolumeMounts[%d].HostPath is required", i)
		}
		if vm.GuestPath == "" {
			return fmt.Errorf("firecracker: VolumeMounts[%d].GuestPath is required", i)
		}
		if !filepath.IsAbs(vm.GuestPath) {
			return fmt.Errorf("firecracker: VolumeMounts[%d].GuestPath %q must be absolute", i, vm.GuestPath)
		}
		if _, err := os.Stat(vm.HostPath); err != nil {
			return fmt.Errorf("firecracker: VolumeMounts[%d].HostPath %q: %w", i, vm.HostPath, err)
		}
	}
	return nil
}

// defaults fills zero-valued resource fields with the package
// defaults. Returned by value so callers don't see their input
// mutated.
func (s Spec) defaults() Spec {
	if s.MemMB == 0 {
		s.MemMB = 512
	}
	if s.CPUCount == 0 {
		s.CPUCount = 1
	}
	return s
}

// errAlreadyStopped is returned by the internal stop path when a VM
// has already been torn down. Promoted to a no-op nil return at the
// public Supervisor.Stop boundary so callers can stop idempotently.
var errAlreadyStopped = errors.New("firecracker: vm already stopped")

// newVMID generates a per-VM identifier of the shape
// "ws-<workspace>-<8hex>". Uses crypto/rand so concurrent VMs from
// the same workspace don't collide.
func newVMID(workspaceID string) (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf("ws-%s-%s", workspaceID, hex.EncodeToString(b[:])), nil
}
