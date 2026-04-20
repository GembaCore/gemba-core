package api

import (
	"context"
	"errors"
	"testing"

	"github.com/MikeBengtson/gemba/internal/core"
	"github.com/MikeBengtson/gemba/internal/transport"
	"github.com/MikeBengtson/gemba/internal/transport/testadaptors"
)

func TestHost_RegisterWorkPlane_OK(t *testing.T) {
	h := New()
	a := testadaptors.NewFakeWorkPlane(core.TransportAPI)
	reg, err := h.RegisterWorkPlane(context.Background(), a)
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if reg.Plane != transport.PlaneWork || reg.Transport != core.TransportAPI {
		t.Errorf("bad registration: %+v", reg)
	}
	if reg.CoreVersion != core.ProtocolVersion {
		t.Errorf("want CoreVersion=%s, got %s", core.ProtocolVersion, reg.CoreVersion)
	}
	if h.WorkPlane() == nil {
		t.Error("host must retain bound WorkPlane")
	}
}

func TestHost_RegisterWorkPlane_VersionMismatch(t *testing.T) {
	h := New()
	a := testadaptors.NewFakeWorkPlane(core.TransportAPI)
	a.Manifest.ProtocolVersion = "99.0.0"

	_, err := h.RegisterWorkPlane(context.Background(), a)
	var vmErr *transport.VersionMismatchError
	if !errors.As(err, &vmErr) {
		t.Fatalf("want VersionMismatchError, got %T: %v", err, err)
	}
	if h.WorkPlane() != nil {
		t.Error("failed registration must not bind the adaptor")
	}
}

func TestHost_RegisterWorkPlane_TransportMismatch(t *testing.T) {
	h := New()
	// Adaptor declares jsonl but is being registered on the api host.
	a := testadaptors.NewFakeWorkPlane(core.TransportJSONL)
	_, err := h.RegisterWorkPlane(context.Background(), a)
	var tmErr *transport.TransportMismatchError
	if !errors.As(err, &tmErr) {
		t.Fatalf("want TransportMismatchError, got %T: %v", err, err)
	}
}

func TestHost_RegisterWorkPlane_RejectsNil(t *testing.T) {
	h := New()
	_, err := h.RegisterWorkPlane(context.Background(), nil)
	if err == nil {
		t.Fatal("want error on nil adaptor")
	}
}

func TestHost_RegisterOrchestrationPlane_OK(t *testing.T) {
	h := New()
	a := testadaptors.NewFakeOrchestrationPlane(core.TransportAPI)
	reg, err := h.RegisterOrchestrationPlane(context.Background(), a)
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if reg.Plane != transport.PlaneOrchestration || reg.AdaptorName != "fake-orch" {
		t.Errorf("bad registration: %+v", reg)
	}
}

func TestHost_RegisterOrchestrationPlane_VersionMismatch(t *testing.T) {
	h := New()
	a := testadaptors.NewFakeOrchestrationPlane(core.TransportAPI)
	a.Manifest.OrchestrationAPIVersion = "0.0.1"

	_, err := h.RegisterOrchestrationPlane(context.Background(), a)
	var vmErr *transport.VersionMismatchError
	if !errors.As(err, &vmErr) {
		t.Fatalf("want VersionMismatchError, got %T: %v", err, err)
	}
}

func TestHost_Registrations(t *testing.T) {
	h := New()
	if got := h.Registrations(); len(got) != 0 {
		t.Errorf("empty host should report no registrations, got %d", len(got))
	}
	if _, err := h.RegisterWorkPlane(context.Background(), testadaptors.NewFakeWorkPlane(core.TransportAPI)); err != nil {
		t.Fatal(err)
	}
	if _, err := h.RegisterOrchestrationPlane(context.Background(), testadaptors.NewFakeOrchestrationPlane(core.TransportAPI)); err != nil {
		t.Fatal(err)
	}
	if got := h.Registrations(); len(got) != 2 {
		t.Errorf("want 2 registrations, got %d", len(got))
	}
}
