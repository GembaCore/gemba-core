package gt

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/MikeBengtson/gemba/internal/core"
)

// stubRunner returns canned bytes for each (subcommand) key. Unknown
// invocations fail the test so every gt call the adapter makes is
// accounted for in the fixture.
type stubRunner struct {
	t        *testing.T
	response map[string][]byte
}

func (s *stubRunner) run(_ context.Context, args ...string) ([]byte, error) {
	s.t.Helper()
	key := strings.Join(args, " ")
	out, ok := s.response[key]
	if !ok {
		s.t.Fatalf("stubRunner: unexpected gt invocation %q (known: %v)", key, keysOf(s.response))
	}
	return out, nil
}

func keysOf(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func newAdaptor(t *testing.T, rigs any, polecats any) *OrchestrationPlane {
	t.Helper()
	rigJSON, err := json.Marshal(rigs)
	if err != nil {
		t.Fatalf("marshal rigs: %v", err)
	}
	pcJSON, err := json.Marshal(polecats)
	if err != nil {
		t.Fatalf("marshal polecats: %v", err)
	}
	s := &stubRunner{
		t: t,
		response: map[string][]byte{
			"rig list --json":           rigJSON,
			"polecat list --all --json": pcJSON,
		},
	}
	return &OrchestrationPlane{run: s.run}
}

func sampleRigs() []gtRig {
	return []gtRig{
		{Name: "gemba", Prefix: "gm", Status: "operational", Witness: "running", Refinery: "running", Polecats: 1, Crew: 1},
		{Name: "lume_spark_api", Prefix: "lsa", Status: "operational", Witness: "running", Refinery: "running", Polecats: 2, Crew: 1},
		{Name: "foolery", Prefix: "fo", Status: "stopped", Witness: "stopped", Refinery: "stopped", Polecats: 0, Crew: 0},
	}
}

func samplePolecats() []gtPolecat {
	return []gtPolecat{
		{Rig: "gemba", Name: "topaz", State: "working", Issue: "gm-e7.1", SessionRunning: true},
		{Rig: "lume_spark_api", Name: "jasper", State: "working", SessionRunning: true},
		{Rig: "lume_spark_api", Name: "mayor", State: "idle", SessionRunning: false},
	}
}

func TestDescribeManifestShape(t *testing.T) {
	m := (&OrchestrationPlane{}).Describe()
	if m.AdaptorID != "gastown" {
		t.Errorf("AdaptorID=%q, want gastown", m.AdaptorID)
	}
	if m.OrchestrationAPIVersion != core.ProtocolVersion {
		t.Errorf("OrchestrationAPIVersion=%q, want %q", m.OrchestrationAPIVersion, core.ProtocolVersion)
	}
	haveStatic, havePool := false, false
	for _, mode := range m.GroupModes {
		if mode == core.GroupStatic {
			haveStatic = true
		}
		if mode == core.GroupPool {
			havePool = true
		}
	}
	if !haveStatic || !havePool {
		t.Errorf("GroupModes=%v missing static+pool (DD-7 / bead gm-e7.1)", m.GroupModes)
	}
}

func TestListAgentsEnumeratesTownRigsAndPolecats(t *testing.T) {
	o := newAdaptor(t, sampleRigs(), samplePolecats())

	agents, err := o.ListAgents(context.Background(), core.AgentFilter{})
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}

	byID := make(map[core.AgentID]core.AgentRef, len(agents))
	for _, a := range agents {
		byID[a.ID] = a
	}

	wantIDs := []core.AgentID{
		"gastown/mayor",
		"gastown/deacon",
		"gastown/gemba/witness",
		"gastown/gemba/refinery",
		"gastown/gemba/crew/0",
		"gastown/lume_spark_api/witness",
		"gastown/lume_spark_api/refinery",
		"gastown/lume_spark_api/crew/0",
		"gastown/gemba/polecats/topaz",
		"gastown/lume_spark_api/polecats/jasper",
		"gastown/lume_spark_api/polecats/mayor",
	}
	for _, id := range wantIDs {
		if _, ok := byID[id]; !ok {
			t.Errorf("ListAgents missing expected agent %q", id)
		}
	}

	// Stopped rig (foolery) must contribute no standing agents.
	for id := range byID {
		if strings.HasPrefix(string(id), "gastown/foolery/") {
			t.Errorf("stopped rig leaked into ListAgents: %q", id)
		}
	}

	// Role field populated on every agent (DoD: UI branches on role).
	for _, a := range agents {
		if a.Role == "" {
			t.Errorf("agent %q has empty Role; DoD requires role taxonomy on every agent", a.ID)
		}
	}
}

