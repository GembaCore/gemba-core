package gt

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/GembaCore/gemba-core/core"
)

// fakeGT routes gt invocations against a per-test response map. It
// captures every call so tests can assert on the exact CLI form
// (subcommand, flags, and positional args).
type fakeGT struct {
	t          *testing.T
	calls      [][]string
	response   map[string]fakeResponse
	defaultErr error
}

type fakeResponse struct {
	out []byte
	err error
}

func (f *fakeGT) run(_ context.Context, args ...string) ([]byte, error) {
	f.t.Helper()
	f.calls = append(f.calls, append([]string(nil), args...))
	key := strings.Join(args, " ")
	if r, ok := f.response[key]; ok {
		return r.out, r.err
	}
	// Prefix-match — many gt commands have variable trailing args
	// (timestamps, reasons) we don't want to pin in the fixture.
	for k, r := range f.response {
		if strings.HasPrefix(key, k+" ") || key == k {
			return r.out, r.err
		}
	}
	if f.defaultErr != nil {
		return nil, f.defaultErr
	}
	return nil, errors.New("fakeGT: no canned response for " + key)
}

func newPlaneWith(t *testing.T, responses map[string]fakeResponse) (*OrchestrationPlane, *fakeGT) {
	t.Helper()
	f := &fakeGT{t: t, response: responses}
	return &OrchestrationPlane{run: f.run}, f
}

const sampleConvoyJSON = `[
	{
		"id": "hq-cv1",
		"title": "Work: gt-abc",
		"status": "open",
		"rig": "gemba",
		"issues": ["gt-abc"],
		"polecats": ["topaz"],
		"workers": [
			{"rig": "gemba", "polecat": "topaz", "bead": "gt-abc", "status": "working"}
		],
		"created_at": "2026-04-29T10:00:00Z"
	}
]`

const samplePolecatsForSessionsJSON = `[
	{"rig": "gemba", "name": "topaz", "state": "working", "issue": "gt-abc", "session_running": true},
	{"rig": "gemba", "name": "ruby", "state": "idle", "session_running": true},
	{"rig": "gemba", "name": "obsidian", "state": "stopped", "session_running": false}
]`

const sampleRigsJSON = `[
	{"name": "gemba", "beads_prefix": "gt", "status": "operational", "witness": "running", "refinery": "running", "polecats": 1, "crew": 1}
]`

func TestStartSessionShellsOutAndReturnsInitializing(t *testing.T) {
	plane, fake := newPlaneWith(t, map[string]fakeResponse{
		"rig list --json":                            {out: []byte(sampleRigsJSON)},
		"convoy list --json":                         {out: []byte(sampleConvoyJSON)},
		"sling gt-abc gemba --agent claude --create": {out: []byte("ok")},
	})

	prompt := core.SessionPrompt{
		Extension: map[string]any{
			"gemba:bead_id":    "gt-abc",
			"gemba:agent_type": "claude",
			"gemba:nonce":      "n-1",
		},
	}
	sess, err := plane.StartSession(context.Background(), "gt-abc", prompt)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if sess.Status != core.SessionInitializing {
		t.Errorf("Status=%q, want %q", sess.Status, core.SessionInitializing)
	}
	if sess.AssignmentID != "gt-abc" {
		t.Errorf("AssignmentID=%q, want gt-abc", sess.AssignmentID)
	}
	if got := sess.ProviderMetadata["adaptor"]; got != "gastown" {
		t.Errorf("ProviderMetadata[adaptor]=%v, want gastown", got)
	}
	// Verify the sling shell-out happened with expected positional args.
	found := false
	for _, c := range fake.calls {
		if len(c) >= 2 && c[0] == "sling" && c[1] == "gt-abc" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected `gt sling gt-abc ...` invocation; got calls=%v", fake.calls)
	}
}

func TestStartSessionRejectsMissingAgentType(t *testing.T) {
	plane, _ := newPlaneWith(t, nil)
	_, err := plane.StartSession(context.Background(), "gt-abc", core.SessionPrompt{
		Extension: map[string]any{"gemba:bead_id": "gt-abc"},
	})
	ae := core.AsAdaptorError(err)
	if ae == nil || ae.Kind != core.KindValidation {
		t.Fatalf("expected validation error, got %v (%T)", err, err)
	}
}

