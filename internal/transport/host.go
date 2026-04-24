// Package transport hosts the three wire shims (api, jsonl, mcp) that
// bind adaptors to the core. This file defines the cross-transport
// registration contract — a single Registration shape plus two
// structured error types that the per-transport hosts in api/, jsonl/,
// and mcp/ all share.
//
// Resolves DD-12 (transport plurality) and DD-15 (version negotiation).
package transport

import (
	"fmt"

	"github.com/MikeBengtson/gemba/internal/core"
)

// Plane names which half of the two-plane architecture (gm-root DD-1) a
// registration targets. Gemba requires exactly one of each to serve.
type Plane string

const (
	PlaneWork          Plane = "work"
	PlaneOrchestration Plane = "orchestration"
)

// Registration is the record a transport host returns after successfully
// binding an adaptor. It is JSON-serialisable so `gemba adaptor register`
// can print a machine-readable confirmation and so the doctor + /api
// surfaces can show the active binding.
type Registration struct {
	Plane           Plane          `json:"plane"`
	Transport       core.Transport `json:"transport"`
	AdaptorName     string         `json:"adaptor_name"`
	AdaptorVersion  string         `json:"adaptor_version"`
	ProtocolVersion string         `json:"protocol_version"`
	CoreVersion     string         `json:"core_version"`
	// ReducedCapability reports that the adaptor registered successfully
	// but failed CapabilityManifest.MinimumBar (gm-ekr). Callers that
	// refuse to bind below-bar adaptors (high-autonomy orchestrators
	// like Gastown / Metaswarm) inspect this flag and bail; the SPA
	// and doctor inspect it to show a "below-bar" badge.
	//
	// OrchestrationPlane registrations never set this — the bar is a
	// WorkPlane invariant today.
	ReducedCapability bool `json:"reduced_capability,omitempty"`
	// ReducedCapabilityReasons carries the human-readable list returned
	// by MinimumBar so doctor / `gemba adaptor register` can show why
	// without re-running the check. Empty when ReducedCapability is
	// false.
	ReducedCapabilityReasons []string `json:"reduced_capability_reasons,omitempty"`
}

// VersionMismatchError is returned when an adaptor's declared protocol
// version disagrees with the core's ProtocolVersion. The message is
// intentionally verbose and actionable — operators see it verbatim from
// `gemba adaptor register` and from doctor output, with no surrounding
// wrapping.
type VersionMismatchError struct {
	AdaptorName     string
	AdaptorVersion  string
	AdaptorProtocol string
	CoreProtocol    string
}

func (e *VersionMismatchError) Error() string {
	return fmt.Sprintf(
		"adaptor %q (version %s) speaks protocol_version=%s but core advertises core_version=%s — "+
			"upgrade the adaptor or pin this gemba build to a compatible version",
		e.AdaptorName, e.AdaptorVersion, e.AdaptorProtocol, e.CoreProtocol,
	)
}

// TransportMismatchError is returned when an adaptor's declared transport
// doesn't match the host it was handed to (e.g. passing an api-transport
// adaptor to the jsonl host). Each host enforces this because a single
// adaptor MUST declare exactly one transport (gm-root DD-12).
type TransportMismatchError struct {
	AdaptorName       string
	HostTransport     core.Transport
	DeclaredTransport core.Transport
}

func (e *TransportMismatchError) Error() string {
	return fmt.Sprintf(
		"adaptor %q declares transport=%s but was registered on the %s host — "+
			"each adaptor must be bound to exactly one transport (gm-root DD-12)",
		e.AdaptorName, e.DeclaredTransport, e.HostTransport,
	)
}

// Negotiate is the shared check every transport host calls after pulling
// a manifest from Describe. It rejects:
//
//   - empty adaptor name (prevents anonymous adaptors polluting the UI),
//   - protocol-version mismatch against core.ProtocolVersion,
//   - transport-kind mismatch against the host's own transport.
//
// Any other structural validation (state map coverage, transport
// enum validity) lives on the manifest's own Validate() and is the
// host's responsibility to call first.
func Negotiate(name, version, protocol string, declared, host core.Transport) error {
	if name == "" {
		return fmt.Errorf("transport: adaptor name is required")
	}
	if protocol != core.ProtocolVersion {
		return &VersionMismatchError{
			AdaptorName:     name,
			AdaptorVersion:  version,
			AdaptorProtocol: protocol,
			CoreProtocol:    core.ProtocolVersion,
		}
	}
	if declared != host {
		return &TransportMismatchError{
			AdaptorName:       name,
			HostTransport:     host,
			DeclaredTransport: declared,
		}
	}
	return nil
}
