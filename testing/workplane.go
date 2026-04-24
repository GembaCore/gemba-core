package gembatesting

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/MikeBengtson/gemba/internal/core"
)

// WorkPlaneFixture carries the optional state some conformance probes
// need to run. Leave any field zero to skip the corresponding probe —
// fixture-independent probes (manifest validity, JSON round-trip,
// capability denial) still run regardless.
type WorkPlaneFixture struct {
	// KnownMissingID is a WorkItemID the adaptor is guaranteed NOT to
	// have. The Group F not-found probe calls GetWorkItem(id) and asserts
	// the error is a tagged *core.AdaptorError that also satisfies
	// errors.Is(err, core.ErrNotFound).
	KnownMissingID core.WorkItemID

	// SeedWorkItemWithEdges is an adaptor-supplied hook that stages one
	// WorkItem with a known-good Relationships slice inside the backend
	// and returns its id plus the expected relationships. The Group C
	// round-trip probe drives GetWorkItem(id) and checks the returned
	// relationships match the declaration. Leave nil when the adaptor
	// has no seed hook; the probe is skipped rather than failing.
	SeedWorkItemWithEdges func(impl core.WorkPlane) (core.WorkItemID, []core.Relationship, error)

	// --- gm-l26 Groups G–K fixture hooks ---------------------------
	// Each hook is optional. A nil hook, or a capability the manifest
	// does not advertise, skips the corresponding probe (it does not
	// fail). When the manifest advertises the capability AND the hook
	// is nil, the probe surfaces an explanatory skip so adaptor
	// authors know how to opt in.

	// ReadySetGraphEvolution — Group G (R3). Adaptor drives a
	// three-step scenario: stage two items A → (blocked by) B; observe
	// that B is in the ready-set and A is not; unblock A (close B or
	// remove the edge); observe A moves into the ready-set. Any
	// deviation returns a non-nil error. Gate:
	// DependencyGraphNative && ReadySetQuery.
	ReadySetGraphEvolution func(impl core.WorkPlane) error

	// DiscoveredFromMidExecution — Group G (R3, beads-specific). An
	// adaptor that declares an EdgeExtension named
	// "beads:discovered_from" stages a "discovered" item mid-execution
	// and asserts the new edge is visible in the parent's read view.
	// Gate: edge extension declared + hook non-nil.
	DiscoveredFromMidExecution func(impl core.WorkPlane) error

	// VersionedStateRoundTrip — Group H (R4). Adaptor exports state
	// via the declared transport, imports it into a fresh instance,
	// and asserts the round-tripped rows match. Runs once per
	// declared VersioningTransport entry that is not "none". Gate:
	// versioning_transport contains at least one real transport.
	VersionedStateRoundTrip func(impl core.WorkPlane, transport core.VersioningTransport) error

	// BranchMergeRoundTrip — Group H (R4). For transports that carry
	// branch semantics (git / dolt), the hook exercises a three-way
	// merge round-trip. Gate: versioning_transport contains git or
	// dolt.
	BranchMergeRoundTrip func(impl core.WorkPlane, transport core.VersioningTransport) error

	// ConcurrencyStressN — Group I (R5). Default N=16 concurrent
	// writers; the hook MAY use a smaller N and report it via the
	// returned int. Returns (N actually run, error). Gate:
	// concurrency_model is declared and not "optimistic".
	ConcurrencyStressN func(impl core.WorkPlane, n int) (int, error)

	// ReadAfterWriteCrossWriter — Group I (R5). Writes from goroutine
	// A, reads from goroutine B, asserts visibility within the
	// adaptor-declared event-latency budget.
	ReadAfterWriteCrossWriter func(impl core.WorkPlane) error

	// SessionDeathRecovery — Group J (R6). Simulates agent session
	// death mid-write; confirms state lands in the store. Gate:
	// agent_session_decoupling.
	SessionDeathRecovery func(impl core.WorkPlane) error

	// WorkPickupBySecondAgent — Group J (R6). First agent claims a
	// work item then disappears; second agent force-steals and
	// completes it. Gate: agent_session_decoupling.
	WorkPickupBySecondAgent func(impl core.WorkPlane) error

	// ReadySetSubscribeLatency — Group K (R8). Subscribe to ready-set
	// transitions; assert subscription receives the right events
	// within the adaptor-declared latency budget. Gate:
	// orchestrator_hooks ∋ ready-set-subscribe.
	ReadySetSubscribeLatency func(impl core.WorkPlane) error

	// ClaimAtomic — Group K (R8). Two concurrent claims on the same
	// item; exactly one succeeds, the other receives a typed conflict.
	// Gate: orchestrator_hooks ∋ claim-atomic.
	ClaimAtomic func(impl core.WorkPlane) error

	// EscalationIngestRoundTrip — Group K (R8). Adaptor accepts a
	// structured escalation and reads it back unchanged. Gate:
	// orchestrator_hooks ∋ escalation-ingest.
	EscalationIngestRoundTrip func(impl core.WorkPlane) error

	// WorkCompleteAck — Group K (R8). Adaptor round-trips a
	// work-complete write with an ack payload. Gate:
	// orchestrator_hooks ∋ work-complete-ack.
	WorkCompleteAck func(impl core.WorkPlane) error

	// PoolBulkDispatch — Group K (R8). N work items dispatched to a
	// pool of agents in one round-trip. Gate: orchestrator_hooks ∋
	// pool-bulk-dispatch.
	PoolBulkDispatch func(impl core.WorkPlane) error
}