func TestListAgentsFilter(t *testing.T) {
	o := newAdaptor(t, sampleRigs(), samplePolecats())

	got, err := o.ListAgents(context.Background(), core.AgentFilter{Workspace: "gemba"})
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	for _, a := range got {
		if a.Workspace != "gemba" {
			t.Errorf("Workspace filter leaked agent from %q", a.Workspace)
		}
	}
	if len(got) == 0 {
		t.Error("Workspace filter returned zero agents for gemba rig")
	}
}

func TestListGroupsRolesAndPools(t *testing.T) {
	o := newAdaptor(t, sampleRigs(), samplePolecats())

	groups, err := o.ListGroups(context.Background())
	if err != nil {
		t.Fatalf("ListGroups: %v", err)
	}

	byID := make(map[string]core.AgentGroup, len(groups))
	for _, g := range groups {
		byID[g.ID] = g
	}

	// Four role groups (mayor, deacon, witness, refinery) must exist and
	// each MUST carry a role key in Extension — DoD: "role taxonomy
	// preserved via AgentGroup.extensions['role']".
	wantRoles := map[string]string{
		"gastown/role/mayor":    RoleMayor,
		"gastown/role/deacon":   RoleDeacon,
		"gastown/role/witness":  RoleWitness,
		"gastown/role/refinery": RoleRefinery,
	}
	for id, role := range wantRoles {
		g, ok := byID[id]
		if !ok {
			t.Errorf("missing group %q", id)
			continue
		}
		if g.Mode != core.GroupStatic {
			t.Errorf("group %q: Mode=%q, want static (convoy)", id, g.Mode)
		}
		if got, _ := g.Extension["role"].(string); got != role {
			t.Errorf("group %q: Extension[role]=%q, want %q", id, got, role)
		}
	}

	// Polecat pools: one per rig the CLI returned, each with mode=pool
	// and Extension[role]="polecats".
	for _, rigName := range []string{"gemba", "lume_spark_api", "foolery"} {
		id := "gastown/rig/" + rigName + "/polecats"
		g, ok := byID[id]
		if !ok {
			t.Errorf("missing per-rig polecat pool %q", id)
			continue
		}
		if g.Mode != core.GroupPool {
			t.Errorf("group %q: Mode=%q, want pool", id, g.Mode)
		}
		if got, _ := g.Extension["role"].(string); got != RolePolecats {
			t.Errorf("group %q: Extension[role]=%q, want %q", id, got, RolePolecats)
		}
		if g.Members.PoolCheckURL == "" {
			t.Errorf("group %q: Members.PoolCheckURL empty; pool groups MUST declare a check url", id)
		}
	}

	// The witness/refinery role groups must enumerate every running rig.
	wm := byID["gastown/role/witness"].Members.Static
	rm := byID["gastown/role/refinery"].Members.Static
	if len(wm) != 2 || len(rm) != 2 {
		t.Errorf("witness/refinery members: wm=%v rm=%v; want 2 each (gemba + lume_spark_api)", wm, rm)
	}
}

func TestListGroupsJSONRoundTrip(t *testing.T) {
	o := newAdaptor(t, sampleRigs(), samplePolecats())
	groups, err := o.ListGroups(context.Background())
	if err != nil {
		t.Fatalf("ListGroups: %v", err)
	}
	for _, g := range groups {
		b, err := json.Marshal(g)
		if err != nil {
			t.Fatalf("marshal group %q: %v", g.ID, err)
		}
		var round core.AgentGroup
		if err := json.Unmarshal(b, &round); err != nil {
			t.Fatalf("unmarshal group %q: %v", g.ID, err)
		}
		if round.ID != g.ID || round.Mode != g.Mode {
			t.Errorf("round-trip drift on %q: got %+v", g.ID, round)
		}
		if got, _ := round.Extension["role"].(string); got == "" {
			t.Errorf("round-trip dropped Extension[role] on %q", g.ID)
		}
	}
}

