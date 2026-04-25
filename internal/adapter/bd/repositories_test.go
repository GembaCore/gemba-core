package bd

import (
	"testing"

	"github.com/MikeBengtson/gemba/internal/core"
)

func TestRepositoriesFromLabels_Empty(t *testing.T) {
	primary, ids := repositoriesFromLabels(nil)
	if primary != "" || ids != nil {
		t.Errorf("got (%q, %v), want zeros", primary, ids)
	}
}

func TestRepositoriesFromLabels_NoRepoLabels(t *testing.T) {
	primary, ids := repositoriesFromLabels([]string{"area:capability", "type:milestone"})
	if primary != "" || len(ids) != 0 {
		t.Errorf("got (%q, %v), want zeros", primary, ids)
	}
}

func TestRepositoriesFromLabels_SingleRepo(t *testing.T) {
	primary, ids := repositoriesFromLabels([]string{"repo:gemba", "area:capability"})
	if primary != "gemba" {
		t.Errorf("primary = %q, want gemba", primary)
	}
	if len(ids) != 1 || ids[0] != "gemba" {
		t.Errorf("ids = %v, want [gemba]", ids)
	}
}

func TestRepositoriesFromLabels_MultiRepoOrderPreserved(t *testing.T) {
	primary, ids := repositoriesFromLabels([]string{
		"area:capability", "repo:backend", "type:bug", "repo:frontend",
	})
	if primary != "backend" {
		t.Errorf("primary = %q, want backend (first repo: label wins)", primary)
	}
	want := []core.RepositoryID{"backend", "frontend"}
	if len(ids) != len(want) {
		t.Fatalf("ids = %v, want %v", ids, want)
	}
	for i := range ids {
		if ids[i] != want[i] {
			t.Errorf("ids[%d] = %q, want %q", i, ids[i], want[i])
		}
	}
}

func TestRepositoriesFromLabels_SkipsEmptyAndDuplicates(t *testing.T) {
	primary, ids := repositoriesFromLabels([]string{
		"repo:frontend", "repo:", "repo:frontend", "repo:backend",
	})
	if primary != "frontend" {
		t.Errorf("primary = %q, want frontend", primary)
	}
	if len(ids) != 2 || ids[0] != "frontend" || ids[1] != "backend" {
		t.Errorf("ids = %v, want [frontend backend]", ids)
	}
}

func TestRepositoryLabels_Empty(t *testing.T) {
	if got := repositoryLabels("", nil); got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

func TestRepositoryLabels_PrimaryOnly(t *testing.T) {
	got := repositoryLabels("gemba", nil)
	if len(got) != 1 || got[0] != "repo:gemba" {
		t.Errorf("got %v, want [repo:gemba]", got)
	}
}

func TestRepositoryLabels_PrimaryAndIDs(t *testing.T) {
	got := repositoryLabels("backend", []core.RepositoryID{"backend", "frontend"})
	want := []string{"repo:backend", "repo:frontend"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("[%d] got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestRepositoryLabels_DropsDuplicates(t *testing.T) {
	got := repositoryLabels("backend", []core.RepositoryID{"frontend", "backend", "backend"})
	if len(got) != 2 || got[0] != "repo:backend" || got[1] != "repo:frontend" {
		t.Errorf("got %v", got)
	}
}

func TestStripRepositoryLabels(t *testing.T) {
	got := stripRepositoryLabels([]string{
		"repo:gemba", "area:capability", "repo:frontend", "type:bug",
	})
	want := []string{"area:capability", "type:bug"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("[%d] got %q, want %q", i, got[i], want[i])
		}
	}
}

// gm-kdh3: end-to-end — a Bead with `repo:*` labels projects onto a
// WorkItem whose RepositoryIDs and PrimaryRepositoryID are populated.
func TestBeadToWorkItem_PopulatesRepositoryFields(t *testing.T) {
	b := &Bead{
		ID:        "e3",
		Title:     "Plan view",
		Status:    "open",
		IssueType: "epic",
		Labels:    []string{"area:capability", "repo:frontend", "repo:backend"},
	}
	wi := b.toWorkItem("gm")
	if wi.PrimaryRepositoryID != "frontend" {
		t.Errorf("primary = %q, want frontend", wi.PrimaryRepositoryID)
	}
	if len(wi.RepositoryIDs) != 2 {
		t.Fatalf("RepositoryIDs = %v, want 2 entries", wi.RepositoryIDs)
	}
}

// A bead with no repo:* labels projects onto a WorkItem with empty
// repository fields — the spawn path is the right place to reject
// such legacy beads.
func TestBeadToWorkItem_NoRepoLabelsLeavesFieldsEmpty(t *testing.T) {
	b := &Bead{
		ID:        "e3",
		Title:     "Plan view",
		Status:    "open",
		IssueType: "epic",
		Labels:    []string{"area:capability"},
	}
	wi := b.toWorkItem("gm")
	if wi.PrimaryRepositoryID != "" {
		t.Errorf("PrimaryRepositoryID = %q, want empty", wi.PrimaryRepositoryID)
	}
	if len(wi.RepositoryIDs) != 0 {
		t.Errorf("RepositoryIDs = %v, want empty", wi.RepositoryIDs)
	}
}