// RunWorkPlaneConformance runs the WorkPlane contract probes against
// impl (see package doc for group breakdown). Each group is a t.Run
// subtest so failures point directly at the offending probe; callers
// should invoke this from a top-level TestXxxConformance and let the
// subtest names surface in go test -v output.
//
// fixture may be nil; that is equivalent to passing a zero-value
// WorkPlaneFixture — fixture-independent probes still run.
//
// Usage:
//
//	func TestBeadsConformance(t *testing.T) {
//	    impl := bd.New(ctx, cfg)
//	    gembatesting.RunWorkPlaneConformance(t, impl, &gembatesting.WorkPlaneFixture{
//	        KnownMissingID: "gemba/gemba/gm-does-not-exist",
//	    })
//	}
func RunWorkPlaneConformance(t *testing.T, impl core.WorkPlane, fixture *WorkPlaneFixture) {
	t.Helper()
	if impl == nil {
		t.Fatal("gembatesting: RunWorkPlaneConformance called with nil WorkPlane")
	}
	if fixture == nil {
		fixture = &WorkPlaneFixture{}
	}

	t.Run("A_describe_returns_valid_manifest", func(t *testing.T) {
		probeDescribeManifestValid(t, impl)
	})
	t.Run("A_manifest_round_trips_json", func(t *testing.T) {
		probeManifestJSONRoundTrip(t, impl)
	})
	t.Run("A_describe_is_idempotent", func(t *testing.T) {
		probeDescribeIdempotent(t, impl)
	})
	t.Run("C_edge_extensions_are_structurally_valid", func(t *testing.T) {
		probeEdgeExtensionsAreStructurallyValid(t, impl)
	})
	if fixture.SeedWorkItemWithEdges != nil {
		t.Run("C_edge_round_trip_get_work_item", func(t *testing.T) {
			probeEdgeRoundTripWorkItem(t, impl, fixture)
		})
	}
	t.Run("E_capability_denial_matches_manifest", func(t *testing.T) {
		probeCapabilityDenialMatchesManifest(t, impl)
	})
	if fixture.KnownMissingID != "" {
		t.Run("F_not_found_is_tagged_adaptor_error", func(t *testing.T) {
			probeNotFoundIsTagged(t, impl, fixture.KnownMissingID)
		})
	}

	// --- gm-l26 Groups G–K -------------------------------------------
	// Each probe is capability-gated against the manifest's R1–R8 shape
	// and fixture-gated against the hook it needs. Absence of either
	// skips (t.Skip) rather than fails.
	m := manifestOrZero(impl)

	// Group G — R3 dep-graph evolution.
	if m.DependencyGraphNative && m.ReadySetQuery {
		t.Run("G_ready_set_graph_evolution", func(t *testing.T) {
			if fixture.ReadySetGraphEvolution == nil {
				t.Skip("fixture.ReadySetGraphEvolution not provided")
			}
			probeReadySetGraphEvolution(t, impl, fixture)
		})
	}
	if hasEdgeExtension(m, "beads:discovered_from") {
		t.Run("G_discovered_from_mid_execution", func(t *testing.T) {
			if fixture.DiscoveredFromMidExecution == nil {
				t.Skip("fixture.DiscoveredFromMidExecution not provided")
			}
			probeDiscoveredFromMidExecution(t, impl, fixture)
		})
	}

	// Group H — R4 versioned transport. One probe per declared
	// transport; branch-merge only runs for git/dolt.
	for _, vt := range m.VersioningTransport {
		if vt == core.VersioningNone {
			continue
		}
		vt := vt
		t.Run("H_versioned_state_round_trip_"+string(vt), func(t *testing.T) {
			if fixture.VersionedStateRoundTrip == nil {
				t.Skip("fixture.VersionedStateRoundTrip not provided")
			}
			probeVersionedStateRoundTrip(t, impl, fixture, vt)
		})
		if vt == core.VersioningGit || vt == core.VersioningDolt {
			t.Run("H_branch_merge_round_trip_"+string(vt), func(t *testing.T) {
				if fixture.BranchMergeRoundTrip == nil {
					t.Skip("fixture.BranchMergeRoundTrip not provided")
				}
				probeBranchMergeRoundTrip(t, impl, fixture, vt)
			})
		}
	}

	// Group I — R5 concurrency stress.
	if m.ConcurrencyModel != "" && m.ConcurrencyModel != core.ConcurrencyOptimistic {
		t.Run("I_concurrent_writer_stress_N16", func(t *testing.T) {
			probeConcurrentWriterStress(t, impl, fixture)
		})
		t.Run("I_read_after_write_cross_writer", func(t *testing.T) {
			if fixture.ReadAfterWriteCrossWriter == nil {
				t.Skip("fixture.ReadAfterWriteCrossWriter not provided")
			}
			probeReadAfterWriteCrossWriter(t, impl, fixture)
		})
	}

	// Group J — R6 session decoupling.
	if m.AgentSessionDecoupling {
		t.Run("J_session_death_recovery", func(t *testing.T) {
			if fixture.SessionDeathRecovery == nil {
				t.Skip("fixture.SessionDeathRecovery not provided")
			}
			probeSessionDeathRecovery(t, impl, fixture)
		})
		t.Run("J_work_pickup_by_second_agent", func(t *testing.T) {
			if fixture.WorkPickupBySecondAgent == nil {
				t.Skip("fixture.WorkPickupBySecondAgent not provided")
			}
			probeWorkPickupBySecondAgent(t, impl, fixture)
		})
	}

	// Group K — R8 orchestrator hooks, per-entry gating.
	if containsOrchestratorHook(m, core.HookReadySetSubscribe) {
		t.Run("K_ready_set_subscribe_latency", func(t *testing.T) {
			if fixture.ReadySetSubscribeLatency == nil {
				t.Skip("fixture.ReadySetSubscribeLatency not provided")
			}
			probeReadySetSubscribeLatency(t, impl, fixture)
		})
	}
	if containsOrchestratorHook(m, core.HookClaimAtomic) {
		t.Run("K_claim_atomic", func(t *testing.T) {
			if fixture.ClaimAtomic == nil {
				t.Skip("fixture.ClaimAtomic not provided")
			}
			probeClaimAtomic(t, impl, fixture)
		})
	}
	if containsOrchestratorHook(m, core.HookEscalationIngest) {
		t.Run("K_escalation_ingest_round_trip", func(t *testing.T) {
			if fixture.EscalationIngestRoundTrip == nil {
				t.Skip("fixture.EscalationIngestRoundTrip not provided")
			}
			probeEscalationIngestRoundTrip(t, impl, fixture)
		})
	}
	if containsOrchestratorHook(m, core.HookWorkCompleteAck) {
		t.Run("K_work_complete_ack", func(t *testing.T) {
			if fixture.WorkCompleteAck == nil {
				t.Skip("fixture.WorkCompleteAck not provided")
			}
			probeWorkCompleteAck(t, impl, fixture)
		})
	}
	if containsOrchestratorHook(m, core.HookPoolBulkDispatch) {
		t.Run("K_pool_bulk_dispatch", func(t *testing.T) {
			if fixture.PoolBulkDispatch == nil {
				t.Skip("fixture.PoolBulkDispatch not provided")
			}
			probePoolBulkDispatch(t, impl, fixture)
		})
	}
}

