package mcp

import (
	"context"
	"fmt"
	"sync"

	"github.com/MikeBengtson/gemba/core"
	"github.com/MikeBengtson/gemba/internal/transport"
)

// Host is the Model Context Protocol adaptor host. MCP support is
// recommended-not-required per gm-root DD-12: operators who don't speak
// MCP get away with api + jsonl, but adaptors that do declare MCP must
// be accepted here with the same registration semantics.
//
// The MCP wire adapter (Claude Desktop / IDE plugins dispatching into the
// bound adaptor) lands with gm-e3.5; this host delivers registration +
// version negotiation.
type Host struct {
	mu            sync.RWMutex
	work          core.WorkPlane
	orchestration core.OrchestrationPlaneAdaptor
	workReg       *transport.Registration
	orchReg       *transport.Registration
}

// New returns an empty mcp Host.
func New() *Host { return &Host{} }

// Transport reports the wire this host serves.
func (h *Host) Transport() core.Transport { return core.TransportMCP }

// RegisterWorkPlane binds a WorkPlane adaptor.
func (h *Host) RegisterWorkPlane(ctx context.Context, a core.WorkPlane) (transport.Registration, error) {
	if a == nil {
		return transport.Registration{}, fmt.Errorf("mcp: WorkPlane is nil")
	}
	m, err := a.Describe(ctx)
	if err != nil {
		return transport.Registration{}, fmt.Errorf("mcp: describe: %w", err)
	}
	if err := m.Validate(); err != nil {
		return transport.Registration{}, fmt.Errorf("mcp: manifest: %w", err)
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

// RegisterOrchestrationPlane binds an OrchestrationPlaneAdaptor.
func (h *Host) RegisterOrchestrationPlane(_ context.Context, a core.OrchestrationPlaneAdaptor) (transport.Registration, error) {
	if a == nil {
		return transport.Registration{}, fmt.Errorf("mcp: OrchestrationPlaneAdaptor is nil")
	}
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
	h.orchestration = a
	h.orchReg = &reg
	h.mu.Unlock()
	return reg, nil
}

func (h *Host) WorkPlane() core.WorkPlane {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.work
}

func (h *Host) OrchestrationPlane() core.OrchestrationPlaneAdaptor {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.orchestration
}

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
