package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/config"
	"github.com/GembaCore/gemba-core/internal/transport/api"
	"github.com/GembaCore/gemba-core/internal/transport/testadaptors"
)

func TestEnsureInteraction_EnrichesWorkItemAndRoutesGasTownCrew(t *testing.T) {
	host := api.New()
	wp := testadaptors.NewFakeWorkPlane(core.TransportAPI)
	wp.GetFn = func(_ context.Context, id core.WorkItemID) (core.WorkItem, error) {
		if id != "gm-1" {
			t.Fatalf("GetWorkItem id=%q, want gm-1", id)
		}
		return core.WorkItem{
			ID:            "gm-1",
			Kind:          "task",
			Title:         "Implement converter",
			Status:        "open",
			StateCategory: core.StateBacklog,
			CreatedAt:     time.Now(),
			UpdatedAt:     time.Now(),
			Evidence: []core.Evidence{{
				ID:      "ev-1",
				Kind:    core.EvidenceCommit,
				Ref:     "abc123",
				Summary: "implemented converter",
			}},
		}, nil
	}
	if _, err := host.RegisterWorkPlane(context.Background(), wp); err != nil {
		t.Fatalf("RegisterWorkPlane: %v", err)
	}
	op := testadaptors.NewFakeOrchestrationPlane(core.TransportAPI)
	op.Manifest.AdaptorID = "gastown"
	if _, err := host.RegisterOrchestrationPlane(context.Background(), op); err != nil {
		t.Fatalf("RegisterOrchestrationPlane: %v", err)
	}

	r := NewRouter(config.ServeConfig{}, fakeSPA(), host)
	body := []byte(`{"kind":"pm_consult","scope":{"type":"workitem","id":"gm-1"}}`)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/interactions:ensure", bytes.NewReader(body)))

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got interactionSession
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Scope.Title != "Implement converter" {
		t.Fatalf("scope title=%q", got.Scope.Title)
	}
	if got.RuntimeHost != interactionRuntimeGasTownCrew {
		t.Fatalf("runtime=%q, want %q", got.RuntimeHost, interactionRuntimeGasTownCrew)
	}
	if len(got.Evidence) != 1 || got.Evidence[0].ID != "ev-1" {
		t.Fatalf("evidence=%+v", got.Evidence)
	}
}

func TestEnsureInteraction_RoutesGasTownEpicToMayorAndIsIdempotent(t *testing.T) {
	host := api.New()
	wp := testadaptors.NewFakeWorkPlane(core.TransportAPI)
	wp.GetFn = func(context.Context, core.WorkItemID) (core.WorkItem, error) {
		return core.WorkItem{
			ID:            "gm-e1",
			Kind:          "epic",
			Title:         "Planning epic",
			Status:        "open",
			StateCategory: core.StateBacklog,
			CreatedAt:     time.Now(),
			UpdatedAt:     time.Now(),
		}, nil
	}
	if _, err := host.RegisterWorkPlane(context.Background(), wp); err != nil {
		t.Fatalf("RegisterWorkPlane: %v", err)
	}
	op := testadaptors.NewFakeOrchestrationPlane(core.TransportAPI)
	op.Manifest.AdaptorID = "gastown"
	if _, err := host.RegisterOrchestrationPlane(context.Background(), op); err != nil {
		t.Fatalf("RegisterOrchestrationPlane: %v", err)
	}
	r := NewRouter(config.ServeConfig{}, fakeSPA(), host)
	body := []byte(`{"kind":"pm_consult","scope":{"type":"epic","id":"gm-e1"}}`)

	var first, second interactionSession
	for i, dst := range []*interactionSession{&first, &second} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/interactions:ensure", bytes.NewReader(body)))
		if rec.Code != http.StatusOK {
			t.Fatalf("call %d status=%d body=%s", i+1, rec.Code, rec.Body.String())
		}
		if err := json.Unmarshal(rec.Body.Bytes(), dst); err != nil {
			t.Fatalf("decode call %d: %v", i+1, err)
		}
	}
	if first.RuntimeHost != interactionRuntimeGasTownMayor {
		t.Fatalf("runtime=%q, want mayor", first.RuntimeHost)
	}
	if first.ID != second.ID || !first.CreatedAt.Equal(second.CreatedAt) {
		t.Fatalf("ensure should return stored record; first=%+v second=%+v", first, second)
	}
}