func TestResolveGroupMembersStaticAndPool(t *testing.T) {
	o := newAdaptor(t, sampleRigs(), samplePolecats())
	ctx := context.Background()

	// Static role group: mayor has exactly one member with the right id.
	got, err := o.ResolveGroupMembers(ctx, "gastown/role/mayor")
	if err != nil {
		t.Fatalf("ResolveGroupMembers(mayor): %v", err)
	}
	if len(got) != 1 || got[0].ID != agentIDMayor {
		t.Errorf("mayor members: got %+v, want single agent %q", got, agentIDMayor)
	}

	// Pool group: polecats for lume_spark_api → two active polecats.
	got, err = o.ResolveGroupMembers(ctx, "gastown/rig/lume_spark_api/polecats")
	if err != nil {
		t.Fatalf("ResolveGroupMembers(lume_spark_api polecats): %v", err)
	}
	if len(got) != 2 {
		t.Errorf("lume_spark_api polecats: got %d, want 2", len(got))
	}
	for _, a := range got {
		if a.Role != RolePolecats {
			t.Errorf("polecat pool member role=%q, want %q", a.Role, RolePolecats)
		}
	}
}

func TestResolveGroupMembersUnknownTagged(t *testing.T) {
	o := newAdaptor(t, sampleRigs(), samplePolecats())
	_, err := o.ResolveGroupMembers(context.Background(), "gastown/role/does-not-exist")
	if err == nil {
		t.Fatal("expected error for unknown group")
	}
	ae := core.AsAdaptorError(err)
	if ae == nil {
		t.Fatalf("expected *core.AdaptorError, got %T", err)
	}
	if ae.Kind != core.KindSessionNotFound {
		t.Errorf("error kind=%q, want %q", ae.Kind, core.KindSessionNotFound)
	}
}

func TestUnsupportedMethodsReturnTaggedError(t *testing.T) {
	o := &OrchestrationPlane{}
	_, err := o.StartSession(context.Background(), "any", core.SessionPrompt{})
	if err == nil {
		t.Fatal("StartSession must return an error in gm-e7.1")
	}
	ae := core.AsAdaptorError(err)
	if ae == nil {
		t.Fatalf("expected *core.AdaptorError, got %T", err)
	}
	if ae.Kind != core.KindUnsupported {
		t.Errorf("StartSession kind=%q, want %q", ae.Kind, core.KindUnsupported)
	}
}

// TestObservedStateMergesAgentsAndGroups shows the SPA path: a single
// ObservedState call returns a topology with both populated so the UI
// doesn't have to fan out for every refresh.
func TestObservedStateMergesAgentsAndGroups(t *testing.T) {
	o := newAdaptor(t, sampleRigs(), samplePolecats())
	topo, err := o.ObservedState(context.Background())
	if err != nil {
		t.Fatalf("ObservedState: %v", err)
	}
	if len(topo.Agents) == 0 {
		t.Error("ObservedState returned zero agents")
	}
	if len(topo.Groups) == 0 {
		t.Error("ObservedState returned zero groups")
	}
	if topo.CapturedAt.IsZero() {
		t.Error("ObservedState.CapturedAt must be set")
	}
	// Ensure every static-group member appears in Agents (no dangling refs).
	agentIDs := make(map[core.AgentID]struct{}, len(topo.Agents))
	for _, a := range topo.Agents {
		agentIDs[a.ID] = struct{}{}
	}
	for _, g := range topo.Groups {
		if g.Mode != core.GroupStatic {
			continue
		}
		for _, id := range g.Members.Static {
			if _, ok := agentIDs[id]; !ok {
				t.Errorf("group %q references unknown agent %q", g.ID, id)
			}
		}
	}
}

// Compile-time assertion: *OrchestrationPlane satisfies the interface.
var _ = func() core.OrchestrationPlaneAdaptor {
	return (*OrchestrationPlane)(nil)
}
