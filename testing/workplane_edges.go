package gembatesting

import (
	"context"

	"github.com/MikeBengtson/gemba/internal/core"
)

// Group C: edge / relationship round-trip.
//
// The WorkPlane interface does not yet expose a first-class edge CRUD
// surface — core relationships ride on WorkItem.Relationships and
// extension edges ride through WorkItem.Custom. Group C checks the
// contract that every adaptor MUST uphold regardless: the declared
// EdgeExtensions are structurally valid, each extension name is
// namespaced so it can't collide with a core edge, and no two
// extensions share a name.
//
// The fixture-optional round-trip probe (probeEdgeRoundTripWorkItem)
// lets adaptors with seed hooks prove that a WorkItem hydrated with
// core Relationships survives Create → Get unchanged. Adaptors whose
// Create path can't accept Relationships skip this probe by leaving
// the fixture seeder empty.

// probeEdgeExtensionsAreStructurallyValid enforces the structural
// invariants every EdgeExtension in the manifest must satisfy:
//
//  1. Name is non-empty — the SPA routes on Name.
//  2. Name is not one of the three core RelationshipKinds — declaring
//     a core edge as an extension is almost always a typo and confuses
//     the UI fallback rule (unknown extensions render as relates_to).
//  3. No two extensions share a Name — duplicate declarations make the
//     capability-negotiation UI's mapping non-deterministic.
//  4. Inverse (if set) is not one of the core kinds for the same
//     reason core edges shouldn't appear as extension names.
func probeEdgeExtensionsAreStructurallyValid(t probeT, impl core.WorkPlane) {
	t.Helper()
	m, err := impl.Describe(context.Background())
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	coreEdges := map[string]struct{}{
		string(core.RelBlocks):      {},
		string(core.RelParentChild): {},
		string(core.RelRelatesTo):   {},
	}
	seen := make(map[string]struct{}, len(m.EdgeExtensions))
	for i, ext := range m.EdgeExtensions {
		if ext.Name == "" {
			t.Errorf("EdgeExtensions[%d]: Name is empty", i)
			continue
		}
		if _, hit := coreEdges[ext.Name]; hit {
			t.Errorf("EdgeExtensions[%d]: Name=%q collides with a core RelationshipKind — extensions MUST be non-core",
				i, ext.Name)
		}
		if _, dup := seen[ext.Name]; dup {
			t.Errorf("EdgeExtensions[%d]: Name=%q is declared more than once", i, ext.Name)
		}
		seen[ext.Name] = struct{}{}
		if ext.Inverse != "" {
			if _, hit := coreEdges[ext.Inverse]; hit {
				t.Errorf("EdgeExtensions[%d] (%q): Inverse=%q collides with a core RelationshipKind",
					i, ext.Name, ext.Inverse)
			}
		}
	}
}

// probeEdgeRoundTripWorkItem walks a bead produced by a fixture seeder
// through GetWorkItem and asserts the resulting WorkItem carries the
// relationships the fixture declared. Adaptors whose native store can't
// be seeded with edges from the test harness supply a nil seeder and
// the probe records a skip.
func probeEdgeRoundTripWorkItem(t probeT, impl core.WorkPlane, f *WorkPlaneFixture) {
	t.Helper()
	if f == nil || f.SeedWorkItemWithEdges == nil {
		t.Fatalf("edge round-trip requires WorkPlaneFixture.SeedWorkItemWithEdges; skipping")
	}
	ctx := context.Background()
	id, want, err := f.SeedWorkItemWithEdges(impl)
	if err != nil {
		t.Fatalf("SeedWorkItemWithEdges: %v", err)
	}
	got, err := impl.GetWorkItem(ctx, id)
	if err != nil {
		t.Fatalf("GetWorkItem(%q): %v", id, err)
	}
	if !relationshipMultisetEqual(got.Relationships, want) {
		t.Errorf("GetWorkItem(%q): relationships mismatch\n  got:  %+v\n  want: %+v",
			id, got.Relationships, want)
	}
}

// relationshipMultisetEqual compares two Relationship slices without
// caring about order. Duplicates are preserved: a mismatch in
// multiplicity fails the check.
func relationshipMultisetEqual(a, b []core.Relationship) bool {
	if len(a) != len(b) {
		return false
	}
	left := make(map[core.Relationship]int, len(a))
	for _, r := range a {
		left[r]++
	}
	for _, r := range b {
		left[r]--
		if left[r] < 0 {
			return false
		}
	}
	for _, n := range left {
		if n != 0 {
			return false
		}
	}
	return true
}
