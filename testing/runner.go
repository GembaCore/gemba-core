package gembatesting

import (
	"context"
	"time"

	"github.com/MikeBengtson/gemba/internal/core"
)

// Report is the machine-readable summary of a conformance run. It is the
// shape the `gemba adaptor test` CLI emits (gm-e3.5), and what third-
// party CI systems read when they call the programmatic runner directly
// rather than go test.
//
// One Report covers one plane (work or orchestration). A CLI run that
// tests both planes produces one Report per plane.
type Report struct {
	// Plane is "work" or "orchestration".
	Plane string `json:"plane"`
	// AdaptorName is the manifest-declared name (best-effort; empty if
	// Describe failed hard enough that no name is available).
	AdaptorName string `json:"adaptor_name,omitempty"`
	// AdaptorVersion is the manifest-declared version.
	AdaptorVersion string `json:"adaptor_version,omitempty"`
	// ProtocolVersion is what core.ProtocolVersion evaluated to at run
	// time; recorded so CI can correlate a failing run to a core pin.
	ProtocolVersion string `json:"protocol_version"`
	// StartedAt / FinishedAt bound the whole run for CI timing metrics.
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	// Groups is the A–F grouping defined in docs/adaptors/*.md.
	Groups []GroupResult `json:"groups"`
}

// GroupResult carries one conformance group's probes. Name is the group
// letter + short description (e.g. "A: describe + capability shape").
// NotApplicable=true means the group has no probes on this plane in the
// current release (e.g. WorkPlane has no edge interface yet, so Group C
// reports not-applicable).
type GroupResult struct {
	Name          string        `json:"name"`
	NotApplicable bool          `json:"not_applicable,omitempty"`
	Note          string        `json:"note,omitempty"`
	Probes        []ProbeResult `json:"probes,omitempty"`
}

// ProbeResult is one probe's outcome. Passed=false with messages is a
// failure; Skipped=true means a required fixture was missing (e.g. no
// KnownMissingID, so the not-found probe could not run).
type ProbeResult struct {
	Name     string   `json:"name"`
	Passed   bool     `json:"passed"`
	Skipped  bool     `json:"skipped,omitempty"`
	Messages []string `json:"messages,omitempty"`
}

// Passed reports whether every non-skipped probe across every group
// passed. Skipped probes do not count as failure (they document a
// fixture gap, not a contract violation).
func (r *Report) Passed() bool {
	for _, g := range r.Groups {
		for _, p := range g.Probes {
			if p.Skipped {
				continue
			}
			if !p.Passed {
				return false
			}
		}
	}
	return true
}

// Totals returns aggregate pass/fail/skip counts across the whole
// report. Convenient for CLI summary lines and JUnit <testsuites>
// attributes.
func (r *Report) Totals() (passed, failed, skipped int) {
	for _, g := range r.Groups {
		for _, p := range g.Probes {
			switch {
			case p.Skipped:
				skipped++
			case p.Passed:
				passed++
			default:
				failed++
			}
		}
	}
	return
}

// RunWorkPlaneProbes runs the WorkPlane conformance probes against impl
// and returns a structured Report. It is the programmatic counterpart
// to RunWorkPlaneConformance: same probes, same group layout, but no
// *testing.T dependency so the `gemba adaptor test` CLI can drive it.
//
// fixture may be nil. Probes that require fixture state record a
// ProbeResult{Skipped: true} rather than silently disappearing, so CI
// can tell a missing fixture from a passed probe.
func RunWorkPlaneProbes(impl core.WorkPlane, fixture *WorkPlaneFixture) *Report {
	r := &Report{
		Plane:           "work",
		ProtocolVersion: core.ProtocolVersion,
		StartedAt:       time.Now(),
	}
	if fixture == nil {
		fixture = &WorkPlaneFixture{}
	}
	if m, err := impl.Describe(context.Background()); err == nil {
		r.AdaptorName = m.AdaptorName
		r.AdaptorVersion = m.AdaptorVersion
	}

	groupA := GroupResult{Name: "A: describe + capability shape"}
	groupA.Probes = []ProbeResult{
		runProbe("A_describe_returns_valid_manifest", func(t probeT) { probeDescribeManifestValid(t, impl) }),
		runProbe("A_manifest_round_trips_json", func(t probeT) { probeManifestJSONRoundTrip(t, impl) }),
		runProbe("A_describe_is_idempotent", func(t probeT) { probeDescribeIdempotent(t, impl) }),
	}

	groupB := GroupResult{Name: "B: WorkItem CRUD round-trip"}
	groupB.Probes = []ProbeResult{
		runProbe("B_create_get_round_trip", func(t probeT) { probeWorkItemCreateGet(t, impl) }),
		runProbe("B_update_patch_applies", func(t probeT) { probeWorkItemUpdate(t, impl) }),
		runProbe("B_list_returns_created", func(t probeT) { probeWorkItemList(t, impl) }),
	}

	groupC := GroupResult{
		Name:          "C: edge / relationship round-trip",
		NotApplicable: true,
		Note:          "WorkPlane interface does not expose an edge/relationship API in this release",
	}

	groupD := GroupResult{
		Name:          "D: subscribe / event delivery",
		NotApplicable: true,
		Note:          "WorkPlane interface does not expose a Subscribe method; event delivery is an OrchestrationPlane concern",
	}

	groupE := GroupResult{Name: "E: capability-declared optional features"}
	groupE.Probes = []ProbeResult{
		runProbe("E_capability_denial_matches_manifest", func(t probeT) { probeCapabilityDenialMatchesManifest(t, impl) }),
	}

	groupF := GroupResult{Name: "F: error semantics"}
	if fixture.KnownMissingID != "" {
		groupF.Probes = append(groupF.Probes,
			runProbe("F_not_found_is_tagged_adaptor_error", func(t probeT) {
				probeNotFoundIsTagged(t, impl, fixture.KnownMissingID)
			}),
		)
	} else {
		groupF.Probes = append(groupF.Probes, ProbeResult{
			Name:     "F_not_found_is_tagged_adaptor_error",
			Skipped:  true,
			Messages: []string{"fixture.KnownMissingID not provided"},
		})
	}

	r.Groups = []GroupResult{groupA, groupB, groupC, groupD, groupE, groupF}
	r.FinishedAt = time.Now()
	return r
}

