package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MikeBengtson/gemba/internal/config"
	"github.com/MikeBengtson/gemba/internal/core"
	"github.com/MikeBengtson/gemba/internal/transport/api"
	"github.com/MikeBengtson/gemba/internal/transport/testadaptors"
)

// programmableWorkPlane wraps FakeWorkPlane with a GetWorkItem hook so
// the beads handler tests can program the return value without standing
// up a full adaptor. Every other method delegates to the embedded fake.
type programmableWorkPlane struct {
	*testadaptors.FakeWorkPlane
	getFn func(ctx context.Context, id core.WorkItemID) (core.WorkItem, error)
}

func (p *programmableWorkPlane) GetWorkItem(ctx context.Context, id core.WorkItemID) (core.WorkItem, error) {
	return p.getFn(ctx, id)
}

func newProgrammableHost(t *testing.T, fn func(ctx context.Context, id core.WorkItemID) (core.WorkItem, error)) *api.Host {
	t.Helper()
	host := api.New()
	wp := &programmableWorkPlane{
		FakeWorkPlane: testadaptors.NewFakeWorkPlane(core.TransportAPI),
		getFn:         fn,
	}
	if _, err := host.RegisterWorkPlane(context.Background(), wp); err != nil {
		t.Fatalf("RegisterWorkPlane: %v", err)
	}
	return host
}

func TestGetBead_Found_ReturnsFullWorkItem(t *testing.T) {
	want := core.WorkItem{
		ID:            "gm-foo",
		Kind:          "task",
		Title:         "test bead",
		Status:        "open",
		StateCategory: core.StateBacklog,
		Relationships: []core.Relationship{
			{Kind: core.RelBlocks, From: "gm-foo", To: "gm-bar"},
			{Kind: core.RelParentChild, From: "gm-parent", To: "gm-foo"},
		},
		Evidence: []core.Evidence{
			{ID: "ev-1", Kind: core.EvidenceCommit, Source: "git", Ref: "abc123", CapturedAt: time.Unix(1700000000, 0).UTC()},
		},
		Custom: map[string]any{
			"beads:dependencies": []any{"gm-other"},
		},
		CreatedAt: time.Unix(1700000000, 0).UTC(),
		UpdatedAt: time.Unix(1700000000, 0).UTC(),
	}

	host := newProgrammableHost(t, func(_ context.Context, id core.WorkItemID) (core.WorkItem, error) {
		if id != "gm-foo" {
			return core.WorkItem{}, core.ErrNotFound
		}
		return want, nil
	})
	h := NewRouter(config.ServeConfig{}, fakeSPA(), host)

	req := httptest.NewRequest(http.MethodGet, "/api/beads/gm-foo", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d; body=%q", rec.Code, rec.Body.String())
	}
	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Fatalf("want application/json, got %q", ct)
	}
	var got core.WorkItem
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v; body=%q", err, rec.Body.String())
	}
	if got.ID != want.ID {
		t.Fatalf("ID: want %q, got %q", want.ID, got.ID)
	}
	if len(got.Relationships) != 2 {
		t.Fatalf("want 2 relationships, got %d", len(got.Relationships))
	}
	if len(got.Evidence) != 1 {
		t.Fatalf("want 1 evidence, got %d", len(got.Evidence))
	}
	if _, ok := got.Custom["beads:dependencies"]; !ok {
		t.Fatalf("missing Custom[beads:dependencies]; got %v", got.Custom)
	}
}

func TestGetBead_NotFound_Returns404Envelope(t *testing.T) {
	host := newProgrammableHost(t, func(_ context.Context, _ core.WorkItemID) (core.WorkItem, error) {
		return core.WorkItem{}, core.ErrNotFound
	})
	h := NewRouter(config.ServeConfig{}, fakeSPA(), host)

	req := httptest.NewRequest(http.MethodGet, "/api/beads/gm-missing", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d; body=%q", rec.Code, rec.Body.String())
	}
	var env map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v; body=%q", err, rec.Body.String())
	}
	if env["error"] != "session_not_found" {
		t.Fatalf("want error=session_not_found, got %q", env["error"])
	}
	if env["message"] != "bead gm-missing not found" {
		t.Fatalf("want specific message, got %q", env["message"])
	}
}

