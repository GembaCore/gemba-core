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
	t.Run("E_capability_denial_matches_manifest", func(t *testing.T) {
		probeCapabilityDenialMatchesManifest(t, impl)
	})
	if fixture.KnownMissingID != "" {
		t.Run("F_not_found_is_tagged_adaptor_error", func(t *testing.T) {
			probeNotFoundIsTagged(t, impl, fixture.KnownMissingID)
		})
	}
}

func probeDescribeManifestValid(t *testing.T, impl core.WorkPlane) {
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

func probeManifestJSONRoundTrip(t *testing.T, impl core.WorkPlane) {
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

func probeDescribeIdempotent(t *testing.T, impl core.WorkPlane) {
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
func probeCapabilityDenialMatchesManifest(t *testing.T, impl core.WorkPlane) {
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

func probeNotFoundIsTagged(t *testing.T, impl core.WorkPlane, missing core.WorkItemID) {
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
