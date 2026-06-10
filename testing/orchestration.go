package gembatesting

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/GembaCore/gemba-core/core"
)

// OrchestrationFixture carries the optional state some Group B probes
// need. Probes that require a live session skip when SessionStarter is
// nil; the manifest / error-algebra probes still run regardless.
type OrchestrationFixture struct {
	// KnownMissingSessionID is a session id the adaptor is guaranteed
	// NOT to have. The Group F probe calls ListPendingRequests(id) and
	// asserts the error is a tagged KindSessionNotFound AdaptorError.
	// Leave empty to skip.
	KnownMissingSessionID string

	// SessionStarter provisions a live session for the Group B
	// lifecycle probes. It returns the session id and a cleanup
	// callback the harness runs after the lifecycle probes complete.
	// Leave nil to skip the session-dependent probes.
	//
	// Used by the testing.T entry point (RunOrchestrationConformance).
	SessionStarter func(t *testing.T, impl core.OrchestrationPlaneAdaptor) (sessionID string, cleanup func())

	// ProgrammaticSessionStarter is the testing.T-free counterpart of
	// SessionStarter, used by RunOrchestrationProbes (the `gemba
	// adaptor test` CLI). It returns a fresh session id and an optional
	// cleanup callback, or an error if provisioning failed; the error
	// surfaces in the probe result instead of silently skipping.
	//
	// Each Group B probe requests its own session because most of them
	// move the session to terminal state. Adaptor authors implementing
	// both starters typically share a single internal helper that
	// mints an assignment and calls StartSession.
	ProgrammaticSessionStarter func(impl core.OrchestrationPlaneAdaptor) (sessionID string, cleanup func(), err error)

	// AcquireWorkspaceRequest seeds the Group D acquire/release event-
	// emission probe (gm-e3.6.3). Leave nil to skip the workspace half
	// of the event-emission group; adaptors without a workspace
	// allocator (noop) won't run it. The probe pairs the request with
	// a matching ReleaseWorkspace call so the workspace doesn't leak
	// into a follow-up probe.
	AcquireWorkspaceRequest *core.WorkspaceRequest
}

// RunOrchestrationConformance runs the OrchestrationPlane contract
// probes against impl. Subtests are named by conformance group so
// failures surface directly; fixture-independent probes always run.
//
// fixture may be nil; that is equivalent to a zero-value
// OrchestrationFixture.
func RunOrchestrationConformance(t *testing.T, impl core.OrchestrationPlaneAdaptor, fixture *OrchestrationFixture) {
	t.Helper()
	if impl == nil {
		t.Fatal("gembatesting: RunOrchestrationConformance called with nil OrchestrationPlaneAdaptor")
	}
	if fixture == nil {
		fixture = &OrchestrationFixture{}
	}

	t.Run("A_describe_returns_valid_manifest", func(t *testing.T) {
		probeOrchestrationDescribeValid(t, impl)
	})
	t.Run("A_manifest_round_trips_json", func(t *testing.T) {
		probeOrchestrationManifestJSONRoundTrip(t, impl)
	})

	if fixture.KnownMissingSessionID != "" {
		t.Run("F_list_pending_requests_unknown_session_is_tagged", func(t *testing.T) {
			probeListPendingRequestsUnknownTagged(t, impl, fixture.KnownMissingSessionID)
		})
		t.Run("F_peek_session_unknown_session_is_tagged", func(t *testing.T) {
			probePeekSessionUnknownTagged(t, impl, fixture.KnownMissingSessionID)
		})
	}

	if fixture.SessionStarter != nil {
		sessionID, cleanup := fixture.SessionStarter(t, impl)
		if cleanup != nil {
			defer cleanup()
		}
		t.Run("B_list_pending_requests_returns_empty_slice", func(t *testing.T) {
			probeListPendingRequestsEmpty(t, impl, sessionID)
		})
		// gm-e7.3: peek_session SHOULD return a populated SessionPeek
		// for a live session whose adaptor declares any peek mode in
		// its manifest. Runs before the end_session probes since they
		// terminate the session.
		t.Run("B_peek_session_returns_snapshot_for_live_session", func(t *testing.T) {
			probePeekSessionLive(t, impl, sessionID)
		})
		t.Run("B_end_session_populates_close_reason", func(t *testing.T) {
			probeEndSessionCloseReason(t, impl, sessionID)
		})
		t.Run("B_end_session_idempotent_under_same_nonce", func(t *testing.T) {
			probeEndSessionIdempotentSameNonce(t, impl, sessionID)
		})
		t.Run("B_end_session_terminal_absorbing", func(t *testing.T) {
			probeEndSessionTerminalAbsorbing(t, impl, sessionID)
		})
	}

	// gm-e3.6.3 — Group D event-emission probes. Pair the lifecycle
	// fixture's session with a fresh starter call (the lifecycle probes
	// above moved their session to terminal) so PauseSession has
	// something live to act on. Acquire/release runs unconditionally
	// when the workspace fixture is present; the probe gates itself.
	if fixture.SessionStarter != nil {
		sid, cleanup := fixture.SessionStarter(t, impl)
		if cleanup != nil {
			defer cleanup()
		}
		t.Run("D_pause_session_emits_transition", func(t *testing.T) {
			probePauseSessionEmitsTransition(t, impl, sid)
		})
	}
	if fixture.AcquireWorkspaceRequest != nil {
		t.Run("D_acquire_release_workspace_emits_events", func(t *testing.T) {
			probeAcquireReleaseWorkspaceEmits(t, impl, fixture)
		})
	}
}

