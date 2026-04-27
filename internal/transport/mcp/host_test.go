package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/MikeBengtson/gemba/core"
	"github.com/MikeBengtson/gemba/internal/transport"
	"github.com/MikeBengtson/gemba/internal/transport/testadaptors"
)

func TestHost_RegisterWorkPlane_OK(t *testing.T) {
	h := New()
	a := testadaptors.NewFakeWorkPlane(core.TransportMCP)
	reg, err := h.RegisterWorkPlane(context.Background(), a)
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if reg.Transport != core.TransportMCP {
		t.Errorf("want TransportMCP, got %s", reg.Transport)
	}
}

func TestHost_RegisterWorkPlane_VersionMismatch(t *testing.T) {
	h := New()
	a := testadaptors.NewFakeWorkPlane(core.TransportMCP)
	a.Manifest.ProtocolVersion = "2.0.0"

	_, err := h.RegisterWorkPlane(context.Background(), a)
	var vmErr *transport.VersionMismatchError
	if !errors.As(err, &vmErr) {
		t.Fatalf("want VersionMismatchError, got %T: %v", err, err)
	}
}

func TestHost_RegisterWorkPlane_TransportMismatch(t *testing.T) {
	h := New()
	a := testadaptors.NewFakeWorkPlane(core.TransportAPI)
	_, err := h.RegisterWorkPlane(context.Background(), a)
	var tmErr *transport.TransportMismatchError
	if !errors.As(err, &tmErr) {
		t.Fatalf("want TransportMismatchError, got %T: %v", err, err)
	}
}

func TestHost_RegisterOrchestrationPlane_OK(t *testing.T) {
	h := New()
	a := testadaptors.NewFakeOrchestrationPlane(core.TransportMCP)
	reg, err := h.RegisterOrchestrationPlane(context.Background(), a)
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if reg.Plane != transport.PlaneOrchestration {
		t.Errorf("bad plane: %s", reg.Plane)
	}
}
