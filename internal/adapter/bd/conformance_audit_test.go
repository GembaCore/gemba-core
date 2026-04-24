package bd_test

import (
	"strings"
	"testing"

	"github.com/MikeBengtson/gemba/internal/adapter/bd"
	"github.com/MikeBengtson/gemba/internal/core"
	gembatesting "github.com/MikeBengtson/gemba/testing"
)

// TestBeadsConformanceR1R8Audit is the programmatic gm-zdx audit:
// runs every conformance group (A–K) against the bd adaptor's
// projected R1–R8 shape (gm-ekr) and pins each group's expected
// state so any drift surfaces immediately.
//
// The bd test harness is an in-process fake — not a live Dolt + bd
// stack. Group probes that require a real substrate (versioned
// transport round-trip, real concurrency stress against Dolt,
// session-death recovery, subscribe latency) land as Skipped with a
// fixture-hook-required message. Those gaps get filed as child
// beads under gm-e6 and picked up once the live-bd test harness
// exists.
//
// This test IS the audit deliverable: a single Go run produces a
// precise ledger of which groups are green, which are skipped (and
// why), and which would fail.
func TestBeadsConformanceR1R8Audit(t *testing.T) {
	impl := bd.NewWorkPlaneWithRunner(newFakeBd(t).run, "")
	report := gembatesting.RunWorkPlaneProbes(impl, &gembatesting.WorkPlaneFixture{
		KnownMissingID: core.WorkItemID("gemba/gemba/gm-does-not-exist"),
	})

	// Known gaps (pre-existing, filed as child beads under gm-e6 in the
	// gm-zdx audit close). A probe name that appears here is allowed
	// to be Failed without breaking the audit; the test still flags
	// drift if a probe fails that ISN'T on this list, or if a probe
	// on this list unexpectedly starts passing (meaning the gap
	// closed and the ledger should be updated).
	knownGaps := map[string]string{
		"B_update_patch_applies":  "bd CreateWorkItem ignores caller-supplied ID; UpdateWorkItem against that id is not-found (gap filed under gm-e6)",
		"B_list_returns_created":  "bd CreateWorkItem ignores caller-supplied ID; ListWorkItems can't match it (gap filed under gm-e6)",
	}

	byName := map[string]gembatesting.GroupResult{}
	for _, g := range report.Groups {
		byName[groupLetter(g.Name)] = g
	}

	for _, letter := range []string{"A", "B", "C", "D", "E", "F", "G", "H", "I", "J", "K"} {
		g, ok := byName[letter]
		if !ok {
			t.Errorf("group %s missing from report", letter)
			continue
		}
		for _, p := range g.Probes {
			if p.Passed || p.Skipped {
				// Drift check: a known gap that starts passing means the
				// ledger is stale. Close the gap bead + remove the entry
				// from knownGaps.
				if _, gap := knownGaps[p.Name]; gap && p.Passed {
					t.Errorf("known-gap probe %q now passes; update gm-zdx ledger + close the gap bead",
						p.Name)
				}
				continue
			}
			if _, gap := knownGaps[p.Name]; !gap {
				t.Errorf("group %s probe %q failed and is NOT a tracked gap: %v",
					letter, p.Name, p.Messages)
			}
		}
	}

	// Pin the audit projection (gm-zdx):
	//  G  — active (R3 declared). Probes Skipped pending fixtures.
	//  H  — active (R4 declares git/dolt/jsonl). Probes Skipped.
	//  I  — active (R5 = dolt-merge). Harness-driven stress probe
	//       runs against the fake bd; read-after-write Skipped.
	//  J  — active (R6 = true). Probes Skipped pending fixtures.
	//  K  — active (R8 declares four hooks). Probes Skipped.
	for _, letter := range []string{"G", "H", "I", "J", "K"} {
		g := byName[letter]
		if g.NotApplicable {
			t.Errorf("group %s marked NotApplicable; bd manifest should advertise the capability (projection gm-zdx)", letter)
		}
	}

	// Every R1–R8 capability the bd manifest advertises should have a
	// corresponding set of probes in Group K.
	gK := byName["K"]
	wantHookProbes := map[string]bool{
		"K_ready_set_subscribe_latency":   false,
		"K_claim_atomic":                  false,
		"K_escalation_ingest_round_trip":  false,
		"K_work_complete_ack":             false,
	}
	for _, p := range gK.Probes {
		if _, ok := wantHookProbes[p.Name]; ok {
			wantHookProbes[p.Name] = true
		}
	}
	for name, seen := range wantHookProbes {
		if !seen {
			t.Errorf("expected Group K probe %q (manifest declares the hook)", name)
		}
	}

	// pool-bulk-dispatch is NOT in the bd manifest today; gm-zdx filed
	// that as a gap. Assert its probe is absent so a silent addition
	// without filing a gap bead trips a test.
	for _, p := range gK.Probes {
		if p.Name == "K_pool_bulk_dispatch" {
			t.Error("K_pool_bulk_dispatch surfaced in Group K but bd manifest does not declare the hook — update gm-zdx audit")
		}
	}
}

// groupLetter extracts the leading letter of a group Name like
// "G: R3 dep-graph evolution" → "G".
func groupLetter(name string) string {
	if len(name) == 0 {
		return ""
	}
	// Name format: "<letter>[:<space><description>]" — take until the
	// first colon or whitespace.
	i := strings.IndexAny(name, ": ")
	if i < 0 {
		return name
	}
	return name[:i]
}
