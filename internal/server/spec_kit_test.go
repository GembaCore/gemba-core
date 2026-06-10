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
	"testing"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/config"
	transportapi "github.com/GembaCore/gemba-core/internal/transport/api"
	"github.com/GembaCore/gemba-core/internal/transport/testadaptors"
)

func TestSpecKitListFeatures(t *testing.T) {
	root := seedSpecKitFeature(t)
	r := NewRouter(config.ServeConfig{BeadsDir: root}, fakeSPA(), nil)
	t.Cleanup(r.Close)

	req := httptest.NewRequest(http.MethodGet, "/api/spec-kit/features", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Configured bool `json:"configured"`
		Total      int  `json:"total"`
		Features   []struct {
			ID        string `json:"id"`
			TaskCount int    `json:"task_count"`
		} `json:"features"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Configured || body.Total != 1 || body.Features[0].ID != "001-auth" || body.Features[0].TaskCount != 2 {
		t.Fatalf("body=%#v", body)
	}
}

func TestSpecKitSyncCreatesBeadsHierarchy(t *testing.T) {
	root := seedSpecKitFeature(t)
	wp := testadaptors.NewFakeWorkPlane(core.TransportAPI)
	items := map[core.WorkItemID]core.WorkItem{}
	wp.ListFn = func(_ context.Context, filter core.WorkItemFilter) ([]core.WorkItem, error) {
		var out []core.WorkItem
		for _, item := range items {
			if containsAllLabels(item.Labels, filter.Labels) {
				out = append(out, item)
			}
		}
		return out, nil
	}
	wp.CreateFn = func(_ context.Context, wi core.WorkItem) (core.WorkItem, error) {
		items[wi.ID] = wi
		return wi, nil
	}
	wp.UpdateFn = func(_ context.Context, id core.WorkItemID, patch core.WorkItemPatch) (core.WorkItem, error) {
		wi := items[id]
		if patch.Title != nil {
			wi.Title = *patch.Title
		}
		if patch.Labels != nil {
			wi.Labels = patch.Labels
		}
		items[id] = wi
		return wi, nil
	}
	host := transportapi.New()
	if _, err := host.RegisterWorkPlane(context.Background(), wp); err != nil {
		t.Fatal(err)
	}
	r := NewRouter(config.ServeConfig{BeadsDir: root}, fakeSPA(), host)
	t.Cleanup(r.Close)

	planReq := httptest.NewRequest(http.MethodGet, "/api/spec-kit/features/001-auth/sync-plan", nil)
	planRec := httptest.NewRecorder()
	r.ServeHTTP(planRec, planReq)
	if planRec.Code != http.StatusOK {
		t.Fatalf("plan status=%d body=%s", planRec.Code, planRec.Body.String())
	}
	var plan struct {
		Hash string `json:"hash"`
	}
	if err := json.Unmarshal(planRec.Body.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/spec-kit/features/001-auth/sync-to-beads",
		bytes.NewBufferString(`{"plan_hash":`+strconv.Quote(plan.Hash)+`}`))
	req.Header.Set(ConfirmHeader, "nonce-speckit-sync")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	kinds := map[string]int{}
	for _, item := range items {
		kinds[item.Kind]++
	}
	if kinds[core.KindMilestone] != 1 || kinds["epic"] != 1 || kinds["story"] != 1 || kinds["task"] != 2 {
		t.Fatalf("kinds=%#v", kinds)
	}
}

func TestSpecKitSyncRejectsMissingPlanHash(t *testing.T) {
	root := seedSpecKitFeature(t)
	wp := testadaptors.NewFakeWorkPlane(core.TransportAPI)
	host := transportapi.New()
	if _, err := host.RegisterWorkPlane(context.Background(), wp); err != nil {
		t.Fatal(err)
	}
	r := NewRouter(config.ServeConfig{BeadsDir: root}, fakeSPA(), host)
	t.Cleanup(r.Close)

	req := httptest.NewRequest(http.MethodPost, "/api/spec-kit/features/001-auth/sync-to-beads",
		bytes.NewBufferString(`{}`))
	req.Header.Set(ConfirmHeader, "nonce-speckit-sync-missing-hash")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestSpecKitWorkspaceScaffoldCreateEdit(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".beads"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := NewRouter(config.ServeConfig{ProjectDir: root}, fakeSPA(), nil)
	t.Cleanup(r.Close)

	initReq := httptest.NewRequest(http.MethodPost, "/api/spec-kit/scaffold", nil)
	initReq.Header.Set(ConfirmHeader, "nonce-speckit-scaffold")
	initRec := httptest.NewRecorder()
	r.ServeHTTP(initRec, initReq)
	if initRec.Code != http.StatusOK {
		t.Fatalf("init status=%d body=%s", initRec.Code, initRec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(root, ".specify", "templates", "spec-template.md")); err != nil {
		t.Fatalf("expected scaffold template: %v", err)
	}

	createReq := httptest.NewRequest(http.MethodPost, "/api/spec-kit/features",
		bytes.NewBufferString(`{"title":"Avatar Upload"}`))
	createReq.Header.Set(ConfirmHeader, "nonce-speckit-feature")
	createRec := httptest.NewRecorder()
	r.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createRec.Code, createRec.Body.String())
	}
	var feature struct {
		ID       string `json:"id"`
		HasSpec  bool   `json:"has_spec"`
		HasPlan  bool   `json:"has_plan"`
		HasTasks bool   `json:"has_tasks"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &feature); err != nil {
		t.Fatal(err)
	}
	if feature.ID != "001-avatar-upload" || !feature.HasSpec || !feature.HasPlan || !feature.HasTasks {
		t.Fatalf("feature=%#v", feature)
	}

	path := "specs/001-avatar-upload/spec.md"
	writeReq := httptest.NewRequest(http.MethodPut, "/api/spec-kit/files?path="+path,
		bytes.NewBufferString(`{"content":"# Feature Specification: Avatar Upload\n\n## Requirements\n\n- **FR-001**: The system MUST store images.\n"}`))
	writeReq.Header.Set(ConfirmHeader, "nonce-speckit-write")
	writeRec := httptest.NewRecorder()
	r.ServeHTTP(writeRec, writeReq)
	if writeRec.Code != http.StatusOK {
		t.Fatalf("write status=%d body=%s", writeRec.Code, writeRec.Body.String())
	}

	readReq := httptest.NewRequest(http.MethodGet, "/api/spec-kit/files?path="+path, nil)
	readRec := httptest.NewRecorder()
	r.ServeHTTP(readRec, readReq)
	if readRec.Code != http.StatusOK {
		t.Fatalf("read status=%d body=%s", readRec.Code, readRec.Body.String())
	}
	if !bytes.Contains(readRec.Body.Bytes(), []byte("MUST store images")) {
		t.Fatalf("read body=%s", readRec.Body.String())
	}
}

func TestSpecKitFileEditorRejectsEscapes(t *testing.T) {
	root := seedSpecKitFeature(t)
	r := NewRouter(config.ServeConfig{ProjectDir: root}, fakeSPA(), nil)
	t.Cleanup(r.Close)

	req := httptest.NewRequest(http.MethodGet, "/api/spec-kit/files?path=../AGENTS.md", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func seedSpecKitFeature(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "specs", "001-auth")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, ".beads"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "spec.md"), []byte(`# Feature Specification: Login Recovery

### User Story 1 - Reset password (Priority: P1)

**Acceptance Scenarios**:
1. **Given** a known email, **When** reset is requested, **Then** a recovery link is sent

## Requirements
- **FR-001**: The system MUST send a reset email.
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tasks.md"), []byte(`## Phase 3: User Story 1
- [ ] T001 [P] [US1] Create recovery form in web/src/auth.tsx
- [ ] T002 [US1] Add validation tests in web/src/auth.test.ts
`), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func containsAllLabels(labels, wants []string) bool {
	for _, want := range wants {
		found := false
		for _, label := range labels {
			if label == want {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
