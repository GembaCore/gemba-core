package speckit

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestScannerReadsSpecKitFeature(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "specs", "001-auth")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "spec.md"), []byte(`# Feature Specification: Login Recovery

### User Story 1 - Reset password (Priority: P1)

**Acceptance Scenarios**
- Given a known email, When reset is requested, Then a recovery link is sent

## Requirements
- **FR-001**: The system MUST send a reset email.
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tasks.md"), []byte(`## Phase 3: User Story 1
- [ ] T001 [P] [US1] Create recovery form
- [x] T002 [US1] Add validation tests
`), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := NewScanner(root).List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !got.Configured || got.Total != 1 {
		t.Fatalf("configured=%v total=%d, want configured total=1", got.Configured, got.Total)
	}
	f := got.Features[0]
	if f.Title != "Login Recovery" {
		t.Fatalf("title=%q", f.Title)
	}
	if len(f.Spec.UserStories) != 1 || f.Spec.UserStories[0].ID != "US1" {
		t.Fatalf("user stories=%#v", f.Spec.UserStories)
	}
	if f.TaskCount != 2 || f.ParallelTaskCount != 1 {
		t.Fatalf("task counts=%d parallel=%d", f.TaskCount, f.ParallelTaskCount)
	}
	if f.Tasks[0].ID != "T001" || !f.Tasks[0].Parallel || f.Tasks[0].StoryID != "US1" {
		t.Fatalf("first task=%#v", f.Tasks[0])
	}
}
