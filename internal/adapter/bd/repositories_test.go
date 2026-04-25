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
	wi := b.toWorkItem("gm", nil)
	if wi.PrimaryRepositoryID != "frontend" {
		t.Errorf("primary = %q, want frontend", wi.PrimaryRepositoryID)
	}
	if len(wi.RepositoryIDs) != 2 {
		t.Fatalf("RepositoryIDs = %v, want 2 entries", wi.RepositoryIDs)
	}
}

// gm-d2ts: prefixOf splits a bead id at the first hyphen.
func TestPrefixOf(t *testing.T) {
	cases := []struct{ in, want string }{
		{"gm-e3", "gm"},
		{"fe-bug-44", "fe"},
		{"backend-1", "backend"},
		{"e3", ""},
		{"", ""},
		{"-", ""},
		{"-leading", ""},
	}
	for _, c := range cases {
		if got := prefixOf(c.in); got != c.want {
			t.Errorf("prefixOf(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// gm-d2ts: when a bead has no `repo:*` label and a registry is wired
// in, the bd adaptor falls back to bead-id-prefix routing to populate
// PrimaryRepositoryID + RepositoryIDs.
func TestBeadToWorkItem_PrefixRoutingFallback(t *testing.T) {
	reg := core.NewRepositoryRegistry()
	if err := reg.Register(&core.Repository{
		ID:            "frontend",
		Path:          "/abs/fe",
		DefaultBranch: "main",
		BeadPrefix:    "fe",
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	b := &Bead{
		ID:        "fe-e3",
		Title:     "Plan view",
		Status:    "open",
		IssueType: "epic",
		Labels:    []string{"area:capability"}, // no repo:* label
	}
	wi := b.toWorkItem("gm", reg)
	if wi.PrimaryRepositoryID != "frontend" {
		t.Errorf("PrimaryRepositoryID = %q, want frontend (prefix routing)", wi.PrimaryRepositoryID)
	}
	if len(wi.RepositoryIDs) != 1 || wi.RepositoryIDs[0] != "frontend" {
		t.Errorf("RepositoryIDs = %v, want [frontend]", wi.RepositoryIDs)
	}
}

// Explicit `repo:*` label wins over prefix routing — the operator's
// intent is authoritative.
func TestBeadToWorkItem_ExplicitLabelBeatsPrefixRouting(t *testing.T) {
	reg := core.NewRepositoryRegistry()
	for _, r := range []*core.Repository{
		{ID: "frontend", Path: "/abs/fe", DefaultBranch: "main", BeadPrefix: "fe"},
		{ID: "backend", Path: "/abs/be", DefaultBranch: "main", BeadPrefix: "be"},
	} {
		_ = reg.Register(r)
	}
	b := &Bead{
		ID:        "fe-e3",
		Title:     "Plan view",
		Status:    "open",
		IssueType: "epic",
		Labels:    []string{"repo:backend"}, // explicit override
	}
	wi := b.toWorkItem("gm", reg)
	if wi.PrimaryRepositoryID != "backend" {
		t.Errorf("explicit label should win: got %q, want backend", wi.PrimaryRepositoryID)
	}
}

// Without a registry (legacy callers), prefix routing is skipped and
// the WorkItem keeps empty repository fields.
func TestBeadToWorkItem_NoRegistryNoPrefixRouting(t *testing.T) {
	b := &Bead{
		ID:        "fe-e3",
		Title:     "x",
		Status:    "open",
		IssueType: "epic",
	}
	wi := b.toWorkItem("gm", nil)
	if wi.PrimaryRepositoryID != "" {
		t.Errorf("no-registry mode should leave repo empty: %q", wi.PrimaryRepositoryID)
	}
}

// Unknown prefix in the registry → no match, empty fields.
func TestBeadToWorkItem_UnknownPrefixLeavesFieldsEmpty(t *testing.T) {
	reg := core.NewRepositoryRegistry()
	_ = reg.Register(&core.Repository{
		ID: "frontend", Path: "/abs/fe", DefaultBranch: "main", BeadPrefix: "fe",
	})
	b := &Bead{
		ID:        "ghost-e3",
		Title:     "x",
		Status:    "open",
		IssueType: "epic",
	}
	wi := b.toWorkItem("gm", reg)
	if wi.PrimaryRepositoryID != "" {
		t.Errorf("unknown prefix should leave repo empty: %q", wi.PrimaryRepositoryID)
	}
}

// gm-v8vr: read:<glob> + write:<glob> label projection.
func TestReadPathsFromLabels(t *testing.T) {
	got := readPathsFromLabels([]string{
		"area:capability",
		"read:~/.aws/credentials",
		"read:",                   // skipped
		"read:~/.aws/credentials", // dup, skipped
		"read:/notes/spec.md",
	})
	want := []string{"~/.aws/credentials", "/notes/spec.md"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("[%d] got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestWritePathsFromLabels(t *testing.T) {
	got := writePathsFromLabels([]string{"write:/scratch", "area:bug"})
	if len(got) != 1 || got[0] != "/scratch" {
		t.Errorf("got %v", got)
	}
}

func TestReadPathLabels_Roundtrip(t *testing.T) {
	in := []string{"~/.aws/credentials", "/notes/spec.md", "", "~/.aws/credentials"}
	got := readPathLabels(in)
	want := []string{"read:~/.aws/credentials", "read:/notes/spec.md"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	parsed := readPathsFromLabels(got)
	if len(parsed) != 2 {
		t.Errorf("round-trip lost: %v", parsed)
	}
}

func TestWritePathLabels(t *testing.T) {
	got := writePathLabels([]string{"/scratch", ""})
	if len(got) != 1 || got[0] != "write:/scratch" {
		t.Errorf("got %v", got)
	}
}

// gm-v8vr end-to-end: bead with read: + write: labels projects onto
// WorkItem.AdditionalReadPaths + AdditionalWritePaths.
func TestBeadToWorkItem_PopulatesAdditionalPaths(t *testing.T) {
	b := &Bead{
		ID:        "e3",
		Title:     "x",
		Status:    "open",
		IssueType: "epic",
		Labels: []string{
			"area:capability",
			"read:~/.aws/credentials",
			"write:/scratch",
		},
	}
	wi := b.toWorkItem("gm", nil)
	if len(wi.AdditionalReadPaths) != 1 || wi.AdditionalReadPaths[0] != "~/.aws/credentials" {
		t.Errorf("AdditionalReadPaths = %v", wi.AdditionalReadPaths)
	}
	if len(wi.AdditionalWritePaths) != 1 || wi.AdditionalWritePaths[0] != "/scratch" {
		t.Errorf("AdditionalWritePaths = %v", wi.AdditionalWritePaths)
	}
}

// gm-ou02: branch:<repo>:<name> labels project onto BeadBranch entries.
func TestBranchesFromLabels_Empty(t *testing.T) {
	if got := branchesFromLabels(nil); got != nil {
		t.Errorf("got %v, want nil", got)
	}
	if got := branchesFromLabels([]string{"area:capability"}); got != nil {
		t.Errorf("non-branch labels should yield nil: %v", got)
	}
}

func TestBranchesFromLabels_Single(t *testing.T) {
	got := branchesFromLabels([]string{
		"area:capability", "branch:gemba:feature/gm-e3",
	})
	if len(got) != 1 || got[0].RepositoryID != "gemba" || got[0].Branch != "feature/gm-e3" {
		t.Errorf("got %+v, want [{gemba feature/gm-e3}]", got)
	}
}

// Branch names with colons (some teams use them) survive — only the
// FIRST colon after the prefix splits repo from branch.
func TestBranchesFromLabels_ColonInBranchName(t *testing.T) {
	got := branchesFromLabels([]string{"branch:frontend:user/jane:fix-bug-44"})
	if len(got) != 1 {
		t.Fatalf("got %d, want 1", len(got))
	}
	if got[0].Branch != "user/jane:fix-bug-44" {
		t.Errorf("branch = %q, want user/jane:fix-bug-44", got[0].Branch)
	}
}

func TestBranchesFromLabels_MultiRepo(t *testing.T) {
	got := branchesFromLabels([]string{
		"branch:backend:feature/x", "branch:frontend:feature/x-client",
	})
	if len(got) != 2 {
		t.Fatalf("got %d, want 2", len(got))
	}
	if got[0].RepositoryID != "backend" || got[1].RepositoryID != "frontend" {
		t.Errorf("order lost: %+v", got)
	}
}

// Malformed labels are silently dropped — the validator on WorkItem
// is the right place to surface "your bd state is wrong".
func TestBranchesFromLabels_DropsMalformed(t *testing.T) {
	got := branchesFromLabels([]string{
		"branch:",          // empty body
		"branch:gemba",     // no colon-name
		"branch:gemba:",    // empty branch
		"branch::feature",  // empty repo
		"branch:gemba:ok",  // valid — kept
		"branch:gemba:dup", // duplicate repo — first wins
	})
	if len(got) != 1 || got[0].Branch != "ok" {
		t.Errorf("got %+v, want [{gemba ok}]", got)
	}
}

func TestBranchLabels_Empty(t *testing.T) {
	if got := branchLabels(nil); got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

func TestBranchLabels_Roundtrip(t *testing.T) {
	in := []core.BeadBranch{
		{RepositoryID: "backend", Branch: "feature/x"},
		{RepositoryID: "frontend", Branch: "feature/x-client"},
	}
	got := branchLabels(in)
	want := []string{
		"branch:backend:feature/x",
		"branch:frontend:feature/x-client",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("[%d] got %q, want %q", i, got[i], want[i])
		}
	}
	// Round-trip back: labels → BeadBranches → labels yields the
	// same shape.
	parsed := branchesFromLabels(got)
	if len(parsed) != len(in) {
		t.Errorf("round-trip lost entries: got %v, want %v", parsed, in)
	}
}

func TestBranchLabels_DropsEmptyAndDuplicates(t *testing.T) {
	got := branchLabels([]core.BeadBranch{
		{RepositoryID: "", Branch: "x"},
		{RepositoryID: "gemba", Branch: ""},
		{RepositoryID: "gemba", Branch: "ok"},
		{RepositoryID: "gemba", Branch: "dup"}, // dup repo — kept once
	})
	if len(got) != 1 || got[0] != "branch:gemba:ok" {
		t.Errorf("got %v, want [branch:gemba:ok]", got)
	}
}

func TestStripBranchLabels(t *testing.T) {
	got := stripBranchLabels([]string{
		"branch:gemba:x", "area:capability", "branch:frontend:y", "type:bug",
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

// End-to-end: a Bead with both repo:* AND branch:*:* labels projects
// onto a WorkItem with both Repositories and Branches populated.
func TestBeadToWorkItem_PopulatesBranches(t *testing.T) {
	b := &Bead{
		ID:        "e3",
		Title:     "Plan view",
		Status:    "open",
		IssueType: "epic",
		Labels: []string{
			"area:capability",
			"repo:frontend",
			"repo:backend",
			"branch:frontend:feature/plan-view",
			"branch:backend:feature/plan-view-api",
		},
	}
	wi := b.toWorkItem("gm", nil)
	if len(wi.Branches) != 2 {
		t.Fatalf("Branches = %v, want 2 entries", wi.Branches)
	}
	// Branches validate against the populated RepositoryIDs.
	if err := wi.ValidateBranches(); err != nil {
		t.Errorf("ValidateBranches: %v", err)
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
	wi := b.toWorkItem("gm", nil)
	if wi.PrimaryRepositoryID != "" {
		t.Errorf("PrimaryRepositoryID = %q, want empty", wi.PrimaryRepositoryID)
	}
	if len(wi.RepositoryIDs) != 0 {
		t.Errorf("RepositoryIDs = %v, want empty", wi.RepositoryIDs)
	}
}
