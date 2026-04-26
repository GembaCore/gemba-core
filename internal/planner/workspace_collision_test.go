package planner

import (
	"testing"

	"github.com/MikeBengtson/gemba/internal/core"
)

func TestWorkspaceCollisions_NoBeadsNoEdges(t *testing.T) {
	got := WorkspaceCollisions(nil, nil)
	if len(got) != 0 {
		t.Errorf("empty inputs produced %d edges", len(got))
	}
}

func TestWorkspaceCollisions_SameRepoBranchCollides(t *testing.T) {
	got := WorkspaceCollisions([]BeadTarget{
		{BeadID: "gm-1", Repository: "gemba", Branch: "main"},
		{BeadID: "gm-2", Repository: "gemba", Branch: "main"},
	}, nil)
	if len(got) != 1 {
		t.Fatalf("got %d edges, want 1: %+v", len(got), got)
	}
	if got[0].A != "gm-1" || got[0].B != "gm-2" {
		t.Errorf("edge endpoints wrong: %+v", got[0])
	}
	if got[0].Reason != "same repo+branch" {
		t.Errorf("reason: %q", got[0].Reason)
	}
}

func TestWorkspaceCollisions_DifferentBranchNoCollision(t *testing.T) {
	got := WorkspaceCollisions([]BeadTarget{
		{BeadID: "gm-1", Repository: "gemba", Branch: "main"},
		{BeadID: "gm-2", Repository: "gemba", Branch: "feature/x"},
	}, nil)
	if len(got) != 0 {
		t.Errorf("got %d edges, want 0: %+v", len(got), got)
	}
}

func TestWorkspaceCollisions_DifferentRepoNoCollision(t *testing.T) {
	got := WorkspaceCollisions([]BeadTarget{
		{BeadID: "gm-1", Repository: "gemba", Branch: "main"},
		{BeadID: "gm-2", Repository: "lume", Branch: "main"},
	}, nil)
	if len(got) != 0 {
		t.Errorf("got %d edges, want 0: %+v", len(got), got)
	}
}

func TestWorkspaceCollisions_EmptyRepoOrBranchSkipped(t *testing.T) {
	// Beads that haven't been routed yet (empty Repository OR
	// Branch) MUST NOT collide with anything — otherwise an
	// under-specified bead would conflict with the world.
	got := WorkspaceCollisions([]BeadTarget{
		{BeadID: "gm-1", Repository: "", Branch: "main"},
		{BeadID: "gm-2", Repository: "", Branch: "main"},
		{BeadID: "gm-3", Repository: "gemba", Branch: ""},
	}, nil)
	if len(got) != 0 {
		t.Errorf("got %d edges from empty-repo/branch beads: %+v", len(got), got)
	}
}

func TestWorkspaceCollisions_SameWorktreePathCollides(t *testing.T) {
	got := WorkspaceCollisions([]BeadTarget{
		{BeadID: "gm-1", WorktreePath: "/var/gemba/worktrees/sess-1"},
		{BeadID: "gm-2", WorktreePath: "/var/gemba/worktrees/sess-1"},
	}, nil)
	if len(got) != 1 {
		t.Fatalf("got %d edges, want 1: %+v", len(got), got)
	}
	if got[0].Reason != "same worktree_path" {
		t.Errorf("reason: %q", got[0].Reason)
	}
}

func TestWorkspaceCollisions_WorktreePathCanonicalised(t *testing.T) {
	// /a/b and /a/b/ MUST be treated as the same worktree;
	// filepath.Clean handles trailing slashes + redundant separators.
	got := WorkspaceCollisions([]BeadTarget{
		{BeadID: "gm-1", WorktreePath: "/a/b/"},
		{BeadID: "gm-2", WorktreePath: "/a/b"},
		{BeadID: "gm-3", WorktreePath: "/a/./b"},
	}, nil)
	// Three beads at the same canonical path → C(3,2) = 3 edges.
	if len(got) != 3 {
		t.Errorf("got %d edges, want 3: %+v", len(got), got)
	}
}

func TestWorkspaceCollisions_BothRelationsNamed(t *testing.T) {
	got := WorkspaceCollisions([]BeadTarget{
		{BeadID: "gm-1", Repository: "gemba", Branch: "main", WorktreePath: "/wt/x"},
		{BeadID: "gm-2", Repository: "gemba", Branch: "main", WorktreePath: "/wt/x"},
	}, nil)
	if len(got) != 1 {
		t.Fatalf("got %d edges, want 1", len(got))
	}
	want := "same repo+branch + same worktree_path"
	if got[0].Reason != want {
		t.Errorf("reason: %q, want %q", got[0].Reason, want)
	}
}

