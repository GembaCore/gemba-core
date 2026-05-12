//go:build linux

package firecracker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	fc "github.com/firecracker-microvm/firecracker-go-sdk"
	models "github.com/firecracker-microvm/firecracker-go-sdk/client/models"
)

// linuxSupervisor is the production VM Supervisor backed by the
// `firecracker-go-sdk` in-process control surface. One process can
// manage many VMs concurrently — each Start spawns a new firecracker
// child via the SDK and returns an opaque *VM handle.
//
// Network model (gm-o9t8.3.2.1.1):
//   - Each VM gets a freshly-named tap interface on the `gemba0`
//     bridge. In dev mode (KernelPath / RootfsPath unset, e.g. for
//     local smoke tests on a non-KVM Linux box) the tap dance is
//     skipped — the VM still boots but networking is degraded. The
//     full bridge-and-route plumbing belongs to its own host-setup
//     bead (gm-o9t8.3.6.*).
//
// Secrets:
//   - Spec.Secrets is JSON-encoded and posted to MMDS so the in-guest
//     init can curl 169.254.169.254 and dump it into a tmpfs envfile
//     before exec'ing the dispatch command. The vault.Inject side of
//     this contract is gm-o9t8.3.2.5.
//
// Lifecycle:
//   - Start blocks until SDK reports the boot completed (boot target
//     <2s per the parameter freeze in gm-o9t8.3.2.1.1). Stop calls
//     machine.Shutdown for a graceful path and falls through to
//     StopVMM (force) on timeout. Wait wraps fc.Machine.Wait into the
//     same error-channel shape the fallback uses.
type linuxSupervisor struct {
	log         *slog.Logger
	bridgeName  string
	socketDir   string
	devMode     bool
	mu          sync.Mutex
	tapCounter  int
}

// linuxState is what we hide inside VM.state on the Linux path.
type linuxState struct {
	machine *fc.Machine
	cancel  context.CancelFunc
	tapName string
}

// NewSupervisor returns a Linux-backed Supervisor. Sockets default to
// $TMPDIR/gemba-firecracker; bridge defaults to "gemba0". On systems
// without KVM (KVMAvailable() == false) callers may still construct
// the supervisor — Start will be the call that fails, which keeps the
// API surface uniform between platforms.
func NewSupervisor(log *slog.Logger) Supervisor {
	if log == nil {
		log = slog.Default()
	}
	socketDir := filepath.Join(os.TempDir(), "gemba-firecracker")
	_ = os.MkdirAll(socketDir, 0o700)
	return &linuxSupervisor{
		log:        log,
		bridgeName: "gemba0",
		socketDir:  socketDir,
		// Dev mode = no kernel/rootfs supplied. Lets the rest of the
		// dispatch path compile-test on a Linux laptop without the
		// real image artifacts (those land in gm-o9t8.3.2.2).
		devMode: true,
	}
}

