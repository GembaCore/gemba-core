package speckit

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/GembaCore/gemba-core/core"
)

func TestSyncFeatureCreatesMilestoneEpicStoriesAndTasks(t *testing.T) {
	wp := newMemoryWP()
	wp.items["gm-stale"] = core.WorkItem{
		ID:     "gm-stale",
		Kind:   "task",
		Title:  "T999: Removed task",
		Labels: []string{LabelSource, featureLabel("001-auth"), LabelTask, "speckit:T999"},
	}
	feature := Feature{
		ID:    "001-auth",
		Title: "Login Recovery",
		Spec: SpecSummary{
			UserStories: []UserStory{
				{ID: "US1", Title: "Reset password", Priority: "P1", AcceptanceScenarios: []string{"Recovery email is sent"}},
			},
			AcceptanceScenarios: []string{"Recovery email is sent"},
		},
		TasksPath: "specs/001-auth/tasks.md",
		Tasks: []Task{
			{ID: "T001", Title: "Create recovery form", StoryID: "US1", Parallel: true, Line: 3},
			{ID: "T002", Title: "Add validation tests", StoryID: "US1", Line: 4},
		},
	}

	result, err := SyncFeature(context.Background(), wp, feature)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Created) != 5 {
		t.Fatalf("created=%#v, want milestone + feature epic + story + 2 tasks", result.Created)
	}
	if len(result.Deleted) != 1 || result.Deleted[0] != "gm-stale" {
		t.Fatalf("deleted=%#v, want stale task delete", result.Deleted)
	}
	if result.Plan.Counts.Create != 5 || result.Plan.Counts.Delete != 1 {
		t.Fatalf("plan counts=%#v", result.Plan.Counts)
	}
	if result.Plan.Hash == "" {
		t.Fatal("plan hash should be populated")
	}
	if err := ValidateManifestJSONL(result.Plan.JSONL); err != nil {
		t.Fatalf("manifest should validate: %v", err)
	}
	kinds := wp.kinds()
	if kinds[core.KindMilestone] != 1 || kinds["epic"] != 1 || kinds["story"] != 1 || kinds["task"] != 2 {
		t.Fatalf("kinds=%#v", kinds)
	}
	task := wp.byLabel("speckit:T001")
	if task.ID == "" {
		t.Fatal("missing task for T001")
	}
	if task.Kind != "task" || !hasLabel(task.Labels, "speckit:parallel") {
		t.Fatalf("task projection=%#v", task)
	}
	parent := parentFrom(task.Relationships)
	if parent == "" || string(parent) != result.Stories["US1"] {
		t.Fatalf("task parent=%q, want US1 story %q", parent, result.Stories["US1"])
	}

	again, err := SyncFeature(context.Background(), wp, feature)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Created) != 0 || len(again.Updated) != 5 {
		t.Fatalf("second sync created=%#v updated=%#v", again.Created, again.Updated)
	}
}

func TestSyncFeatureRequiresDeleteApprovalWhenConfigured(t *testing.T) {
	wp := newMemoryWP()
	wp.items["gm-stale"] = core.WorkItem{
		ID:     "gm-stale",
		Kind:   "task",
		Title:  "T999: Removed task",
		Labels: []string{LabelSource, featureLabel("001-auth"), LabelTask, "speckit:T999"},
	}
	feature := Feature{
		ID:    "001-auth",
		Title: "Login Recovery",
		Tasks: []Task{{ID: "T001", Title: "Create recovery form"}},
	}
	plan, err := PlanFeature(context.Background(), wp, feature)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SyncFeatureWithOptions(context.Background(), wp, feature, SyncOptions{
		ExpectedHash: plan.Hash,
	}); err == nil || !strings.Contains(err.Error(), "allow_deletes") {
		t.Fatalf("expected delete approval error, got %v", err)
	}
	if _, err := SyncFeatureWithOptions(context.Background(), wp, feature, SyncOptions{
		ExpectedHash: "sha256:stale",
		AllowDeletes: true,
	}); err == nil || !strings.Contains(err.Error(), "stale plan hash") {
		t.Fatalf("expected stale hash error, got %v", err)
	}
}