func TestWorkspaceCollisions_LiveSessionFlagsReadyBead(t *testing.T) {
	live := []OperationalContext{{
		Session:   &core.Session{ID: "sess-active"},
		Workspace: &core.Workspace{Kind: core.WorkspaceWorktree, Repository: "gemba", Branch: "main"},
	}}
	got := WorkspaceCollisions([]BeadTarget{
		{BeadID: "gm-1", Repository: "gemba", Branch: "main"},
	}, live)
	if len(got) != 1 {
		t.Fatalf("got %d edges, want 1: %+v", len(got), got)
	}
	if got[0].LiveSessionID != "sess-active" {
		t.Errorf("LiveSessionID: %q", got[0].LiveSessionID)
	}
	if got[0].A != "" {
		t.Errorf("A should be empty for bead↔live edges, got %q", got[0].A)
	}
	if got[0].B != "gm-1" {
		t.Errorf("B should be the bead id, got %q", got[0].B)
	}
	if got[0].Reason != "live session: same repo+branch" {
		t.Errorf("reason: %q", got[0].Reason)
	}
}

func TestWorkspaceCollisions_LiveSessionWithLegacyMetadataMatches(t *testing.T) {
	// A live session whose adaptor still stores the worktree under
	// ProviderMetadata["worktree"] (native pre-gm-s47n.2.6) MUST
	// still match — WorkspaceWorktreePath has the fallback wired.
	live := []OperationalContext{{
		Session: &core.Session{ID: "sess-legacy"},
		Workspace: &core.Workspace{
			Kind:             core.WorkspaceWorktree,
			ProviderMetadata: map[string]any{"worktree": "/legacy/wt"},
		},
	}}
	got := WorkspaceCollisions([]BeadTarget{
		{BeadID: "gm-1", WorktreePath: "/legacy/wt"},
	}, live)
	if len(got) != 1 {
		t.Fatalf("got %d edges, want 1: %+v", len(got), got)
	}
	if got[0].LiveSessionID != "sess-legacy" {
		t.Errorf("legacy worktree fallback didn't fire: %+v", got[0])
	}
}

func TestWorkspaceCollisions_LiveSessionWithoutWorkspaceSkipped(t *testing.T) {
	// A live session that hasn't fully materialised (Workspace is
	// nil) MUST NOT crash or emit edges.
	live := []OperationalContext{
		{Session: &core.Session{ID: "sess-booting"}},                      // no Workspace
		{Workspace: &core.Workspace{Repository: "gemba", Branch: "main"}}, // no Session
	}
	got := WorkspaceCollisions([]BeadTarget{
		{BeadID: "gm-1", Repository: "gemba", Branch: "main"},
	}, live)
	if len(got) != 0 {
		t.Errorf("partial live contexts produced edges: %+v", got)
	}
}

func TestWorkspaceCollisions_DeterministicOrder(t *testing.T) {
	// Same input twice MUST yield byte-identical output. The
	// .4.3 composer relies on this for change detection.
	beads := []BeadTarget{
		{BeadID: "gm-c", Repository: "gemba", Branch: "main"},
		{BeadID: "gm-a", Repository: "gemba", Branch: "main"},
		{BeadID: "gm-b", Repository: "gemba", Branch: "main"},
	}
	live := []OperationalContext{
		{Session: &core.Session{ID: "z"}, Workspace: &core.Workspace{Repository: "gemba", Branch: "main"}},
		{Session: &core.Session{ID: "a"}, Workspace: &core.Workspace{Repository: "gemba", Branch: "main"}},
	}
	first := WorkspaceCollisions(beads, live)
	second := WorkspaceCollisions(beads, live)
	if len(first) != len(second) {
		t.Fatalf("non-deterministic length: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Errorf("non-deterministic edge at %d: %+v vs %+v", i, first[i], second[i])
		}
	}
	// 3 bead↔bead pairs C(3,2) + 2 live sessions × 3 ready beads
	// = 9 edges.
	if len(first) != 9 {
		t.Fatalf("got %d edges, want 9", len(first))
	}
	// Bead↔bead edges first (LiveSessionID==""), then bead↔live in
	// LiveSessionID order ("a" before "z").
	wantOrder := []string{"", "", "", "a", "a", "a", "z", "z", "z"}
	for i, want := range wantOrder {
		if first[i].LiveSessionID != want {
			t.Errorf("edge %d: LiveSessionID=%q, want %q (%+v)", i, first[i].LiveSessionID, want, first[i])
		}
	}
}