// Wrapped ErrNotFound (errors.Is still holds) must also hit the 404 path.
// Adaptors regularly wrap with fmt.Errorf("…: %w", core.ErrNotFound).
func TestGetBead_WrappedNotFound_Returns404(t *testing.T) {
	host := newProgrammableHost(t, func(_ context.Context, id core.WorkItemID) (core.WorkItem, error) {
		return core.WorkItem{}, fmt.Errorf("bd: lookup %s: %w", id, core.ErrNotFound)
	})
	h := NewRouter(config.ServeConfig{}, fakeSPA(), host)

	req := httptest.NewRequest(http.MethodGet, "/api/beads/gm-x", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404 on wrapped ErrNotFound, got %d; body=%q", rec.Code, rec.Body.String())
	}
}

// Adaptor-tagged degraded error surfaces as 503 via httperr.WriteError.
// Conformance Group F requires adaptors to return tagged *AdaptorError;
// the shared mapper picks up the kind and emits the canonical envelope.
func TestGetBead_AdaptorDegraded_Returns503(t *testing.T) {
	host := newProgrammableHost(t, func(_ context.Context, _ core.WorkItemID) (core.WorkItem, error) {
		return core.WorkItem{}, core.NewAdaptorError(core.KindAdaptorDegraded,
			"bd probe timed out; Dolt may be hung")
	})
	h := NewRouter(config.ServeConfig{}, fakeSPA(), host)

	req := httptest.NewRequest(http.MethodGet, "/api/beads/gm-foo", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d; body=%q", rec.Code, rec.Body.String())
	}
	var env map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v; body=%q", err, rec.Body.String())
	}
	if env["error"] != "adaptor_degraded" {
		t.Fatalf("want error=adaptor_degraded, got %v", env["error"])
	}
	if env["message"] != "bd probe timed out; Dolt may be hung" {
		t.Fatalf("want original message, got %v", env["message"])
	}
}

// An untagged error from an adaptor violates Conformance Group F and
// surfaces as 500 internal — the mapper's signal that the adaptor is
// non-conformant rather than pretending to know what went wrong.
func TestGetBead_UntaggedError_Returns500(t *testing.T) {
	host := newProgrammableHost(t, func(_ context.Context, _ core.WorkItemID) (core.WorkItem, error) {
		return core.WorkItem{}, errors.New("boom")
	})
	h := NewRouter(config.ServeConfig{}, fakeSPA(), host)

	req := httptest.NewRequest(http.MethodGet, "/api/beads/gm-foo", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d; body=%q", rec.Code, rec.Body.String())
	}
	var env map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v; body=%q", err, rec.Body.String())
	}
	if env["error"] != "internal" {
		t.Fatalf("want error=internal, got %v", env["error"])
	}
}

// With no host wired, the handler must report adaptor_not_configured (503)
// rather than panic or hang. Matches the requireHealthyAdaptor envelope.
func TestGetBead_NoHost_Returns503(t *testing.T) {
	h := NewRouter(config.ServeConfig{}, fakeSPA(), nil)

	req := httptest.NewRequest(http.MethodGet, "/api/beads/gm-foo", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d; body=%q", rec.Code, rec.Body.String())
	}
	var env map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v; body=%q", err, rec.Body.String())
	}
	if env["error"] != "adaptor_not_configured" {
		t.Fatalf("want error=adaptor_not_configured, got %v", env["error"])
	}
}

// Static sibling routes must still win over the {id} param. /beads/ready
// is mounted above the wildcard — chi prefers the literal — so it keeps
// returning the 501 stub until M1.x replaces it, not 404-via-handler.
func TestGetBead_StaticSiblingRoutesWin(t *testing.T) {
	host := newProgrammableHost(t, func(_ context.Context, id core.WorkItemID) (core.WorkItem, error) {
		t.Fatalf("getBead should not be called for /beads/ready; got id=%q", id)
		return core.WorkItem{}, nil
	})
	h := NewRouter(config.ServeConfig{}, fakeSPA(), host)

	req := httptest.NewRequest(http.MethodGet, "/api/beads/ready", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("/api/beads/ready: want 501 (stub), got %d; body=%q", rec.Code, rec.Body.String())
	}
}