func probeDescribeManifestValid(t probeT, impl core.WorkPlane) {
	t.Helper()
	m, err := impl.Describe(context.Background())
	if err != nil {
		t.Fatalf("Describe: unexpected error: %v", err)
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("manifest.Validate: %v", err)
	}
	if m.ProtocolVersion != core.ProtocolVersion {
		t.Errorf("manifest.ProtocolVersion=%q, want %q (update adaptor or bump core.ProtocolVersion)",
			m.ProtocolVersion, core.ProtocolVersion)
	}
}

func probeManifestJSONRoundTrip(t probeT, impl core.WorkPlane) {
	t.Helper()
	m, err := impl.Describe(context.Background())
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	var round core.CapabilityManifest
	if err := json.Unmarshal(data, &round); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	if !reflect.DeepEqual(m, round) {
		t.Errorf("manifest JSON round-trip drift:\n  orig:  %+v\n  round: %+v", m, round)
	}
}

func probeDescribeIdempotent(t probeT, impl core.WorkPlane) {
	t.Helper()
	ctx := context.Background()
	first, err := impl.Describe(ctx)
	if err != nil {
		t.Fatalf("Describe (1): %v", err)
	}
	second, err := impl.Describe(ctx)
	if err != nil {
		t.Fatalf("Describe (2): %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Errorf("Describe is not idempotent:\n  first:  %+v\n  second: %+v", first, second)
	}
}

// probeCapabilityDenialMatchesManifest asserts the adaptor-side
// fail-fast requirement from gm-4qf: for each gated op the manifest
// opts out of, calling the corresponding method directly on impl MUST
// return a tagged capability_denied *core.AdaptorError. The port-level
// core.GuardedWorkPlane is the primary gate, but the adaptor is the
// last line of defense (docs/adaptors/workplane.md §Adaptor-side
// fail-fast).
func probeCapabilityDenialMatchesManifest(t probeT, impl core.WorkPlane) {
	t.Helper()
	ctx := context.Background()
	m, err := impl.Describe(ctx)
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}

	type gatedCall struct {
		op   core.Operation
		name string
		call func() error
	}
	calls := []gatedCall{
		{
			op:   core.OpAttachEvidence,
			name: "AttachEvidence",
			call: func() error {
				return impl.AttachEvidence(ctx, core.WorkItemID("gemba/gemba/gm-conformance-probe"), core.Evidence{})
			},
		},
		{
			op:   core.OpListSprints,
			name: "ListSprints",
			call: func() error {
				_, err := impl.ListSprints(ctx)
				return err
			},
		},
		{
			op:   core.OpReadBudgetRollup,
			name: "ReadBudgetRollup",
			call: func() error {
				_, err := impl.ReadBudgetRollup(ctx, "conformance-probe-sprint")
				return err
			},
		},
	}

	for _, gc := range calls {
		d := core.CheckCapability(m, gc.op)
		if d.Allowed {
			// Manifest permits the op; no capability_denied is expected
			// and we can't synthesize a meaningful test payload for every
			// adaptor, so skip.
			continue
		}
		err := gc.call()
		if assertErr := core.AssertCapabilityDenied(err); assertErr != nil {
			t.Errorf("%s (op=%s) denied by manifest but adaptor did not fail fast: %v",
				gc.name, gc.op, assertErr)
		}
	}
}

func probeNotFoundIsTagged(t probeT, impl core.WorkPlane, missing core.WorkItemID) {
	t.Helper()
	_, err := impl.GetWorkItem(context.Background(), missing)
	if err == nil {
		t.Fatalf("GetWorkItem(%q) returned nil error; fixture.KnownMissingID must name an id the adaptor does not have",
			missing)
	}
	if assertErr := core.AssertAdaptorError(err); assertErr != nil {
		t.Errorf("GetWorkItem(%q): %v", missing, assertErr)
	}
	if !errors.Is(err, core.ErrNotFound) {
		t.Errorf("GetWorkItem(%q): errors.Is(err, core.ErrNotFound) = false; "+
			"tagged AdaptorError must use Kind=session_not_found so the legacy sentinel matches",
			missing)
	}
}