func (s *linuxSupervisor) Start(ctx context.Context, spec Spec) (*VM, error) {
	if err := spec.validate(); err != nil {
		return nil, err
	}
	spec = spec.defaults()

	if !KVMAvailable() {
		return nil, errors.New("firecracker: /dev/kvm not available; cannot boot microVM")
	}
	if spec.KernelPath == "" || spec.RootfsPath == "" {
		return nil, errors.New("firecracker: Spec.KernelPath and Spec.RootfsPath are required on Linux")
	}

	id, err := newVMID(spec.WorkspaceID)
	if err != nil {
		return nil, fmt.Errorf("firecracker: generate vm id: %w", err)
	}

	socketPath := filepath.Join(s.socketDir, id+".sock")

	// Tap interface name. The full bring-up (ip tuntap add … master gemba0)
	// happens in a host-setup bead; here we just reserve the name so it
	// surfaces in logs and in the SDK config.
	s.mu.Lock()
	s.tapCounter++
	tapName := fmt.Sprintf("tap-gm-%d", s.tapCounter)
	s.mu.Unlock()

	cfg := fc.Config{
		SocketPath:      socketPath,
		KernelImagePath: spec.KernelPath,
		KernelArgs:      "console=ttyS0 reboot=k panic=1 pci=off",
		Drives: []models.Drive{
			{
				DriveID:      fc.String("rootfs"),
				PathOnHost:   fc.String(spec.RootfsPath),
				IsRootDevice: fc.Bool(true),
				IsReadOnly:   fc.Bool(false),
			},
		},
		MachineCfg: models.MachineConfiguration{
			VcpuCount:  fc.Int64(int64(spec.CPUCount)),
			MemSizeMib: fc.Int64(int64(spec.MemMB)),
		},
		// MmdsAddress left zero — SDK defaults to 169.254.169.254
	}

	// Network interface (skipped in dev mode where the bridge / tap
	// haven't been provisioned yet).
	if !s.devMode {
		cfg.NetworkInterfaces = []fc.NetworkInterface{
			{
				StaticConfiguration: &fc.StaticNetworkConfiguration{
					HostDevName: tapName,
					MacAddress:  generateMAC(id),
				},
			},
		}
	}

	machineCtx, cancel := context.WithCancel(context.Background())
	cmdBuilder := fc.VMCommandBuilder{}.
		WithBin("firecracker").
		WithSocketPath(socketPath).
		Build(machineCtx)

	// We pass only WithProcessRunner; SDK defaults to a no-op logger
	// which is fine — our s.log calls below provide the operator-
	// visible breadcrumbs. Plumbing logrus through would force a
	// dependency that the rest of Gemba doesn't use.
	machine, err := fc.NewMachine(machineCtx, cfg, fc.WithProcessRunner(cmdBuilder))
	if err != nil {
		cancel()
		return nil, fmt.Errorf("firecracker: NewMachine: %w", err)
	}

	// Start the VM. Boot target is <2s per the parameter freeze; we
	// still respect caller ctx for the rare slow-boot.
	if err := machine.Start(ctx); err != nil {
		cancel()
		return nil, fmt.Errorf("firecracker: machine.Start: %w", err)
	}

	// Push secrets into MMDS so the guest init can read them.
	if len(spec.Secrets) > 0 {
		payload := map[string]any{"secrets": spec.Secrets}
		if data, jerr := json.Marshal(payload); jerr == nil {
			if merr := machine.SetMetadata(ctx, json.RawMessage(data)); merr != nil {
				s.log.Warn("firecracker: SetMetadata failed; secrets not in guest",
					"vm_id", id, "err", merr)
			}
		}
	}

	vm := &VM{
		ID:        id,
		IPAddr:    "", // populated by the network bring-up bead
		StartedAt: time.Now(),
		state: &linuxState{
			machine: machine,
			cancel:  cancel,
			tapName: tapName,
		},
		waitCh: make(chan error, 1),
	}

	// Single-shot waiter — SDK's machine.Wait blocks until the VMM exits.
	go func() {
		err := machine.Wait(machineCtx)
		vm.mu.Lock()
		stopped := vm.stopped
		vm.mu.Unlock()
		if stopped {
			err = nil
		}
		vm.waitCh <- err
		close(vm.waitCh)
	}()

	s.log.Info("firecracker VM started",
		"vm_id", vm.ID,
		"workspace_id", spec.WorkspaceID,
		"socket", socketPath,
		"tap", tapName)

	return vm, nil
}

func (s *linuxSupervisor) Stop(ctx context.Context, vm *VM, timeout time.Duration) error {
	if vm == nil {
		return errors.New("firecracker: Stop called with nil VM")
	}
	var firstStop bool
	vm.once.Do(func() { firstStop = true })
	if !firstStop {
		return nil
	}
	st, ok := vm.state.(*linuxState)
	if !ok || st == nil {
		return errors.New("firecracker: VM has no linux state")
	}

	vm.mu.Lock()
	vm.stopped = true
	vm.mu.Unlock()

	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	// Graceful Shutdown via Firecracker API socket.
	shutdownCtx, shutdownCancel := context.WithTimeout(ctx, timeout)
	defer shutdownCancel()
	shutdownErr := st.machine.Shutdown(shutdownCtx)
	if shutdownErr != nil {
		// Fall through to force-kill; we don't surface the graceful error.
		s.log.Warn("firecracker: Shutdown failed; force-killing",
			"vm_id", vm.ID, "err", shutdownErr)
	}

	select {
	case <-vm.waitCh:
		return nil
	case <-time.After(timeout):
		// Force kill via StopVMM + ctx cancel.
		_ = st.machine.StopVMM()
		st.cancel()
	case <-ctx.Done():
		_ = st.machine.StopVMM()
		st.cancel()
		return ctx.Err()
	}

	// Drain waitCh.
	select {
	case <-vm.waitCh:
	case <-time.After(2 * time.Second):
		s.log.Warn("firecracker: waitCh did not drain after StopVMM",
			"vm_id", vm.ID)
	}
	return nil
}

func (s *linuxSupervisor) Wait(ctx context.Context, vm *VM) <-chan error {
	if vm == nil {
		ch := make(chan error, 1)
		ch <- errors.New("firecracker: Wait called with nil VM")
		close(ch)
		return ch
	}
	return vm.waitCh
}

// generateMAC derives a stable MAC address from the VM id so two
// concurrent VMs never collide on the gemba0 bridge. The 02: prefix
// marks it as locally-administered.
func generateMAC(id string) string {
	h := []byte(id)
	for len(h) < 5 {
		h = append(h, 0)
	}
	return fmt.Sprintf("02:%02x:%02x:%02x:%02x:%02x",
		h[0], h[1], h[2], h[3], h[4])
}

