package gt

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/GembaCore/gemba-core/core"
)

// newRecyclePlane wires a plane with caps populated (HasHandoff=true)
// plus the recycledEvents side-channel so Subscribe can drain emitted
// events. Tests pass canned shell responses and inspect the emitted
// session.recycled event off the channel.
func newRecyclePlane(t *testing.T, responses map[string]fakeResponse) (*OrchestrationPlane, *fakeGT) {
	t.Helper()
	plane, fake := newPlaneWith(t, responses)
	plane.caps = gtCapabilities{
		Version:    "1.0.0",
		VersionRaw: "gt version 1.0.0",
		HasHandoff: true,
	}
	plane.recycledEvents = make(chan core.OrchestrationEvent, 4)
	return plane, fake
}

// idlePolecatJSON is a polecat whose status maps to SessionReady (idle
// + session_running=true) — the only state recycle accepts.
const idlePolecatJSON = `[
	{"rig": "gemba", "name": "topaz", "state": "idle", "session_running": true}
]`

const workingPolecatJSON = `[
	{"rig": "gemba", "name": "topaz", "state": "working", "issue": "gt-abc", "session_running": true}
]`

const cleanGitStateJSON = `{"clean": true, "uncommitted_files": [], "unpushed_commits": 0, "stash_count": 0}`
const dirtyGitStateJSON = `{"clean": false, "uncommitted_files": ["foo.txt"], "unpushed_commits": 0, "stash_count": 0}`