func TestStartSessionShellFailureSurfacesAdaptorDegraded(t *testing.T) {
	plane, _ := newPlaneWith(t, map[string]fakeResponse{
		"rig list --json": {out: []byte(sampleRigsJSON)},
		"sling gt-abc gemba --agent claude --create": {err: errors.New("boom")},
	})
	_, err := plane.StartSession(context.Background(), "gt-abc", core.SessionPrompt{
		Extension: map[string]any{
			"gemba:bead_id":    "gt-abc",
			"gemba:agent_type": "claude",
		},
	})
	if err == nil {
		t.Fatal("expected error from gt sling failure")
	}
	ae := core.AsAdaptorError(err)
	if ae == nil {
		t.Fatalf("expected *core.AdaptorError, got %T", err)
	}
	if ae.Kind != core.KindAdaptorDegraded {
		t.Errorf("Kind=%q, want adaptor_degraded", ae.Kind)
	}
}

// TestStartSessionAlreadyHookedSurfacesSentinel pins the gm-e3.8
// inline-claim contract: when `gt sling` rejects with
// "bead already hooked", the adaptor wraps the failure into the
// core.ErrBeadAlreadyClaimed sentinel so the auto-dispatch daemon's
// soft-skip path can pick the next candidate.
func TestStartSessionAlreadyHookedSurfacesSentinel(t *testing.T) {
	plane, _ := newPlaneWith(t, map[string]fakeResponse{
		"rig list --json": {out: []byte(sampleRigsJSON)},
		"sling gt-abc gemba --agent claude --create": {
			err: errors.New("error: bead already hooked to gemba/topaz"),
		},
	})
	_, err := plane.StartSession(context.Background(), "gt-abc", core.SessionPrompt{
		Extension: map[string]any{
			"gemba:bead_id":    "gt-abc",
			"gemba:agent_type": "claude",
		},
	})
	if err == nil {
		t.Fatal("expected error from gt sling collision")
	}
	if !core.IsAlreadyClaimedError(err) {
		t.Errorf("expected ErrBeadAlreadyClaimed sentinel, got %T (%v)", err, err)
	}
	// The error must still be a tagged AdaptorError (Conformance Group F).
	if core.AsAdaptorError(err) == nil {
		t.Errorf("expected wrapped *AdaptorError, got %T", err)
	}
}

