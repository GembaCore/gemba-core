package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/MikeBengtson/gemba/internal/config"
	"github.com/MikeBengtson/gemba/internal/core"
	"github.com/MikeBengtson/gemba/internal/transport/api"
	"github.com/MikeBengtson/gemba/internal/transport/testadaptors"
)

func newProgrammableHost(t *testing.T, fn func(ctx context.Context, id core.WorkItemID) (core.WorkItem, error)) *api.Host {
	t.Helper()
	return newProgrammableHostFull(t, fn, nil)
}

func newProgrammableHostFull(
	t *testing.T,
	getFn func(ctx context.Context, id core.WorkItemID) (core.WorkItem, error),
	listFn func(ctx context.Context, filter core.WorkItemFilter) ([]core.WorkItem, error),
) *api.Host {
	t.Helper()
	host := api.New()
	wp := testadaptors.NewFakeWorkPlane(core.TransportAPI)
	wp.GetFn = getFn
	wp.ListFn = listFn
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

// ============================================================================
// GET /api/beads — list handler (gm-peg)
// ============================================================================

// Envelope type for the list handler. Kept local so the wire shape stays
// pinned in tests rather than being reimported from a shared package.
type listBeadsEnvelope struct {
	Items []core.WorkItem `json:"items"`
	Total int             `json:"total"`
}

func TestListBeads_HappyPath_ReturnsEnvelope(t *testing.T) {
	items := []core.WorkItem{
		{ID: "gm-1", Kind: "task", Title: "first", Status: "open", StateCategory: core.StateBacklog},
		{ID: "gm-2", Kind: "task", Title: "second", Status: "in_progress", StateCategory: core.StateStarted},
	}
	var gotFilter core.WorkItemFilter
	host := newProgrammableHostFull(t, nil,
		func(_ context.Context, filter core.WorkItemFilter) ([]core.WorkItem, error) {
			gotFilter = filter
			return items, nil
		})
	h := NewRouter(config.ServeConfig{}, fakeSPA(), host)

	req := httptest.NewRequest(http.MethodGet, "/api/beads", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d; body=%q", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("want application/json, got %q", ct)
	}
	var env listBeadsEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v; body=%q", err, rec.Body.String())
	}
	if env.Total != 2 {
		t.Fatalf("total: want 2, got %d", env.Total)
	}
	if len(env.Items) != 2 {
		t.Fatalf("items: want 2, got %d", len(env.Items))
	}
	if env.Items[0].ID != "gm-1" || env.Items[1].ID != "gm-2" {
		t.Fatalf("ids: want [gm-1 gm-2], got [%s %s]", env.Items[0].ID, env.Items[1].ID)
	}
	// M1.3 scope: empty filter only — narrowing and pagination are future work.
	zero := core.WorkItemFilter{}
	if !reflect.DeepEqual(gotFilter, zero) {
		t.Fatalf("handler should pass zero-valued filter, got %+v", gotFilter)
	}
}

// Empty DB → `{items: [], total: 0}`. Must serialise as a JSON array, not null.
func TestListBeads_EmptyDB_ReturnsEmptyArray(t *testing.T) {
	host := newProgrammableHostFull(t, nil,
		func(_ context.Context, _ core.WorkItemFilter) ([]core.WorkItem, error) {
			return nil, nil
		})
	h := NewRouter(config.ServeConfig{}, fakeSPA(), host)

	req := httptest.NewRequest(http.MethodGet, "/api/beads", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d; body=%q", rec.Code, rec.Body.String())
	}
	// Raw body check — env.Items could silently decode null into an empty
	// slice and hide the bug. Pin the wire shape instead.
	body := strings.TrimSpace(rec.Body.String())
	if !strings.Contains(body, `"items":[]`) {
		t.Fatalf("want items:[] in body, got %q", body)
	}
	if !strings.Contains(body, `"total":0`) {
		t.Fatalf("want total:0 in body, got %q", body)
	}
}

// Adaptor-tagged degraded error → 503 via shared mapper.
func TestListBeads_AdaptorDegraded_Returns503(t *testing.T) {
	host := newProgrammableHostFull(t, nil,
		func(_ context.Context, _ core.WorkItemFilter) ([]core.WorkItem, error) {
			return nil, core.NewAdaptorError(core.KindAdaptorDegraded,
				"bd probe timed out; Dolt may be hung")
		})
	h := NewRouter(config.ServeConfig{}, fakeSPA(), host)

	req := httptest.NewRequest(http.MethodGet, "/api/beads", nil)
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

// Untagged error → 500 internal (Conformance Group F: adaptors must tag).
func TestListBeads_UntaggedError_Returns500(t *testing.T) {
	host := newProgrammableHostFull(t, nil,
		func(_ context.Context, _ core.WorkItemFilter) ([]core.WorkItem, error) {
			return nil, errors.New("boom")
		})
	h := NewRouter(config.ServeConfig{}, fakeSPA(), host)

	req := httptest.NewRequest(http.MethodGet, "/api/beads", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d; body=%q", rec.Code, rec.Body.String())
	}
}

// No host wired → 503 adaptor_not_configured. Mirrors the single-bead behaviour
// so the SPA can treat both endpoints with one degraded-state handler.
func TestListBeads_NoHost_Returns503(t *testing.T) {
	h := NewRouter(config.ServeConfig{}, fakeSPA(), nil)

	req := httptest.NewRequest(http.MethodGet, "/api/beads", nil)
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

// ============================================================================
// GET /api/beads/{id} — single-bead handler
// ============================================================================

// Workspace-prefixed ids arrive as percent-encoded slashes on the wire
// (the dolt adaptor emits "gemba/gemba/gm-foo" and the SPA sends it as
// "gemba%2Fgemba%2Fgm-foo"). The handler MUST path-unescape before
// handing the id to the adaptor, otherwise the adaptor sees the raw
// encoded form and reports "not found" against every prefixed lookup.
func TestGetBead_URLEncodedSlashes_DecodedBeforeLookup(t *testing.T) {
	var gotID core.WorkItemID
	host := newProgrammableHost(t, func(_ context.Context, id core.WorkItemID) (core.WorkItem, error) {
		gotID = id
		return core.WorkItem{
			ID:            id,
			Kind:          "task",
			Title:         "prefixed",
			Status:        "open",
			StateCategory: core.StateBacklog,
		}, nil
	})
	h := NewRouter(config.ServeConfig{}, fakeSPA(), host)

	req := httptest.NewRequest(http.MethodGet, "/api/beads/gemba%2Fgemba%2Fgm-foo", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d; body=%q", rec.Code, rec.Body.String())
	}
	if string(gotID) != "gemba/gemba/gm-foo" {
		t.Fatalf("handler handed adaptor encoded id: %q", gotID)
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
