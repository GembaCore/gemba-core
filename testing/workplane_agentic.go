// Conformance Groups G–K probes (gm-l26). Each probe is
// capability-gated against the R1–R8 manifest shape (gm-ekr) AND
// fixture-hook-gated: an adaptor that declares the capability but
// doesn't supply the matching hook gets a clear "hook required"
// skip rather than a bogus failure. Adaptor authors opt into each
// group by supplying the corresponding fixture hook.
//
// The probes here drive adaptor-specific behaviour through the hook
// interfaces defined on WorkPlaneFixture. The harness does the
// gating + result accounting; the adaptor does the scenario work.

package gembatesting

import (
	"context"
	"fmt"
	"sync"

	"github.com/MikeBengtson/gemba/core"
)

// --- Group G: dep-graph evolution (R3) ----------------------------

func probeReadySetGraphEvolution(t probeT, impl core.WorkPlane, fixture *WorkPlaneFixture) {
	t.Helper()
	if fixture.ReadySetGraphEvolution == nil {
		t.Errorf("fixture.ReadySetGraphEvolution required (manifest declares ready_set_query + dependency_graph_native)")
		return
	}
	if err := fixture.ReadySetGraphEvolution(impl); err != nil {
		t.Errorf("ready-set graph evolution: %v", err)
	}
}

func probeDiscoveredFromMidExecution(t probeT, impl core.WorkPlane, fixture *WorkPlaneFixture) {
	t.Helper()
	if fixture.DiscoveredFromMidExecution == nil {
		t.Errorf("fixture.DiscoveredFromMidExecution required (adaptor declares beads:discovered_from edge extension)")
		return
	}
	if err := fixture.DiscoveredFromMidExecution(impl); err != nil {
		t.Errorf("discovered_from mid-execution: %v", err)
	}
}

// --- Group H: versioned transport (R4) ----------------------------

func probeVersionedStateRoundTrip(t probeT, impl core.WorkPlane, fixture *WorkPlaneFixture, transport core.VersioningTransport) {
	t.Helper()
	if fixture.VersionedStateRoundTrip == nil {
		t.Errorf("fixture.VersionedStateRoundTrip required (manifest declares versioning_transport=%q)", transport)
		return
	}
	if err := fixture.VersionedStateRoundTrip(impl, transport); err != nil {
		t.Errorf("versioned state round-trip (%s): %v", transport, err)
	}
}

func probeBranchMergeRoundTrip(t probeT, impl core.WorkPlane, fixture *WorkPlaneFixture, transport core.VersioningTransport) {
	t.Helper()
	if fixture.BranchMergeRoundTrip == nil {
		t.Errorf("fixture.BranchMergeRoundTrip required (manifest declares versioning_transport=%q)", transport)
		return
	}
	if err := fixture.BranchMergeRoundTrip(impl, transport); err != nil {
		t.Errorf("branch-merge round-trip (%s): %v", transport, err)
	}
}

// --- Group I: concurrency stress (R5) -----------------------------

// defaultConcurrencyStressN is the concurrent-writer count the
// harness attempts when the fixture doesn't cap it. 16 matches
// gm-l26's 'N=16' default.
const defaultConcurrencyStressN = 16

func probeConcurrentWriterStress(t probeT, impl core.WorkPlane, fixture *WorkPlaneFixture) {
	t.Helper()
	if fixture.ConcurrencyStressN != nil {
		// Adaptor-supplied stress runner: defer to the adaptor's own
		// idea of "legal stress load" — it may know about rate limits
		// or transactional boundaries the harness doesn't.
		ran, err := fixture.ConcurrencyStressN(impl, defaultConcurrencyStressN)
		if err != nil {
			t.Errorf("concurrent writer stress (N=%d): %v", ran, err)
		}
		return
	}
	// Fallback: harness-driven stress via the public surface. CreateWorkItem
	// is the narrowest surface every adaptor supports, so N concurrent
	// creates is a portable probe. A merge-based concurrency model
	// (mvcc, git-merge, dolt-merge) MUST produce N distinct items; an
	// optimistic model may fail some — those are surfaced as a warning
	// rather than a failure, on the theory that the adaptor's declared
	// model is the source of truth.
	ctx := context.Background()
	var wg sync.WaitGroup
	errs := make([]error, defaultConcurrencyStressN)
	for i := 0; i < defaultConcurrencyStressN; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := impl.CreateWorkItem(ctx, core.WorkItem{
				Kind:          "task",
				Title:         fmt.Sprintf("conformance-stress-%02d", i),
				Status:        "open",
				StateCategory: core.StateUnstarted,
			})
			errs[i] = err
		}(i)
	}
	wg.Wait()
	fails := 0
	for _, err := range errs {
		if err != nil {
			fails++
		}
	}
	if fails > 0 {
		t.Errorf("concurrent writer stress N=%d: %d/%d creates failed; "+
			"concurrency_model declares non-optimistic semantics but the "+
			"adaptor dropped writes under contention",
			defaultConcurrencyStressN, fails, defaultConcurrencyStressN)
	}
}