// TestStartSessionGenericShellErrorIsNotAlreadyClaimed verifies the
// negative case: a non-collision shell failure (e.g. transport blip)
// surfaces as KindAdaptorDegraded, NOT as the already-claimed
// sentinel — the soft-skip path must NOT swallow real adaptor errors.
func TestStartSessionGenericShellErrorIsNotAlreadyClaimed(t *testing.T) {
	plane, _ := newPlaneWith(t, map[string]fakeResponse{
		"rig list --json": {out: []byte(sampleRigsJSON)},
		"sling gt-abc gemba --agent claude --create": {
			err: errors.New("error: gt server unreachable"),
		},
	})
	_, err := plane.StartSession(context.Background(), "gt-abc", core.SessionPrompt{
		Extension: map[string]any{
			"gemba:bead_id":    "gt-abc",
			"gemba:agent_type": "claude",
		},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if core.IsAlreadyClaimedError(err) {
		t.Errorf("generic shell failure must NOT match already-claimed sentinel; got %v", err)
	}
}

func TestStartSessionWithExplicitTargetRig(t *testing.T) {
	plane, fake := newPlaneWith(t, map[string]fakeResponse{
		"convoy list --json":                         {out: []byte("[]")},
		"sling gt-abc beads --agent claude --create": {out: []byte("ok")},
	})
	prompt := core.SessionPrompt{
		Extension: map[string]any{
			"gemba:bead_id":    "gt-abc",
			"gemba:agent_type": "claude",
			"gemba:target_rig": "beads",
		},
	}
	if _, err := plane.StartSession(context.Background(), "gt-abc", prompt); err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	// `gt rig list --json` must NOT be invoked when target is explicit.
	for _, c := range fake.calls {
		if len(c) >= 2 && c[0] == "rig" && c[1] == "list" {
			t.Errorf("rig list called even though target_rig was explicit: %v", c)
		}
	}
}

func TestEndSessionUnslingsForCompleted(t *testing.T) {
	plane, fake := newPlaneWith(t, map[string]fakeResponse{
		"convoy list --json": {out: []byte(sampleConvoyJSON)},
		"unsling gt-abc":     {out: []byte("ok")},
	})
	sess, err := plane.EndSession(context.Background(), "gastown/gemba/polecats/topaz", core.SessionEndCompleted, "")
	if err != nil {
		t.Fatalf("EndSession: %v", err)
	}
	if sess.Status != core.SessionCompleted {
		t.Errorf("Status=%q, want completed", sess.Status)
	}
	if sess.CloseReason == nil || *sess.CloseReason != core.CloseUserStop {
		t.Errorf("CloseReason=%v, want user_stop", sess.CloseReason)
	}
	// Verify unsling, not release.
	saw := false
	for _, c := range fake.calls {
		if len(c) >= 2 && c[0] == "unsling" && c[1] == "gt-abc" {
			saw = true
		}
	}
	if !saw {
		t.Errorf("expected `gt unsling gt-abc`; got %v", fake.calls)
	}
}

func TestEndSessionReleasesForFailed(t *testing.T) {
	plane, fake := newPlaneWith(t, map[string]fakeResponse{
		"convoy list --json": {out: []byte(sampleConvoyJSON)},
		"release gt-abc":     {out: []byte("ok")},
	})
	sess, err := plane.EndSession(context.Background(), "gastown/gemba/polecats/topaz", core.SessionEndFailed, "")
	if err != nil {
		t.Fatalf("EndSession: %v", err)
	}
	if sess.Status != core.SessionFailed {
		t.Errorf("Status=%q, want failed", sess.Status)
	}
	saw := false
	for _, c := range fake.calls {
		if len(c) >= 2 && c[0] == "release" && c[1] == "gt-abc" {
			saw = true
		}
	}
	if !saw {
		t.Errorf("expected `gt release gt-abc`; got %v", fake.calls)
	}
}

func TestEndSessionUnknownSessionTagged(t *testing.T) {
	plane, _ := newPlaneWith(t, map[string]fakeResponse{
		"convoy list --json": {out: []byte("[]")},
	})
	_, err := plane.EndSession(context.Background(), "gastown/gemba/polecats/ghost", core.SessionEndCompleted, "")
	ae := core.AsAdaptorError(err)
	if ae == nil {
		t.Fatalf("expected *core.AdaptorError, got %T", err)
	}
	if ae.Kind != core.KindSessionNotFound {
		t.Errorf("Kind=%q, want session_not_found", ae.Kind)
	}
}

func TestListSessionsMapsPolecatsToSessions(t *testing.T) {
	plane, _ := newPlaneWith(t, map[string]fakeResponse{
		"convoy list --json":        {out: []byte(sampleConvoyJSON)},
		"polecat list --all --json": {out: []byte(samplePolecatsForSessionsJSON)},
	})
	got, err := plane.ListSessions(context.Background(), core.SessionFilter{})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	// 2 active polecats; obsidian is session_running=false → excluded
	// when IncludeTerminal is false (default).
	if len(got) != 2 {
		t.Fatalf("expected 2 sessions, got %d (%+v)", len(got), got)
	}
	idx := map[string]core.Session{}
	for _, s := range got {
		idx[s.ID] = s
	}
	if s, ok := idx["gastown/gemba/polecats/topaz"]; !ok || s.Status != core.SessionWorking {
		t.Errorf("topaz status=%q, want working", s.Status)
	}
	if s, ok := idx["gastown/gemba/polecats/ruby"]; !ok || s.Status != core.SessionReady {
		t.Errorf("ruby status=%q, want ready", s.Status)
	}
	// AssignmentID should be picked up from the convoy/issue map.
	if idx["gastown/gemba/polecats/topaz"].AssignmentID != "gt-abc" {
		t.Errorf("topaz assignment=%q, want gt-abc",
			idx["gastown/gemba/polecats/topaz"].AssignmentID)
	}
}

func TestListSessionsFilterByStatus(t *testing.T) {
	plane, _ := newPlaneWith(t, map[string]fakeResponse{
		"convoy list --json":        {out: []byte(sampleConvoyJSON)},
		"polecat list --all --json": {out: []byte(samplePolecatsForSessionsJSON)},
	})
	got, err := plane.ListSessions(context.Background(), core.SessionFilter{
		Status: []core.SessionStatus{core.SessionReady},
	})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(got) != 1 || got[0].Status != core.SessionReady {
		t.Errorf("expected single ready session, got %+v", got)
	}
}

func TestListSessionsBadJSONErrors(t *testing.T) {
	plane, _ := newPlaneWith(t, map[string]fakeResponse{
		"convoy list --json":        {out: []byte("[]")},
		"polecat list --all --json": {out: []byte("not json")},
	})
	_, err := plane.ListSessions(context.Background(), core.SessionFilter{})
	if err == nil {
		t.Fatal("expected JSON decode error")
	}
}

func TestPeekSessionShellsToGtPeek(t *testing.T) {
	plane, fake := newPlaneWith(t, map[string]fakeResponse{
		"peek gemba/topaz -n 200": {out: []byte("transcript-bytes")},
	})
	peek, err := plane.PeekSession(context.Background(), "gastown/gemba/polecats/topaz")
	if err != nil {
		t.Fatalf("PeekSession: %v", err)
	}
	if peek.Transcript != "transcript-bytes" {
		t.Errorf("Transcript=%q, want transcript-bytes", peek.Transcript)
	}
	saw := false
	for _, c := range fake.calls {
		if len(c) >= 2 && c[0] == "peek" && c[1] == "gemba/topaz" {
			saw = true
		}
	}
	if !saw {
		t.Errorf("expected `gt peek gemba/topaz -n 200`, got %v", fake.calls)
	}
}

func TestPeekSessionRequiresID(t *testing.T) {
	plane, _ := newPlaneWith(t, nil)
	_, err := plane.PeekSession(context.Background(), "")
	ae := core.AsAdaptorError(err)
	if ae == nil || ae.Kind != core.KindValidation {
		t.Errorf("expected validation error, got %v", err)
	}
}

const sampleMailInboxJSON = `[
	{"id": "hq-mail-1", "from": "gemba/crew/mike", "to": "gemba/topaz", "subject": "review please", "body": "details", "created_at": "2026-04-29T10:00:00Z", "read": false}
]`

func TestListPendingRequestsMapsMail(t *testing.T) {
	plane, _ := newPlaneWith(t, map[string]fakeResponse{
		"mail inbox --identity gemba/topaz --unread --json": {out: []byte(sampleMailInboxJSON)},
	})
	got, err := plane.ListPendingRequests(context.Background(), "gastown/gemba/polecats/topaz")
	if err != nil {
		t.Fatalf("ListPendingRequests: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 pending, got %d", len(got))
	}
	if got[0].Source != core.EscalationOrchestratorPause {
		t.Errorf("Source=%q, want orchestrator_pause", got[0].Source)
	}
	if got[0].Title != "review please" {
		t.Errorf("Title=%q", got[0].Title)
	}
}

func TestListPendingRequestsEmptyOK(t *testing.T) {
	plane, _ := newPlaneWith(t, map[string]fakeResponse{
		"mail inbox --identity gemba/topaz --unread --json": {out: []byte("[]")},
	})
	got, err := plane.ListPendingRequests(context.Background(), "gastown/gemba/polecats/topaz")
	if err != nil {
		t.Fatalf("ListPendingRequests: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty, got %d", len(got))
	}
}

func TestResolveEscalationShellsToGtClose(t *testing.T) {
	plane, fake := newPlaneWith(t, map[string]fakeResponse{
		"escalate close hq-esc-1 --reason approve by operator": {out: []byte("ok")},
	})
	got, err := plane.ResolveEscalation(context.Background(), "hq-esc-1", core.EscalationResolution{
		Kind:       core.ResolutionApprove,
		ResolvedBy: "operator",
	}, "")
	if err != nil {
		t.Fatalf("ResolveEscalation: %v", err)
	}
	if got.State != core.EscalationResolved {
		t.Errorf("State=%q, want resolved", got.State)
	}
	saw := false
	for _, c := range fake.calls {
		if len(c) >= 3 && c[0] == "escalate" && c[1] == "close" && c[2] == "hq-esc-1" {
			saw = true
		}
	}
	if !saw {
		t.Errorf("expected gt escalate close, got %v", fake.calls)
	}
}

func TestResolveEscalationRequiresID(t *testing.T) {
	plane, _ := newPlaneWith(t, nil)
	_, err := plane.ResolveEscalation(context.Background(), "", core.EscalationResolution{}, "")
	ae := core.AsAdaptorError(err)
	if ae == nil || ae.Kind != core.KindValidation {
		t.Errorf("expected validation error, got %v", err)
	}
}

// TestSubscribeEmitsSessionTransitionsOnPoll verifies the poll-based
// emitter publishes a session_transition event on the first tick when
// it discovers an active polecat. Uses a tightened poll period so the
// test completes in a few hundred ms.
func TestSubscribeEmitsSessionTransitionsOnPoll(t *testing.T) {
	prev := subscribePollPeriod
	subscribePollPeriod = 25 * time.Millisecond
	defer func() { subscribePollPeriod = prev }()

	plane, _ := newPlaneWith(t, map[string]fakeResponse{
		"convoy list --json":        {out: []byte(sampleConvoyJSON)},
		"polecat list --all --json": {out: []byte(samplePolecatsForSessionsJSON)},
		"escalate list --json":      {out: []byte("[]")},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	ch, err := plane.Subscribe(ctx, core.SubscribeFilter{Kinds: []string{"session_transition"}})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	got := 0
	for evt := range ch {
		if evt.Kind != "session_transition" {
			t.Errorf("filter leak: kind=%q", evt.Kind)
		}
		got++
		if got >= 2 {
			cancel()
		}
	}
	if got == 0 {
		t.Fatal("expected at least one session_transition event")
	}
}

// TestSubscribeRespectsContextCancellation ensures the channel is
// closed when ctx is canceled and no more events leak.
func TestSubscribeRespectsContextCancellation(t *testing.T) {
	prev := subscribePollPeriod
	subscribePollPeriod = 10 * time.Millisecond
	defer func() { subscribePollPeriod = prev }()

	plane, _ := newPlaneWith(t, map[string]fakeResponse{
		"convoy list --json":        {out: []byte("[]")},
		"polecat list --all --json": {out: []byte("[]")},
		"escalate list --json":      {out: []byte("[]")},
	})
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := plane.Subscribe(ctx, core.SubscribeFilter{})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	cancel()
	deadline := time.After(500 * time.Millisecond)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return // channel closed cleanly
			}
		case <-deadline:
			t.Fatal("Subscribe channel did not close after ctx cancel")
		}
	}
}

func TestPolecatStateMappings(t *testing.T) {
	cases := []struct {
		state   string
		running bool
		want    core.SessionStatus
	}{
		{"idle", true, core.SessionReady},
		{"working", true, core.SessionWorking},
		{"claimed", true, core.SessionWorking},
		{"stalled", true, core.SessionStalled},
		{"prompting", true, core.SessionPrompting},
		{"suspended", true, core.SessionSuspended},
		{"unknown", true, core.SessionWorking},
		{"working", false, core.SessionCompleted},
	}
	for _, c := range cases {
		got := polecatStateToSessionStatus(c.state, c.running)
		if got != c.want {
			t.Errorf("state=%q running=%v: got %q want %q",
				c.state, c.running, got, c.want)
		}
	}
}

func TestPeekTargetFromSessionID(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"gastown/gemba/polecats/topaz", "gemba/topaz"},
		{"gastown/gemba/crew/mike", "gemba/crew/mike"},
		{"gastown/mayor", "mayor"},
	}
	for _, c := range cases {
		got := peekTargetFromSessionID(c.in)
		if got != c.want {
			t.Errorf("in=%q: got %q want %q", c.in, got, c.want)
		}
	}
}
