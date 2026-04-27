package gt

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/MikeBengtson/gemba/core"
)

// workspaceRunner is a scriptable runner that records gt invocations so
// tests can assert idempotency (`rig add` MUST NOT fire when the rig
// already exists) without real side effects.
type workspaceRunner struct {
	t        *testing.T
	rigs     []gtRig
	polecats []gtPolecat
	// addOK: when true, a successful `rig add` pushes the derived row
	// onto rigs so a subsequent acquire resolves it as existing.
	addOK   bool
	addFail error
	stopErr error
	// calls records every (argv) join for assertion.
	calls []string
}

func (w *workspaceRunner) run(_ context.Context, args ...string) ([]byte, error) {
	w.t.Helper()
	w.calls = append(w.calls, strings.Join(args, " "))
	switch {
	case len(args) == 3 && args[0] == "rig" && args[1] == "list" && args[2] == "--json":
		out, err := json.Marshal(w.rigs)
		if err != nil {
			w.t.Fatalf("marshal rigs: %v", err)
		}
		return out, nil
	case len(args) == 4 && args[0] == "polecat" && args[1] == "list":
		out, err := json.Marshal(w.polecats)
		if err != nil {
			w.t.Fatalf("marshal polecats: %v", err)
		}
		return out, nil
	case len(args) >= 2 && args[0] == "rig" && args[1] == "add":
		if w.addFail != nil {
			return nil, w.addFail
		}
		if w.addOK {
			w.rigs = append(w.rigs, gtRig{Name: args[2], Status: "operational"})
		}
		return nil, nil
	case len(args) >= 3 && args[0] == "rig" && args[1] == "stop":
		if w.stopErr != nil {
			return nil, w.stopErr
		}
		return nil, nil
	}
	w.t.Fatalf("workspaceRunner: unexpected gt invocation %q", strings.Join(args, " "))
	return nil, nil
}