func probeReadAfterWriteCrossWriter(t probeT, impl core.WorkPlane, fixture *WorkPlaneFixture) {
	t.Helper()
	if fixture.ReadAfterWriteCrossWriter == nil {
		t.Errorf("fixture.ReadAfterWriteCrossWriter required (manifest declares concurrency_model)")
		return
	}
	if err := fixture.ReadAfterWriteCrossWriter(impl); err != nil {
		t.Errorf("read-after-write cross-writer: %v", err)
	}
}

// --- Group J: session decoupling (R6) -----------------------------

func probeSessionDeathRecovery(t probeT, impl core.WorkPlane, fixture *WorkPlaneFixture) {
	t.Helper()
	if fixture.SessionDeathRecovery == nil {
		t.Errorf("fixture.SessionDeathRecovery required (manifest declares agent_session_decoupling)")
		return
	}
	if err := fixture.SessionDeathRecovery(impl); err != nil {
		t.Errorf("session death recovery: %v", err)
	}
}

func probeWorkPickupBySecondAgent(t probeT, impl core.WorkPlane, fixture *WorkPlaneFixture) {
	t.Helper()
	if fixture.WorkPickupBySecondAgent == nil {
		t.Errorf("fixture.WorkPickupBySecondAgent required (manifest declares agent_session_decoupling)")
		return
	}
	if err := fixture.WorkPickupBySecondAgent(impl); err != nil {
		t.Errorf("work pickup by second agent: %v", err)
	}
}

// --- Group K: orchestrator hooks (R8) -----------------------------
// Each hook is individually gated against the OrchestratorHooks set.

func probeReadySetSubscribeLatency(t probeT, impl core.WorkPlane, fixture *WorkPlaneFixture) {
	t.Helper()
	if fixture.ReadySetSubscribeLatency == nil {
		t.Errorf("fixture.ReadySetSubscribeLatency required (manifest declares ready-set-subscribe)")
		return
	}
	if err := fixture.ReadySetSubscribeLatency(impl); err != nil {
		t.Errorf("ready-set subscribe latency: %v", err)
	}
}

func probeClaimAtomic(t probeT, impl core.WorkPlane, fixture *WorkPlaneFixture) {
	t.Helper()
	if fixture.ClaimAtomic == nil {
		t.Errorf("fixture.ClaimAtomic required (manifest declares claim-atomic)")
		return
	}
	if err := fixture.ClaimAtomic(impl); err != nil {
		t.Errorf("claim atomic: %v", err)
	}
}

func probeEscalationIngestRoundTrip(t probeT, impl core.WorkPlane, fixture *WorkPlaneFixture) {
	t.Helper()
	if fixture.EscalationIngestRoundTrip == nil {
		t.Errorf("fixture.EscalationIngestRoundTrip required (manifest declares escalation-ingest)")
		return
	}
	if err := fixture.EscalationIngestRoundTrip(impl); err != nil {
		t.Errorf("escalation ingest round-trip: %v", err)
	}
}

func probeWorkCompleteAck(t probeT, impl core.WorkPlane, fixture *WorkPlaneFixture) {
	t.Helper()
	if fixture.WorkCompleteAck == nil {
		t.Errorf("fixture.WorkCompleteAck required (manifest declares work-complete-ack)")
		return
	}
	if err := fixture.WorkCompleteAck(impl); err != nil {
		t.Errorf("work complete ack: %v", err)
	}
}

func probePoolBulkDispatch(t probeT, impl core.WorkPlane, fixture *WorkPlaneFixture) {
	t.Helper()
	if fixture.PoolBulkDispatch == nil {
		t.Errorf("fixture.PoolBulkDispatch required (manifest declares pool-bulk-dispatch)")
		return
	}
	if err := fixture.PoolBulkDispatch(impl); err != nil {
		t.Errorf("pool bulk dispatch: %v", err)
	}
}

// --- helpers ------------------------------------------------------

// manifestOrZero reads the manifest once for gating. Errors produce a
// zero manifest — downstream capability checks then all skip, which
// is the correct behaviour for a describe that failed earlier Group A.
func manifestOrZero(impl core.WorkPlane) core.CapabilityManifest {
	m, err := impl.Describe(context.Background())
	if err != nil {
		return core.CapabilityManifest{}
	}
	return m
}

func hasEdgeExtension(m core.CapabilityManifest, name string) bool {
	for _, e := range m.EdgeExtensions {
		if e.Name == name {
			return true
		}
	}
	return false
}

func containsOrchestratorHook(m core.CapabilityManifest, want core.OrchestratorHook) bool {
	for _, h := range m.OrchestratorHooks {
		if h == want {
			return true
		}
	}
	return false
}
