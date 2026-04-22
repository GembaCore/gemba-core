package bd_test

import (
	"context"
	"testing"

	"github.com/MikeBengtson/gemba/internal/adapter/bd"
	"github.com/MikeBengtson/gemba/internal/core"
)

// TestAgentRefSynthesisFromLabels locks the gm-e6.3 DoD: listing a bead
// whose label bag carries agent:role:* / agent:parent:* produces an
// AgentRef with those fields populated. Absence of the labels must leave
// the fields zero — synthesis is permissive, never fabricating metadata.
func TestAgentRefSynthesisFromLabels(t *testing.T) {
	cases := []struct {
		name       string
		assignee   string
		labels     []string
		wantNil    bool
		wantRole   string
		wantParent core.AgentID
	}{
		{
			name:    "no assignee yields nil ref",
			labels:  []string{"agent:role:polecat"},
			wantNil: true,
		},
		{
			name:       "role + parent labels populate both fields",
			assignee:   "gemba/polecats/onyx",
			labels:     []string{"agent:role:polecat", "agent:parent:gemba/crew/mike", "area:core"},
			wantRole:   "polecat",
			wantParent: "gemba/crew/mike",
		},
		{
			name:     "role-only label leaves parent nil",
			assignee: "gemba/polecats/onyx",
			labels:   []string{"agent:role:witness"},
			wantRole: "witness",
		},
		{
			name:       "parent-only label leaves role blank",
			assignee:   "gemba/polecats/onyx",
			labels:     []string{"agent:parent:gemba/crew/mayor"},
			wantParent: "gemba/crew/mayor",
		},
		{
			name:     "bare assignee with no federated labels still produces ref",
			assignee: "gemba/polecats/onyx",
			labels:   []string{"area:core", "tier:sonnet"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := newFakeBd(t)
			src := bd.Bead{
				ID:        "gm-t1",
				Title:     "t",
				Status:    "open",
				IssueType: "task",
				Assignee:  tc.assignee,
				Labels:    tc.labels,
			}
			fake.beads[src.ID] = &src
			impl := bd.NewWorkPlaneWithRunner(fake.run, "")

			wi, err := impl.GetWorkItem(context.Background(), core.WorkItemID("gemba/gemba/gm-t1"))
			if err != nil {
				t.Fatalf("GetWorkItem: %v", err)
			}
			if tc.wantNil {
				if wi.Assignee != nil {
					t.Fatalf("want nil Assignee, got %+v", wi.Assignee)
				}
				return
			}
			if wi.Assignee == nil {
				t.Fatal("want non-nil Assignee")
			}
			if string(wi.Assignee.ID) != tc.assignee {
				t.Errorf("Assignee.ID=%q want %q", wi.Assignee.ID, tc.assignee)
			}
			if wi.Assignee.Kind != core.AgentKindAgent {
				t.Errorf("Assignee.Kind=%q want %q", wi.Assignee.Kind, core.AgentKindAgent)
			}
			if wi.Assignee.Role != tc.wantRole {
				t.Errorf("Assignee.Role=%q want %q", wi.Assignee.Role, tc.wantRole)
			}
			if tc.wantParent == "" {
				if wi.Assignee.ParentID != nil {
					t.Errorf("want nil ParentID, got %q", *wi.Assignee.ParentID)
				}
			} else {
				if wi.Assignee.ParentID == nil || *wi.Assignee.ParentID != tc.wantParent {
					t.Errorf("ParentID=%v want %q", wi.Assignee.ParentID, tc.wantParent)
				}
			}
		})
	}
}

// TestAgentRefRoundTripOnCreate locks the write half of the DoD: a
// WorkItem created with a fully-populated Assignee lands in Beads with
// --assignee set and agent:role:* / agent:parent:* labels present.
func TestAgentRefRoundTripOnCreate(t *testing.T) {
	fake := newFakeBd(t)
	impl := bd.NewWorkPlaneWithRunner(fake.run, "")

	parent := core.AgentID("gemba/crew/mike")
	created, err := impl.CreateWorkItem(context.Background(), core.WorkItem{
		Title:  "claimable",
		Kind:   "task",
		Status: "open",
		Labels: []string{"area:core"},
		Assignee: &core.AgentRef{
			ID:       "gemba/polecats/onyx",
			Name:     "gemba/polecats/onyx",
			Kind:     core.AgentKindAgent,
			Role:     "polecat",
			ParentID: &parent,
		},
	})
	if err != nil {
		t.Fatalf("CreateWorkItem: %v", err)
	}
	if created.Assignee == nil {
		t.Fatal("created.Assignee nil — AgentRef lost on round-trip")
	}
	if string(created.Assignee.ID) != "gemba/polecats/onyx" {
		t.Errorf("Assignee.ID=%q want gemba/polecats/onyx", created.Assignee.ID)
	}
	if created.Assignee.Role != "polecat" {
		t.Errorf("Assignee.Role=%q want polecat", created.Assignee.Role)
	}
	if created.Assignee.ParentID == nil || *created.Assignee.ParentID != parent {
		t.Errorf("Assignee.ParentID=%v want %q", created.Assignee.ParentID, parent)
	}
	if !containsLabel(created.Labels, "area:core") {
		t.Errorf("caller-supplied label area:core dropped; got %v", created.Labels)
	}
}