func (w *workspaceRunner) countCalls(prefix string) int {
	n := 0
	for _, c := range w.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

func TestAcquireWorkspaceCreatesRig(t *testing.T) {
	wr := &workspaceRunner{t: t, addOK: true}
	o := &OrchestrationPlane{run: wr.run}

	ws, err := o.AcquireWorkspace(context.Background(), core.WorkspaceRequest{
		AssignmentID:      "a-1",
		Repository:        "https://github.com/MikeBengtson/acme.git",
		Branch:            "main",
		RequiredIsolation: core.IsolationCapabilities{FSScoped: true},
	})
	if err != nil {
		t.Fatalf("AcquireWorkspace: %v", err)
	}
	if ws.ID != "acme" {
		t.Errorf("ws.ID=%q, want %q", ws.ID, "acme")
	}
	if ws.Kind != core.WorkspaceWorktree {
		t.Errorf("ws.Kind=%q, want %q", ws.Kind, core.WorkspaceWorktree)
	}
	if !ws.Isolation.FSScoped {
		t.Error("ws.Isolation.FSScoped must be true (DD-5)")
	}
	if ws.Status != core.WorkspaceReady {
		t.Errorf("ws.Status=%q, want %q", ws.Status, core.WorkspaceReady)
	}
	if wr.countCalls("rig add") != 1 {
		t.Errorf("rig add calls=%d, want 1", wr.countCalls("rig add"))
	}
}

func TestAcquireWorkspaceIdempotentReusesRig(t *testing.T) {
	// DoD: "existing rig is reused, not recreated".
	wr := &workspaceRunner{t: t, rigs: []gtRig{{Name: "acme", Status: "operational"}}}
	o := &OrchestrationPlane{run: wr.run}

	ws, err := o.AcquireWorkspace(context.Background(), core.WorkspaceRequest{
		Repository: "git@github.com:MikeBengtson/acme.git",
	})
	if err != nil {
		t.Fatalf("AcquireWorkspace: %v", err)
	}
	if ws.ID != "acme" {
		t.Errorf("ws.ID=%q, want %q", ws.ID, "acme")
	}
	if n := wr.countCalls("rig add"); n != 0 {
		t.Errorf("rig add calls=%d on idempotent acquire, want 0", n)
	}
}

func TestAcquireWorkspaceRejectsStrongerIsolation(t *testing.T) {
	wr := &workspaceRunner{t: t}
	o := &OrchestrationPlane{run: wr.run}

	cases := []core.IsolationCapabilities{
		{FSScoped: true, NetIsolated: true},
		{FSScoped: true, CPULimited: true},
		{FSScoped: true, MemLimited: true},
		{FSScoped: true, SnapshotRestore: true},
	}
	for _, iso := range cases {
		_, err := o.AcquireWorkspace(context.Background(), core.WorkspaceRequest{
			Repository:        "https://github.com/x/y.git",
			RequiredIsolation: iso,
		})
		if err == nil {
			t.Errorf("AcquireWorkspace(%+v): expected error", iso)
			continue
		}
		ae := core.AsAdaptorError(err)
		if ae == nil || ae.Kind != core.KindUnsupported {
			t.Errorf("AcquireWorkspace(%+v): kind=%v, want %q", iso, errKind(ae), core.KindUnsupported)
		}
	}
}

func TestAcquireWorkspaceRejectsUnknownPreferredKind(t *testing.T) {
	wr := &workspaceRunner{t: t}
	o := &OrchestrationPlane{run: wr.run}

	_, err := o.AcquireWorkspace(context.Background(), core.WorkspaceRequest{
		Repository:    "https://github.com/x/y.git",
		PreferredKind: core.WorkspaceContainer,
	})
	if err == nil {
		t.Fatal("expected error for preferred_kind=container")
	}
	ae := core.AsAdaptorError(err)
	if ae == nil || ae.Kind != core.KindUnsupported {
		t.Errorf("kind=%v, want %q", errKind(ae), core.KindUnsupported)
	}
}

func TestAcquireWorkspaceRequiresRepository(t *testing.T) {
	wr := &workspaceRunner{t: t}
	o := &OrchestrationPlane{run: wr.run}

	_, err := o.AcquireWorkspace(context.Background(), core.WorkspaceRequest{})
	if err == nil {
		t.Fatal("expected validation error for empty repository")
	}
	ae := core.AsAdaptorError(err)
	if ae == nil || ae.Kind != core.KindValidation {
		t.Errorf("kind=%v, want %q", errKind(ae), core.KindValidation)
	}
}

func TestReleaseWorkspaceStopsExistingRig(t *testing.T) {
	wr := &workspaceRunner{t: t, rigs: []gtRig{{Name: "acme", Status: "operational"}}}
	o := &OrchestrationPlane{run: wr.run}

	if err := o.ReleaseWorkspace(context.Background(), "acme"); err != nil {
		t.Fatalf("ReleaseWorkspace: %v", err)
	}
	if wr.countCalls("rig stop acme") != 1 {
		t.Errorf("rig stop calls=%d, want 1", wr.countCalls("rig stop acme"))
	}
}

func TestReleaseWorkspaceIdempotentOnUnknown(t *testing.T) {
	// Interface contract: "ReleaseWorkspace releases a workspace; idempotent."
	// We map "unknown id" to "already released" so reapers/retries don't
	// churn — same shape as noop.
	wr := &workspaceRunner{t: t}
	o := &OrchestrationPlane{run: wr.run}

	if err := o.ReleaseWorkspace(context.Background(), "nope"); err != nil {
		t.Errorf("ReleaseWorkspace(unknown) must not error: %v", err)
	}
	if wr.countCalls("rig stop") != 0 {
		t.Errorf("rig stop fired on unknown id: %v", wr.calls)
	}
}

func TestInspectWorkspaceUnknownTagged(t *testing.T) {
	wr := &workspaceRunner{t: t}
	o := &OrchestrationPlane{run: wr.run}

	_, err := o.InspectWorkspace(context.Background(), "nope")
	if err == nil {
		t.Fatal("expected error for unknown workspace")
	}
	ae := core.AsAdaptorError(err)
	if ae == nil || ae.Kind != core.KindSessionNotFound {
		t.Errorf("kind=%v, want %q", errKind(ae), core.KindSessionNotFound)
	}
}

func TestInspectWorkspaceReportsStatus(t *testing.T) {
	wr := &workspaceRunner{t: t, rigs: []gtRig{
		{Name: "live", Status: "operational"},
		{Name: "docked", Status: "docked"},
	}}
	o := &OrchestrationPlane{run: wr.run}

	live, err := o.InspectWorkspace(context.Background(), "live")
	if err != nil {
		t.Fatalf("InspectWorkspace(live): %v", err)
	}
	if live.Status != core.WorkspaceReady {
		t.Errorf("live.Status=%q, want %q", live.Status, core.WorkspaceReady)
	}

	docked, err := o.InspectWorkspace(context.Background(), "docked")
	if err != nil {
		t.Fatalf("InspectWorkspace(docked): %v", err)
	}
	if docked.Status != core.WorkspaceReleased {
		t.Errorf("docked.Status=%q, want %q", docked.Status, core.WorkspaceReleased)
	}
}

func TestDeriveRigName(t *testing.T) {
	cases := map[string]string{
		"https://github.com/MikeBengtson/gemba.git": "gemba",
		"git@github.com:MikeBengtson/gemba.git":     "gemba",
		"https://example.com/foo/bar-baz":           "bar_baz",
		"/local/path/to/ACME_Corp":                  "acme_corp",
		"":                                          "",
		"___":                                       "",
	}
	for in, want := range cases {
		if got := deriveRigName(in); got != want {
			t.Errorf("deriveRigName(%q)=%q, want %q", in, got, want)
		}
	}
}

func TestAcquireWorkspacePropagatesRigAddFailure(t *testing.T) {
	wr := &workspaceRunner{t: t, addFail: errors.New("clone: host unreachable")}
	o := &OrchestrationPlane{run: wr.run}

	_, err := o.AcquireWorkspace(context.Background(), core.WorkspaceRequest{
		Repository: "https://github.com/x/y.git",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	ae := core.AsAdaptorError(err)
	if ae == nil || ae.Kind != core.KindAdaptorDegraded {
		t.Errorf("kind=%v, want %q", errKind(ae), core.KindAdaptorDegraded)
	}
}

func errKind(ae *core.AdaptorError) any {
	if ae == nil {
		return "<nil>"
	}
	return ae.Kind
}
