package api

import (
	"context"
	"fmt"
	"sync"

	"github.com/MikeBengtson/gemba/internal/core"
	"github.com/MikeBengtson/gemba/internal/transport"
)

// Host is the HTTP+JSON adaptor host. It binds in-process adaptors to the
// `api` transport vocabulary and performs version negotiation on every
// registration.
//
// The wire-level handlers (mux, routing, auth) land in gm-e4.x; this host
// is the registration-and-describe layer those handlers will dispatch
// through. Keeping the two phases separate means a new adaptor can prove
// it speaks the contract without waiting on the HTTP server.
type Host struct {
	mu            sync.RWMutex
	work          core.WorkPlane
	orchestration core.OrchestrationPlaneAdaptor
	workReg       *transport.Registration
	orchReg       *transport.Registration
}

// New returns an empty api Host.
func New() *Host { return &Host{} }

// Transport reports the wire this host serves.
func (h *Host) Transport() core.Transport { return core.TransportAPI }

// RegisterWorkPlane binds a WorkPlane adaptor to this host. Calls
// adaptor.Describe, validates the manifest, and runs version +
// transport-kind negotiation before accepting the binding.
//
// Re-registration replaces the previous adaptor; operators occasionally
// rebind in tests and in `gemba adaptor register --replace` flows.
func (h *Host) RegisterWorkPlane(ctx context.Context, a core.WorkPlane) (transport.Registration, error) {
	if a == nil {
		return transport.Registration{}, fmt.Errorf("api: WorkPlane is nil")
	}
	m, err := a.Describe(ctx)
	if err != nil {
		return transport.Registration{}, fmt.Errorf("api: describe: %w", err)
	}
	if err := m.Validate(); err != nil {
		return transport.Registration{}, fmt.Errorf("api: manifest: %w", err)
	}
	if err := transport.Negotiate(m.AdaptorName, m.AdaptorVersion, m.ProtocolVersion, m.Transport, h.Transport()); err != nil {
		return transport.Registration{}, err
	}
	barOK, barReasons := m.MinimumBar()
	reg := transport.Registration{
		Plane:                    transport.PlaneWork,
		Transport:                h.Transport(),
		AdaptorName:              m.AdaptorName,
		AdaptorVersion:           m.AdaptorVersion,
		ProtocolVersion:          m.ProtocolVersion,
		CoreVersion:              core.ProtocolVersion,
		ReducedCapability:        !barOK,
		ReducedCapabilityReasons: barReasons,
	}
	h.mu.Lock()
	h.work = a
	h.workReg = &reg
	h.mu.Unlock()
	return reg, nil
}

// RegisterOrchestrationPlane binds an OrchestrationPlaneAdaptor. Same
// contract as RegisterWorkPlane, but consults the orchestration manifest.
// Single-slot invariant (gm-native.1): only one OrchestrationPlaneAdaptor
// may be registered at a time. A second Register call while one is
// already bound returns a typed core.AdaptorError{KindValidation} —
// routing and event fan-out assume a single plane.
func (h *Host) RegisterOrchestrationPlane(_ context.Context, a core.OrchestrationPlaneAdaptor) (transport.Registration, error) {
	if a == nil {
		return transport.Registration{}, fmt.Errorf("api: OrchestrationPlaneAdaptor is nil")
	}
	h.mu.Lock()
	if h.orchestration != nil {
		prior := h.orchReg
		h.mu.Unlock()
		return transport.Registration{}, core.NewAdaptorError(core.KindValidation,
			"api: orchestration plane already registered (adaptor=%q version=%q)",
			prior.AdaptorName, prior.AdaptorVersion)
	}
	h.mu.Unlock()
	m := a.Describe()
	if err := transport.Negotiate(m.AdaptorID, m.AdaptorVersion, m.OrchestrationAPIVersion, m.Transport, h.Transport()); err != nil {
		return transport.Registration{}, err
	}
	reg := transport.Registration{
		Plane:           transport.PlaneOrchestration,
		Transport:       h.Transport(),
		AdaptorName:     m.AdaptorID,
		AdaptorVersion:  m.AdaptorVersion,
		ProtocolVersion: m.OrchestrationAPIVersion,
		CoreVersion:     core.ProtocolVersion,
	}
	h.mu.Lock()
	// Re-check under the write lock in case two goroutines raced past
	// the initial non-nil check.
	if h.orchestration != nil {
		prior := h.orchReg
		h.mu.Unlock()
		return transport.Registration{}, core.NewAdaptorError(core.KindValidation,
			"api: orchestration plane already registered (adaptor=%q version=%q)",
			prior.AdaptorName, prior.AdaptorVersion)
	}
	h.orchestration = a
	h.orchReg = &reg
	h.mu.Unlock()
	return reg, nil
}

// WorkPlane returns the bound WorkPlane (or nil). Used by server handlers.
func (h *Host) WorkPlane() core.WorkPlane {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.work
}

// OrchestrationPlane returns the bound OrchestrationPlaneAdaptor (or nil).
func (h *Host) OrchestrationPlane() core.OrchestrationPlaneAdaptor {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.orchestration
}

// Registrations returns the current plane bindings for display / doctor.
func (h *Host) Registrations() []transport.Registration {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := []transport.Registration{}
	if h.workReg != nil {
		out = append(out, *h.workReg)
	}
	if h.orchReg != nil {
		out = append(out, *h.orchReg)
	}
	return out
}
