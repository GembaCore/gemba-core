// Package dispatch defines the runtime-strategy seam between Gemba's
// classic native (tmux-backed) dispatch path and the new microVM
// (Firecracker) dispatch path landing in gm-o9t8.3.2.*.
//
// Existing dispatch (cmd/gemba's `dispatch` and the cascade endpoint in
// internal/server/work_items_cascade.go) is unchanged — that code path
// remains the default. Callers that want VM isolation construct a
// FirecrackerRuntime and call DispatchRequest through it; that yields
// a run id that other surfaces (logs, events) can correlate against.
//
// The Runtime selection is a constitution key `runtime` (or the CLI
// flag `--runtime`). Selection logic lives in SelectRuntime — handlers
// take whatever Runtime falls out and drive it the same way.
//
// gm-o9t8.3.2.6.
package dispatch

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/GembaCore/gemba-core/internal/adapter/firecracker"
	"github.com/GembaCore/gemba-core/internal/server/workspacelayout"
)

// RuntimeKind names a strategy. The string form matches the
// constitution key value so we can round-trip cleanly.
type RuntimeKind string

const (
	// RuntimeNative is the existing tmux/native dispatch surface.
	RuntimeNative RuntimeKind = "native"
	// RuntimeFirecracker boots a per-bead microVM via the firecracker
	// supervisor and returns the VM id as the run id.
	RuntimeFirecracker RuntimeKind = "firecracker"
)

// Request is the per-bead dispatch input the runtime needs. Kept
// minimal so neither strategy needs to import the full work-item
// type. The caller (cascade dispatch handler) translates a real
// work item into this shape.
type Request struct {
	// WorkspaceID is the workspace the bead executes in.
	WorkspaceID string
	// BeadID is the runnable leaf being dispatched.
	BeadID string
	// AgentType is the agent kind the native path would spawn (claude,
	// codex, …). The firecracker path forwards this into the guest as
	// a hint via env var.
	AgentType string
	// DataRoot is the gemba-server's data dir (used by workspacelayout
	// to resolve the workspace tree). May be empty when the runtime
	// does not need filesystem hand-off (e.g. native).
	DataRoot string
}

// Result is what every runtime returns from Dispatch.
type Result struct {
	// RunID is the opaque identifier callers correlate against logs,
	// events, and the audit log. For the firecracker runtime this is
	// the VM ID; for native it is whatever the existing path returns.
	RunID string
}

// Runtime is the strategy interface. Implementations MUST be safe for
// concurrent Dispatch calls.
type Runtime interface {
	// Kind reports which strategy this is.
	Kind() RuntimeKind
	// Dispatch starts the bead's execution and returns a run id.
	Dispatch(ctx context.Context, req Request) (Result, error)
}

// nativeDispatcher is the function the native path supplies. Kept as a
// function value rather than another package-level interface so the
// server can wire the existing internal handler without exposing its
// shape to this package.
type NativeDispatchFunc func(ctx context.Context, req Request) (Result, error)

// nativeRuntime wraps the existing tmux dispatch path. We do NOT
// reimplement it here — the server passes in a callback that drives
// whatever it already drives, and this package just hands the call
// through. That preserves the "native path is unchanged" guarantee.
type nativeRuntime struct {
	fn NativeDispatchFunc
}

// NewNativeRuntime returns a Runtime that delegates to fn. fn may be
// nil — in that case Dispatch returns ErrNativeUnconfigured so callers
// can choose to fail-soft.
func NewNativeRuntime(fn NativeDispatchFunc) Runtime {
	return &nativeRuntime{fn: fn}
}

// ErrNativeUnconfigured surfaces when the native runtime is selected
// but no underlying dispatcher was wired.
var ErrNativeUnconfigured = errors.New("dispatch: native runtime not configured")

func (r *nativeRuntime) Kind() RuntimeKind { return RuntimeNative }

func (r *nativeRuntime) Dispatch(ctx context.Context, req Request) (Result, error) {
	if r.fn == nil {
		return Result{}, ErrNativeUnconfigured
	}
	return r.fn(ctx, req)
}