func probeOrchestrationDescribeValid(t probeT, impl core.OrchestrationPlaneAdaptor) {
	t.Helper()
	m := impl.Describe()

	if m.AdaptorID == "" {
		t.Error("manifest.AdaptorID is required")
	}
	if m.OrchestrationAPIVersion != core.ProtocolVersion {
		t.Errorf("manifest.OrchestrationAPIVersion=%q, want %q",
			m.OrchestrationAPIVersion, core.ProtocolVersion)
	}
	if !m.Transport.Valid() {
		t.Errorf("manifest.Transport=%q is not valid", m.Transport)
	}
	if len(m.WorkspaceKinds) == 0 {
		t.Error("manifest.WorkspaceKinds must declare at least one kind (gm-root DD-5)")
	}
	if len(m.GroupModes) == 0 {
		t.Error("manifest.GroupModes must declare at least one mode (gm-root DD-7)")
	}
	if len(m.CostAxes) == 0 {
		t.Error("manifest.CostAxes must declare at least one axis (gm-root DD-4)")
	}
	if len(m.EscalationKinds) == 0 {
		t.Error("manifest.EscalationKinds must not be empty (gm-root DD-6)")
	}
	if len(m.PeekModes) == 0 {
		t.Error("manifest.PeekModes must not be empty")
	}
}

func probeOrchestrationManifestJSONRoundTrip(t probeT, impl core.OrchestrationPlaneAdaptor) {
	t.Helper()
	m := impl.Describe()
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	var round core.OrchestrationCapabilityManifest
	if err := json.Unmarshal(data, &round); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	if !reflect.DeepEqual(m, round) {
		t.Errorf("manifest JSON round-trip drift:\n  orig:  %+v\n  round: %+v", m, round)
	}
}

func probeListPendingRequestsEmpty(t probeT, impl core.OrchestrationPlaneAdaptor, sessionID string) {
	t.Helper()
	reqs, err := impl.ListPendingRequests(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("ListPendingRequests(%q): %v", sessionID, err)
	}
	if reqs == nil {
		t.Error("ListPendingRequests returned nil; contract says return empty slice (not nil) for a session with nothing pending")
	}
	if len(reqs) != 0 {
		t.Errorf("ListPendingRequests returned %d entries for a freshly-started session; want 0", len(reqs))
	}
}

// probePeekSessionLive asserts the adaptor's PeekSession contract for
// a session it knows about: returns a populated SessionPeek with the
// matching SessionID and a non-zero CapturedAt. Adaptors are free to
// leave Transcript / Screenshot / Structured empty if their declared
// peek_modes don't apply, but the envelope itself MUST be present so
// callers (the SPA's session drawer) can render even a "no transcript
// available" placeholder reliably. gm-e7.3.
func probePeekSessionLive(t probeT, impl core.OrchestrationPlaneAdaptor, sessionID string) {
	t.Helper()
	peek, err := impl.PeekSession(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("PeekSession(%q): %v", sessionID, err)
	}
	if peek.SessionID != sessionID {
		t.Errorf("PeekSession.SessionID: want %q got %q", sessionID, peek.SessionID)
	}
	if peek.CapturedAt.IsZero() {
		t.Error("PeekSession.CapturedAt is zero; adaptor MUST populate the wall-clock the snapshot was taken")
	}
}