func TestRecycleSessionCleanWorktreePasses(t *testing.T) {
	plane, fake := newRecyclePlane(t, map[string]fakeResponse{
		"convoy list --json":                   {out: []byte(sampleConvoyJSON)},
		"polecat list --all --json":            {out: []byte(idlePolecatJSON)},
		"polecat git-state gemba/topaz --json": {out: []byte(cleanGitStateJSON)},
		"handoff gemba/topaz":                  {out: []byte("ok")},
	})

	sess, err := plane.RecycleSession(context.Background(), "gastown/gemba/polecats/topaz")
	if err != nil {
		t.Fatalf("RecycleSession: %v", err)
	}
	if sess.Status != core.SessionInitializing {
		t.Errorf("Status=%q, want Initializing", sess.Status)
	}
	if got := sess.ProviderMetadata["prior_session_id"]; got != "gastown/gemba/polecats/topaz" {
		t.Errorf("prior_session_id=%v", got)
	}
	if got := sess.ProviderMetadata["gt_handoff"]; got != true {
		t.Errorf("gt_handoff=%v, want true", got)
	}

	// Verify `gt handoff` was actually shelled.
	saw := false
	for _, c := range fake.calls {
		if len(c) >= 2 && c[0] == "handoff" && c[1] == "gemba/topaz" {
			saw = true
		}
	}
	if !saw {
		t.Errorf("expected `gt handoff gemba/topaz`; got %v", fake.calls)
	}

	// Drain session.recycled event.
	select {
	case evt := <-plane.recycledEvents:
		if evt.Kind != "session.recycled" {
			t.Errorf("evt.Kind=%q, want session.recycled", evt.Kind)
		}
		if got := evt.Payload["prior_session_id"]; got != "gastown/gemba/polecats/topaz" {
			t.Errorf("payload prior_session_id=%v", got)
		}
		if got := evt.Payload["new_session_id"]; got != sess.ID {
			t.Errorf("payload new_session_id=%v, want %q", got, sess.ID)
		}
		if got := evt.Payload["reason"]; got != "explicit" {
			t.Errorf("payload reason=%v, want explicit", got)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected session.recycled event on side-channel; none received")
	}
}

func TestRecycleSessionDirtyWorktreeRefuses(t *testing.T) {
	plane, fake := newRecyclePlane(t, map[string]fakeResponse{
		"convoy list --json":                   {out: []byte(sampleConvoyJSON)},
		"polecat list --all --json":            {out: []byte(idlePolecatJSON)},
		"polecat git-state gemba/topaz --json": {out: []byte(dirtyGitStateJSON)},
		// EndSession fallback path: convoy list (already covered) +
		// unsling for the bead. The bead is recovered from the convoy
		// listing (sampleConvoyJSON has issue gt-abc).
		"unsling gt-abc": {out: []byte("ok")},
	})

	_, err := plane.RecycleSession(context.Background(), "gastown/gemba/polecats/topaz")
	ae := core.AsAdaptorError(err)
	if ae == nil {
		t.Fatalf("expected *core.AdaptorError, got %T", err)
	}
	if ae.Kind != core.KindValidation {
		t.Errorf("Kind=%q, want validation (refuse-on-dirty)", ae.Kind)
	}

	// Must NOT have shelled handoff.
	for _, c := range fake.calls {
		if len(c) >= 1 && c[0] == "handoff" {
			t.Errorf("handoff shelled despite dirty worktree: %v", c)
		}
	}
	// Must HAVE shelled unsling (end fallback).
	saw := false
	for _, c := range fake.calls {
		if len(c) >= 2 && c[0] == "unsling" && c[1] == "gt-abc" {
			saw = true
		}
	}
	if !saw {
		t.Errorf("expected `gt unsling` end-fallback; got %v", fake.calls)
	}

	// Must NOT have emitted session.recycled.
	select {
	case evt := <-plane.recycledEvents:
		t.Errorf("unexpected recycle event on dirty refuse: %+v", evt)
	default:
	}
}

func TestRecycleSessionHandoffFailureSurfacesError(t *testing.T) {
	plane, _ := newRecyclePlane(t, map[string]fakeResponse{
		"convoy list --json":                   {out: []byte(sampleConvoyJSON)},
		"polecat list --all --json":            {out: []byte(idlePolecatJSON)},
		"polecat git-state gemba/topaz --json": {out: []byte(cleanGitStateJSON)},
		"handoff gemba/topaz":                  {err: errors.New("boom")},
	})

	_, err := plane.RecycleSession(context.Background(), "gastown/gemba/polecats/topaz")
	if err == nil {
		t.Fatal("expected error when handoff fails")
	}
	ae := core.AsAdaptorError(err)
	if ae == nil {
		t.Fatalf("expected *core.AdaptorError, got %T", err)
	}
	if ae.Kind != core.KindAdaptorDegraded {
		t.Errorf("Kind=%q, want adaptor_degraded", ae.Kind)
	}

	// No event emitted on failure.
	select {
	case evt := <-plane.recycledEvents:
		t.Errorf("unexpected recycle event on failure: %+v", evt)
	default:
	}
}

func TestRecycleSessionRejectsNonReadyStatus(t *testing.T) {
	plane, fake := newRecyclePlane(t, map[string]fakeResponse{
		"convoy list --json":        {out: []byte(sampleConvoyJSON)},
		"polecat list --all --json": {out: []byte(workingPolecatJSON)},
	})

	_, err := plane.RecycleSession(context.Background(), "gastown/gemba/polecats/topaz")
	ae := core.AsAdaptorError(err)
	if ae == nil {
		t.Fatalf("expected *core.AdaptorError, got %T", err)
	}
	if ae.Kind != core.KindValidation {
		t.Errorf("Kind=%q, want validation (mid-bead recycle)", ae.Kind)
	}
	// Must NOT have shelled git-state or handoff for a non-Ready session.
	for _, c := range fake.calls {
		if len(c) >= 1 && (c[0] == "handoff" || (len(c) >= 2 && c[0] == "polecat" && c[1] == "git-state")) {
			t.Errorf("unexpected shell for non-Ready session: %v", c)
		}
	}
}

func TestRecycleSessionCapabilityGate(t *testing.T) {
	// Probe ran but reported HasHandoff=false → KindUnsupported with a
	// pointed diagnostic. We mark VersionRaw to indicate the probe
	// actually ran (zero-valued caps from tests bypass the gate).
	plane, _ := newPlaneWith(t, nil)
	plane.caps = gtCapabilities{
		Version:    "1.0.0",
		VersionRaw: "gt version 1.0.0",
		HasHandoff: false,
	}
	_, err := plane.RecycleSession(context.Background(), "gastown/gemba/polecats/topaz")
	ae := core.AsAdaptorError(err)
	if ae == nil {
		t.Fatalf("expected *core.AdaptorError, got %T", err)
	}
	if ae.Kind != core.KindUnsupported {
		t.Errorf("Kind=%q, want unsupported (handoff missing)", ae.Kind)
	}
}

func TestRecycleSessionRequiresPolecatShape(t *testing.T) {
	plane, _ := newRecyclePlane(t, map[string]fakeResponse{
		"convoy list --json": {out: []byte(sampleConvoyJSON)},
	})
	// Synthetic "gastown/<target>/<bead>/<ts>" shape — not a polecat.
	_, err := plane.RecycleSession(context.Background(), "gastown/gemba/gt-abc/20260429T120000")
	ae := core.AsAdaptorError(err)
	if ae == nil {
		t.Fatalf("expected *core.AdaptorError, got %T", err)
	}
	if ae.Kind != core.KindValidation {
		t.Errorf("Kind=%q, want validation (non-polecat session)", ae.Kind)
	}
}