// firecrackerRuntime drives the firecracker.Supervisor to start a
// per-bead microVM. The workspace's repo dir is mounted at /workspace
// in the guest; the vault (already wired into the supervisor) fills
// in boot-time secrets — this code does NOT re-inject them.
type firecrackerRuntime struct {
	sup        firecracker.Supervisor
	kernelPath string
	rootfsPath string
}

// FirecrackerOptions describes how to construct a Firecracker runtime.
// KernelPath/RootfsPath default to empty (dev mode); a production
// caller wires the real artifacts (gm-o9t8.3.2.2).
type FirecrackerOptions struct {
	Supervisor firecracker.Supervisor
	KernelPath string
	RootfsPath string
}

// NewFirecrackerRuntime returns a Runtime that boots a microVM per
// dispatch call. Supervisor is required.
func NewFirecrackerRuntime(opts FirecrackerOptions) (Runtime, error) {
	if opts.Supervisor == nil {
		return nil, errors.New("dispatch: FirecrackerOptions.Supervisor is required")
	}
	return &firecrackerRuntime{
		sup:        opts.Supervisor,
		kernelPath: opts.KernelPath,
		rootfsPath: opts.RootfsPath,
	}, nil
}

func (r *firecrackerRuntime) Kind() RuntimeKind { return RuntimeFirecracker }

func (r *firecrackerRuntime) Dispatch(ctx context.Context, req Request) (Result, error) {
	if req.WorkspaceID == "" {
		return Result{}, errors.New("dispatch: Request.WorkspaceID is required")
	}
	if req.BeadID == "" {
		return Result{}, errors.New("dispatch: Request.BeadID is required")
	}

	spec := firecracker.Spec{
		WorkspaceID: req.WorkspaceID,
		KernelPath:  r.kernelPath,
		RootfsPath:  r.rootfsPath,
	}

	// Resolve the workspace repo dir and mount it into the guest at
	// /workspace. We use Resolve (not EnsureLayout) — dispatch should
	// fail loudly if the workspace tree has not been ratified yet.
	if req.DataRoot != "" {
		paths, err := workspacelayout.Resolve(req.WorkspaceID, req.DataRoot)
		if err != nil {
			return Result{}, fmt.Errorf("dispatch: resolve workspace: %w", err)
		}
		spec.VolumeMounts = []firecracker.VolumeMount{
			{HostPath: paths.Repo, GuestPath: "/workspace", ReadOnly: false},
		}
	}

	// Hand the bead id + agent type to the guest via env. The vault
	// (wired into the supervisor) fills in the rest of the secrets.
	hints := map[string]string{
		"GEMBA_BEAD_ID":    req.BeadID,
		"GEMBA_AGENT_TYPE": req.AgentType,
	}
	// FallbackCommand: a process that keeps the VM alive on non-Linux
	// CI runners (this Spec path is exercised by tests via the
	// fallback supervisor; production Linux boots use rootfs init).
	spec.FallbackCommand = "exec cat"
	spec.Secrets = hints

	vm, err := r.sup.Start(ctx, spec)
	if err != nil {
		return Result{}, fmt.Errorf("dispatch: firecracker.Start: %w", err)
	}
	return Result{RunID: vm.ID}, nil
}

// SelectRuntime resolves the user-supplied runtime name (from a
// constitution key or CLI flag) into a Runtime. Unknown / empty values
// default to native — the migration is opt-in. The native and fc args
// may be nil; nil + selection of that kind returns an error.
func SelectRuntime(name string, native Runtime, fc Runtime) (Runtime, error) {
	switch RuntimeKind(strings.ToLower(strings.TrimSpace(name))) {
	case RuntimeFirecracker:
		if fc == nil {
			return nil, errors.New("dispatch: runtime=firecracker requested but not wired")
		}
		return fc, nil
	case RuntimeNative, "":
		if native == nil {
			return nil, errors.New("dispatch: runtime=native requested but not wired")
		}
		return native, nil
	default:
		return nil, fmt.Errorf("dispatch: unknown runtime %q", name)
	}
}