// TestAgentRefRoundTripOnUpdateStripsStaleLabels verifies the write path
// is authoritative for agent:* labels: a prior assignee's role/parent
// labels are replaced by the new assignee's, not stacked on top.
func TestAgentRefRoundTripOnUpdateStripsStaleLabels(t *testing.T) {
	fake := newFakeBd(t)
	// Seed a bead whose assignee is already tagged with role + parent,
	// so the update path must prove it CAN overwrite stale federation
	// labels when the caller resets the full label set.
	src := bd.Bead{
		ID:        "gm-abc",
		Title:     "t",
		Status:    "in_progress",
		IssueType: "task",
		Assignee:  "gemba/polecats/jasper",
		Labels:    []string{"area:core", "agent:role:polecat", "agent:parent:gemba/crew/mayor"},
	}
	fake.beads[src.ID] = &src
	impl := bd.NewWorkPlaneWithRunner(fake.run, "")

	newParent := core.AgentID("gemba/crew/mike")
	newAssignee := &core.AgentRef{
		ID:       "gemba/polecats/onyx",
		Name:     "gemba/polecats/onyx",
		Kind:     core.AgentKindAgent,
		Role:     "witness",
		ParentID: &newParent,
	}
	// Caller-supplied labels carry a stale agent:role:* that we expect
	// the adaptor to strip in favor of the AgentRef's authoritative view.
	patch := core.WorkItemPatch{
		Assignee: newAssignee,
		Labels:   []string{"area:core", "agent:role:stale-should-be-stripped"},
	}
	wi, err := impl.UpdateWorkItem(
		context.Background(),
		core.WorkItemID("gemba/gemba/gm-abc"),
		patch,
	)
	if err != nil {
		t.Fatalf("UpdateWorkItem: %v", err)
	}

	if wi.Assignee == nil || string(wi.Assignee.ID) != "gemba/polecats/onyx" {
		t.Errorf("Assignee not reassigned: %+v", wi.Assignee)
	}
	if wi.Assignee.Role != "witness" {
		t.Errorf("Assignee.Role=%q want witness", wi.Assignee.Role)
	}
	if wi.Assignee.ParentID == nil || *wi.Assignee.ParentID != newParent {
		t.Errorf("Assignee.ParentID=%v want %q", wi.Assignee.ParentID, newParent)
	}
	if containsLabel(wi.Labels, "agent:role:polecat") {
		t.Errorf("stale agent:role:polecat not stripped; labels=%v", wi.Labels)
	}
	if containsLabel(wi.Labels, "agent:role:stale-should-be-stripped") {
		t.Errorf("patch's stale agent:role:* not stripped; labels=%v", wi.Labels)
	}
	if containsLabel(wi.Labels, "agent:parent:gemba/crew/mayor") {
		t.Errorf("stale agent:parent:gemba/crew/mayor not stripped; labels=%v", wi.Labels)
	}
	if !containsLabel(wi.Labels, "agent:role:witness") {
		t.Errorf("new agent:role:witness missing; labels=%v", wi.Labels)
	}
	if !containsLabel(wi.Labels, "agent:parent:gemba/crew/mike") {
		t.Errorf("new agent:parent:gemba/crew/mike missing; labels=%v", wi.Labels)
	}
}

// TestAgentRefUpdateAssigneeOnlyIsAdditive verifies an Assignee-only
// patch (no Labels) uses --add-label instead of --set-labels: we don't
// want a bare "re-claim" to silently erase every other label the caller
// never touched.
func TestAgentRefUpdateAssigneeOnlyIsAdditive(t *testing.T) {
	fake := newFakeBd(t)
	src := bd.Bead{
		ID:        "gm-xyz",
		Title:     "t",
		Status:    "open",
		IssueType: "task",
		Assignee:  "gemba/polecats/old",
		Labels:    []string{"area:core", "tier:sonnet"},
	}
	fake.beads[src.ID] = &src
	impl := bd.NewWorkPlaneWithRunner(fake.run, "")

	parent := core.AgentID("gemba/crew/mike")
	wi, err := impl.UpdateWorkItem(
		context.Background(),
		core.WorkItemID("gemba/gemba/gm-xyz"),
		core.WorkItemPatch{Assignee: &core.AgentRef{
			ID: "gemba/polecats/onyx", Name: "onyx",
			Kind: core.AgentKindAgent, Role: "polecat", ParentID: &parent,
		}},
	)
	if err != nil {
		t.Fatalf("UpdateWorkItem: %v", err)
	}
	if !containsLabel(wi.Labels, "area:core") || !containsLabel(wi.Labels, "tier:sonnet") {
		t.Errorf("additive update erased pre-existing labels; got %v", wi.Labels)
	}
	if !containsLabel(wi.Labels, "agent:role:polecat") {
		t.Errorf("new agent:role:polecat missing; labels=%v", wi.Labels)
	}
	if !containsLabel(wi.Labels, "agent:parent:gemba/crew/mike") {
		t.Errorf("new agent:parent:gemba/crew/mike missing; labels=%v", wi.Labels)
	}
}

func containsLabel(labels []string, want string) bool {
	for _, l := range labels {
		if l == want {
			return true
		}
	}
	return false
}
