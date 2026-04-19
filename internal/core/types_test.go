package core

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestStateCategoryValid(t *testing.T) {
	for _, s := range []StateCategory{
		StateBacklog, StateUnstarted, StateStarted, StateCompleted, StateCanceled,
	} {
		if !s.Valid() {
			t.Errorf("%q expected Valid()=true", s)
		}
	}
	if StateCategory("in_progress").Valid() {
		t.Error("in_progress should not be a valid core StateCategory")
	}
}

func TestStateCategoryJSON(t *testing.T) {
	// Accept the five known strings.
	for _, s := range []StateCategory{
		StateBacklog, StateUnstarted, StateStarted, StateCompleted, StateCanceled,
	} {
		data, err := json.Marshal(s)
		if err != nil {
			t.Fatalf("marshal %q: %v", s, err)
		}
		var round StateCategory
		if err := json.Unmarshal(data, &round); err != nil {
			t.Fatalf("unmarshal %q: %v", s, err)
		}
		if round != s {
			t.Errorf("round trip: got %q, want %q", round, s)
		}
	}

	// Reject unknown strings.
	var s StateCategory
	if err := json.Unmarshal([]byte(`"nonsense"`), &s); err == nil {
		t.Error("expected decode error for unknown category")
	}
}

func TestWorkItemRoundTrip(t *testing.T) {
	ts := time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)
	prio := 0
	sprintID := "sp-2026-w16"

	wi := WorkItem{
		ID:            "bd:gm-e3.1",
		Kind:          "task",
		Title:         "Define core types",
		Description:   "See gm-e3.1.",
		Status:        "in_progress",
		StateCategory: StateStarted,
		Priority:      &prio,
		Owner: &AgentRef{
			ID: "gt:gemba/crew/mike", Name: "mike", Role: "crew",
		},
		Assignee: &AgentRef{
			ID: "gt:gemba/polecats/jasper", Name: "jasper",
			Role: "polecat", Workspace: "gemba",
		},
		Labels: []string{"fed:safe", "layer:core"},
		Relationships: []Relationship{
			{Kind: RelBlocks, From: "bd:gm-e3.1", To: "bd:gm-e3.2"},
			{Kind: RelChildOf, From: "bd:gm-e3.1", To: "bd:gm-e3"},
		},
		Evidence: []Evidence{
			{
				ID: "ev-1", Kind: EvidenceCommit,
				Source: "git", Ref: "deadbeef",
				Summary: "types.go added", CapturedAt: ts,
			},
		},
		DoD: &DefinitionOfDone{
			AcceptanceCriteria: []string{"godoc present", "JSON round-trip tested"},
			Notes:              "informational-only",
			Version:            "v1",
		},
		SprintID:  &sprintID,
		CreatedAt: ts,
		UpdatedAt: ts,
		Custom:    map[string]any{"adaptor": "bd"},
	}

	data, err := json.Marshal(wi)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var round WorkItem
	if err := json.Unmarshal(data, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !reflect.DeepEqual(wi, round) {
		t.Errorf("round trip mismatch:\n got: %+v\nwant: %+v", round, wi)
	}
}

func TestEvidenceKindOptionalFields(t *testing.T) {
	// Kind=url with only URL fields set should round-trip without losing
	// empties as `null`.
	ev := Evidence{
		ID:         "ev-x",
		Kind:       EvidenceURL,
		Source:     "github",
		Ref:        "https://example.test/pr/1",
		CapturedAt: time.Unix(1_700_000_000, 0).UTC(),
	}
	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Summary and Payload are omitempty; neither key should appear.
	var generic map[string]any
	if err := json.Unmarshal(data, &generic); err != nil {
		t.Fatalf("decode generic: %v", err)
	}
	for _, k := range []string{"summary", "payload"} {
		if _, ok := generic[k]; ok {
			t.Errorf("unexpected key %q in encoded evidence: %s", k, data)
		}
	}
}