// RunOrchestrationProbes runs the OrchestrationPlane conformance probes
// against impl and returns a structured Report. Programmatic counterpart
// to RunOrchestrationConformance (gm-e3.5).
//
// fixture may be nil.
func RunOrchestrationProbes(impl core.OrchestrationPlaneAdaptor, fixture *OrchestrationFixture) *Report {
	r := &Report{
		Plane:           "orchestration",
		ProtocolVersion: core.ProtocolVersion,
		StartedAt:       time.Now(),
	}
	if fixture == nil {
		fixture = &OrchestrationFixture{}
	}
	m := impl.Describe()
	r.AdaptorName = m.AdaptorID
	r.AdaptorVersion = m.AdaptorVersion

	groupA := GroupResult{Name: "A: describe + capability shape"}
	groupA.Probes = []ProbeResult{
		runProbe("A_describe_returns_valid_manifest", func(t probeT) { probeOrchestrationDescribeValid(t, impl) }),
		runProbe("A_manifest_round_trips_json", func(t probeT) { probeOrchestrationManifestJSONRoundTrip(t, impl) }),
	}

	groupB := GroupResult{Name: "B: session lifecycle"}
	if fixture.ProgrammaticSessionStarter != nil {
		// Each lifecycle probe needs its own session because most of
		// them mutate it to terminal; we mint a fresh one per probe.
		groupB.Probes = []ProbeResult{
			runSessionProbe("B_list_pending_requests_returns_empty_slice", impl, fixture.ProgrammaticSessionStarter,
				func(t probeT, sessionID string) { probeListPendingRequestsEmpty(t, impl, sessionID) }),
			runSessionProbe("B_end_session_populates_close_reason", impl, fixture.ProgrammaticSessionStarter,
				func(t probeT, sessionID string) { probeEndSessionCloseReason(t, impl, sessionID) }),
			runSessionProbe("B_end_session_idempotent_under_same_nonce", impl, fixture.ProgrammaticSessionStarter,
				func(t probeT, sessionID string) { probeEndSessionIdempotentSameNonce(t, impl, sessionID) }),
			runSessionProbe("B_end_session_terminal_absorbing", impl, fixture.ProgrammaticSessionStarter,
				func(t probeT, sessionID string) { probeEndSessionTerminalAbsorbing(t, impl, sessionID) }),
		}
	} else {
		groupB.Probes = []ProbeResult{{
			Name:     "B_session_lifecycle_group",
			Skipped:  true,
			Messages: []string{"fixture.ProgrammaticSessionStarter not provided"},
		}}
	}

	groupD := GroupResult{Name: "D: subscribe / event delivery"}
	groupD.Probes = []ProbeResult{
		runProbe("D_subscribe_returns_channel", func(t probeT) { probeSubscribeChannel(t, impl) }),
	}

	groupF := GroupResult{Name: "F: error semantics"}
	if fixture.KnownMissingSessionID != "" {
		groupF.Probes = append(groupF.Probes,
			runProbe("F_list_pending_requests_unknown_session_is_tagged", func(t probeT) {
				probeListPendingRequestsUnknownTagged(t, impl, fixture.KnownMissingSessionID)
			}),
		)
	} else {
		groupF.Probes = append(groupF.Probes, ProbeResult{
			Name:     "F_list_pending_requests_unknown_session_is_tagged",
			Skipped:  true,
			Messages: []string{"fixture.KnownMissingSessionID not provided"},
		})
	}

	r.Groups = []GroupResult{groupA, groupB, groupD, groupF}
	r.FinishedAt = time.Now()
	return r
}

// runSessionProbe provisions a fresh session via the starter callback,
// runs fn against it, then records the result. Used by Group B where
// each lifecycle probe needs an independent session.
func runSessionProbe(
	name string,
	impl core.OrchestrationPlaneAdaptor,
	starter func(impl core.OrchestrationPlaneAdaptor) (sessionID string, cleanup func(), err error),
	fn func(t probeT, sessionID string),
) ProbeResult {
	sessionID, cleanup, err := starter(impl)
	if err != nil {
		return ProbeResult{
			Name:     name,
			Passed:   false,
			Messages: []string{"SessionStarter failed: " + err.Error()},
		}
	}
	if cleanup != nil {
		defer cleanup()
	}
	return runProbe(name, func(t probeT) { fn(t, sessionID) })
}