type memoryWP struct {
	items map[core.WorkItemID]core.WorkItem
}

func newMemoryWP() *memoryWP { return &memoryWP{items: map[core.WorkItemID]core.WorkItem{}} }

func (m *memoryWP) Describe(context.Context) (core.CapabilityManifest, error) {
	return core.CapabilityManifest{
		AdaptorName:     "memory",
		AdaptorVersion:  "0.1.0",
		ProtocolVersion: core.ProtocolVersion,
		Transport:       core.TransportAPI,
		StateMap:        core.StateMap{"open": core.StateBacklog},
	}, nil
}

func (m *memoryWP) ListWorkItems(_ context.Context, filter core.WorkItemFilter) ([]core.WorkItem, error) {
	var out []core.WorkItem
	for _, item := range m.items {
		if labelsContainAll(item.Labels, filter.Labels) {
			out = append(out, item)
		}
	}
	return out, nil
}

func (m *memoryWP) GetWorkItem(_ context.Context, id core.WorkItemID) (core.WorkItem, error) {
	item, ok := m.items[id]
	if !ok {
		return core.WorkItem{}, core.ErrNotFound
	}
	return item, nil
}

func (m *memoryWP) CreateWorkItem(_ context.Context, wi core.WorkItem) (core.WorkItem, error) {
	if wi.ID == "" {
		wi.ID = core.WorkItemID("gm-" + strings.ReplaceAll(wi.Title, " ", "-"))
	}
	now := time.Now().UTC()
	wi.CreatedAt = now
	wi.UpdatedAt = now
	m.items[wi.ID] = wi
	return wi, nil
}

func (m *memoryWP) UpdateWorkItem(_ context.Context, id core.WorkItemID, patch core.WorkItemPatch) (core.WorkItem, error) {
	item, ok := m.items[id]
	if !ok {
		return core.WorkItem{}, core.ErrNotFound
	}
	if patch.Title != nil {
		item.Title = *patch.Title
	}
	if patch.Description != nil {
		item.Description = *patch.Description
	}
	if patch.Status != nil {
		item.Status = *patch.Status
	}
	if patch.StateCategory != nil {
		item.StateCategory = *patch.StateCategory
	}
	if patch.Labels != nil {
		item.Labels = patch.Labels
	}
	if patch.DoD != nil {
		item.DoD = patch.DoD
	}
	if patch.Parent != nil {
		item.Relationships = []core.Relationship{{Kind: core.RelParentChild, From: core.WorkItemID(*patch.Parent), To: id}}
	}
	item.UpdatedAt = time.Now().UTC()
	m.items[id] = item
	return item, nil
}

func (m *memoryWP) DeleteWorkItem(_ context.Context, id core.WorkItemID) (core.WorkItem, error) {
	item, ok := m.items[id]
	if !ok {
		return core.WorkItem{}, core.ErrNotFound
	}
	delete(m.items, id)
	return item, nil
}

func (m *memoryWP) AttachEvidence(context.Context, core.WorkItemID, core.Evidence) error { return nil }
func (m *memoryWP) ListSprints(context.Context) ([]core.Sprint, error)                   { return nil, nil }
func (m *memoryWP) ReadBudgetRollup(context.Context, string) (core.BudgetRollup, error) {
	return core.BudgetRollup{}, nil
}
func (m *memoryWP) Subscribe(context.Context, core.WorkPlaneSubscribeFilter) (<-chan core.WorkPlaneEvent, error) {
	return make(chan core.WorkPlaneEvent), nil
}

func (m *memoryWP) kinds() map[string]int {
	out := map[string]int{}
	for _, item := range m.items {
		out[item.Kind]++
	}
	return out
}

func (m *memoryWP) byLabel(label string) core.WorkItem {
	for _, item := range m.items {
		if hasLabel(item.Labels, label) {
			return item
		}
	}
	return core.WorkItem{}
}

func labelsContainAll(labels, wants []string) bool {
	for _, want := range wants {
		if !hasLabel(labels, want) {
			return false
		}
	}
	return true
}

func hasLabel(labels []string, want string) bool {
	for _, label := range labels {
		if label == want {
			return true
		}
	}
	return false
}