// probePeekSessionUnknownTagged asserts that a peek against an id the
// adaptor doesn't know about surfaces a typed KindSessionNotFound,
// matching the rest of the session-lookup methods. gm-e7.3.
func probePeekSessionUnknownTagged(t probeT, impl core.OrchestrationPlaneAdaptor, missing string) {
	t.Helper()
	_, err := impl.PeekSession(context.Background(), missing)
	if err == nil {
		t.Fatalf("PeekSession(%q) returned nil error for unknown session", missing)
	}
	if assertErr := core.AssertAdaptorError(err); assertErr != nil {
		t.Errorf("PeekSession(%q): %v", missing, assertErr)
		return
	}
	ae := core.AsAdaptorError(err)
	// KindUnsupported is acceptable for adaptors whose backend
	// genuinely can't peek. Otherwise the contract is SessionNotFound.
	if ae.Kind != core.KindSessionNotFound && ae.Kind != core.KindUnsupported {
		t.Errorf("PeekSession(%q): kind=%q, want %q (or %q for adaptors that can't peek at all)",
			missing, ae.Kind, core.KindSessionNotFound, core.KindUnsupported)
	}
}

func probeListPendingRequestsUnknownTagged(t probeT, impl core.OrchestrationPlaneAdaptor, missing string) {
	t.Helper()
	_, err := impl.ListPendingRequests(context.Background(), missing)
	if err == nil {
		t.Fatalf("ListPendingRequests(%q) returned nil error for unknown session; fixture.KnownMissingSessionID must name an id the adaptor does not have",
			missing)
	}
	if assertErr := core.AssertAdaptorError(err); assertErr != nil {
		t.Errorf("ListPendingRequests(%q): %v", missing, assertErr)
		return
	}
	ae := core.AsAdaptorError(err)
	if ae.Kind != core.KindSessionNotFound {
		t.Errorf("ListPendingRequests(%q): kind=%q, want %q",
			missing, ae.Kind, core.KindSessionNotFound)
	}
}

func probeEndSessionCloseReason(t probeT, impl core.OrchestrationPlaneAdaptor, sessionID string) {
	t.Helper()
	nonce := core.ConfirmNonce("conformance-close-reason")
	s, err := impl.EndSession(context.Background(), sessionID, core.SessionEndCompleted, nonce)
	if err != nil {
		t.Fatalf("EndSession: %v", err)
	}
	switch s.Status {
	case core.SessionCompleted, core.SessionFailed:
	default:
		t.Errorf("EndSession: status=%q, want terminal (completed|failed)", s.Status)
	}
	if s.CloseReason == nil {
		t.Fatal("EndSession: Session.CloseReason is nil; adaptor MUST populate a typed SessionCloseReason on terminal close (t3code audit)")
	}
	if !s.CloseReason.Valid() {
		t.Errorf("EndSession: CloseReason=%q is not one of the canonical values", *s.CloseReason)
	}
}

func probeEndSessionIdempotentSameNonce(t probeT, impl core.OrchestrationPlaneAdaptor, sessionID string) {
	t.Helper()
	ctx := context.Background()
	nonce := core.ConfirmNonce("conformance-idempotent-same-nonce")
	first, err := impl.EndSession(ctx, sessionID, core.SessionEndCompleted, nonce)
	if err != nil {
		t.Fatalf("EndSession (1): %v", err)
	}
	second, err := impl.EndSession(ctx, sessionID, core.SessionEndCompleted, nonce)
	if err != nil {
		t.Fatalf("EndSession (2): same nonce replay MUST NOT error: %v", err)
	}
	if first.Status != second.Status {
		t.Errorf("EndSession same-nonce replay returned different status: first=%q second=%q",
			first.Status, second.Status)
	}
}

func probeEndSessionTerminalAbsorbing(t probeT, impl core.OrchestrationPlaneAdaptor, sessionID string) {
	t.Helper()
	ctx := context.Background()
	if _, err := impl.EndSession(ctx, sessionID, core.SessionEndCompleted, core.ConfirmNonce("conformance-absorb-first")); err != nil {
		t.Fatalf("EndSession (first close): %v", err)
	}
	// Fresh nonce on an already-terminal session MUST be a no-op: same
	// Session returned, no error, no second event. Protects defensive
	// callers (reaper, stop-stale, explicit stop) from racing.
	s, err := impl.EndSession(ctx, sessionID, core.SessionEndCompleted, core.ConfirmNonce("conformance-absorb-second"))
	if err != nil {
		t.Fatalf("EndSession on terminal session (fresh nonce) MUST NOT error: %v", err)
	}
	switch s.Status {
	case core.SessionCompleted, core.SessionFailed:
	default:
		t.Errorf("EndSession on terminal: status=%q, want terminal", s.Status)
	}
}
