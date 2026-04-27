package transport

import (
	"errors"
	"strings"
	"testing"

	"github.com/MikeBengtson/gemba/core"
)

func TestNegotiate_OK(t *testing.T) {
	err := Negotiate("noop", "0.1.0", core.ProtocolVersion, core.TransportJSONL, core.TransportJSONL)
	if err != nil {
		t.Fatalf("Negotiate should succeed when protocol and transport match: %v", err)
	}
}

func TestNegotiate_RejectsEmptyName(t *testing.T) {
	err := Negotiate("", "0.1.0", core.ProtocolVersion, core.TransportAPI, core.TransportAPI)
	if err == nil || !strings.Contains(err.Error(), "adaptor name") {
		t.Fatalf("want empty-name error, got %v", err)
	}
}

func TestNegotiate_VersionMismatch(t *testing.T) {
	err := Negotiate("noop", "0.1.0", "99.0.0", core.TransportAPI, core.TransportAPI)
	var vmErr *VersionMismatchError
	if !errors.As(err, &vmErr) {
		t.Fatalf("want *VersionMismatchError, got %T: %v", err, err)
	}
	if vmErr.AdaptorProtocol != "99.0.0" || vmErr.CoreProtocol != core.ProtocolVersion {
		t.Errorf("version fields wrong: %+v", vmErr)
	}
	// Actionable error: must name the adaptor AND both versions AND give a remediation.
	msg := err.Error()
	for _, want := range []string{"noop", "99.0.0", core.ProtocolVersion, "upgrade"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error missing %q: %s", want, msg)
		}
	}
}

func TestNegotiate_TransportMismatch(t *testing.T) {
	err := Negotiate("noop", "0.1.0", core.ProtocolVersion, core.TransportAPI, core.TransportJSONL)
	var tmErr *TransportMismatchError
	if !errors.As(err, &tmErr) {
		t.Fatalf("want *TransportMismatchError, got %T: %v", err, err)
	}
	if tmErr.DeclaredTransport != core.TransportAPI || tmErr.HostTransport != core.TransportJSONL {
		t.Errorf("transport fields wrong: %+v", tmErr)
	}
	if !strings.Contains(err.Error(), "gm-root DD-12") {
		t.Errorf("error should cite DD-12 so operators can find the contract: %s", err.Error())
	}
}