func TestEnsureInteraction_RejectsBadScope(t *testing.T) {
	r := NewRouter(config.ServeConfig{}, fakeSPA(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/interactions:ensure", bytes.NewReader([]byte(`{"scope":{"id":"gm-1"}}`))))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestInteractionTurn_AppendsBootstrapConversation(t *testing.T) {
	r := NewRouter(config.ServeConfig{}, fakeSPA(), nil)
	ensureBody := []byte(`{"kind":"pm_consult","scope":{"type":"bootstrap","id":"001-auth","title":"Login Recovery"}}`)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/interactions:ensure", bytes.NewReader(ensureBody)))
	if rec.Code != http.StatusOK {
		t.Fatalf("ensure status=%d body=%s", rec.Code, rec.Body.String())
	}
	var ensured interactionSession
	if err := json.Unmarshal(rec.Body.Bytes(), &ensured); err != nil {
		t.Fatal(err)
	}

	turnBody := []byte(`{"id":` + strconv.Quote(ensured.ID) + `,"message":"split the export story out"}`)
	turnRec := httptest.NewRecorder()
	r.ServeHTTP(turnRec, httptest.NewRequest(http.MethodPost, "/api/v1/interactions:turn", bytes.NewReader(turnBody)))
	if turnRec.Code != http.StatusOK {
		t.Fatalf("turn status=%d body=%s", turnRec.Code, turnRec.Body.String())
	}
	var turned interactionSession
	if err := json.Unmarshal(turnRec.Body.Bytes(), &turned); err != nil {
		t.Fatal(err)
	}
	if len(turned.Messages) != len(ensured.Messages)+2 {
		t.Fatalf("messages=%d want %d", len(turned.Messages), len(ensured.Messages)+2)
	}
	if got := turned.Messages[len(turned.Messages)-2]; got.Role != "operator" || !strings.Contains(got.Body, "split") {
		t.Fatalf("operator turn=%+v", got)
	}
	if got := turned.Messages[len(turned.Messages)-1]; got.Role != "assistant" || !strings.Contains(got.Body, "batch-shaping") {
		t.Fatalf("assistant reply=%+v", got)
	}
}

func TestEnsureInteraction_SeedsBootstrapWithSpecKitContext(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "specs", "001-auth")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "spec.md"), []byte(`# Feature Specification: Login Recovery

## User Story 1 - Reset password (Priority: P1)

### Acceptance Scenarios

- Recovery email is sent.

## Functional Requirements

- Users can request a one-time recovery link.
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tasks.md"), []byte(`# Tasks

## Phase 3

- [ ] T001 [P] [US1] Create recovery form.
`), 0o644); err != nil {
		t.Fatal(err)
	}

	host := api.New()
	wp := testadaptors.NewFakeWorkPlane(core.TransportAPI)
	wp.ListFn = func(context.Context, core.WorkItemFilter) ([]core.WorkItem, error) {
		return nil, nil
	}
	if _, err := host.RegisterWorkPlane(context.Background(), wp); err != nil {
		t.Fatalf("RegisterWorkPlane: %v", err)
	}
	r := NewRouter(config.ServeConfig{BeadsDir: root}, fakeSPA(), host)
	body := []byte(`{"kind":"pm_consult","scope":{"type":"bootstrap","id":"001-auth"}}`)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/interactions:ensure", bytes.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got interactionSession
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Scope.Title != "Login Recovery" {
		t.Fatalf("scope title=%q", got.Scope.Title)
	}
	system := got.Messages[0].Body
	for _, want := range []string{
		"Provider: Spec Kit",
		"US1 (P1): Reset password",
		"acceptance: Recovery email is sent.",
		"T001 [P] [US1]: Create recovery form.",
		"Plan hash:",
		"Draft Beads tree:",
	} {
		if !strings.Contains(system, want) {
			t.Fatalf("system message missing %q:\n%s", want, system)
		}
	}
	if len(got.QuickReplies) < 4 {
		t.Fatalf("quick replies=%+v", got.QuickReplies)
	}
}
