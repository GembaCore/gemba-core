// Tests for the OperationalContext read API (gm-s47n.2.5).
//
// Table-driven where it makes sense; explicit-test where the wiring
// of one missing reader vs another differs. Every reader is faked
// inline so a regression in the join logic surfaces here, not in a
// downstream consumer that depends on a sibling adaptor.

package planner

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MikeBengtson/gemba/internal/core"
)

// ── fakes ──────────────────────────────────────────────────────────

type fakeSessionLookup struct {
	byID map[string]*core.Session
	err  error
}

func (f fakeSessionLookup) FindSession(_ context.Context, id string) (*core.Session, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.byID[id], nil
}

type fakeAgentLookup struct {
	byID map[core.AgentID]*core.AgentRef
	err  error
}

func (f fakeAgentLookup) ReadAgent(_ context.Context, id core.AgentID) (*core.AgentRef, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.byID[id], nil
}

type fakeAssignmentLookup struct {
	byID map[string]*core.Assignment
	err  error
}

func (f fakeAssignmentLookup) FindAssignment(_ context.Context, id string) (*core.Assignment, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.byID[id], nil
}

type fakeWorkspaceLookup struct {
	byID map[string]core.Workspace
	err  error
}

func (f fakeWorkspaceLookup) InspectWorkspace(_ context.Context, id string) (core.Workspace, error) {
	if f.err != nil {
		return core.Workspace{}, f.err
	}
	if ws, ok := f.byID[id]; ok {
		return ws, nil
	}
	return core.Workspace{}, errors.New("workspace not found")
}

type fakeProfileLookup struct {
	byID map[string]*SessionProfile
	err  error
}

func (f fakeProfileLookup) GetProfile(_ context.Context, id string) (*SessionProfile, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.byID[id], nil
}

// ── helpers ────────────────────────────────────────────────────────

func startedAt(min time.Duration) time.Time {
	return time.Date(2026, 4, 26, 10, 0, 0, 0, time.UTC).Add(-min)
}

func fixedNow() time.Time {
	return time.Date(2026, 4, 26, 10, 0, 0, 0, time.UTC)
}

// fixture seeds a complete reader bundle for a single session that
// exercises every component of the join. Tests then mutate the
// bundle (zero out a reader, swap an error in) to exercise specific
// paths without rebuilding the seed each time.
func fixture() (string, OperationalContextReaders) {
	sess := &core.Session{
		ID:           "sess-1",
		AssignmentID: "asn-1",
		AgentID:      "agent-1",
		Status:       core.SessionWorking,
		StartedAt:    startedAt(20 * time.Minute),
	}
	agent := &core.AgentRef{ID: "agent-1", Name: "planner-agent"}
	assignment := &core.Assignment{
		ID:          "asn-1",
		WorkItemID:  "gm-1",
		AgentID:     "agent-1",
		WorkspaceID: "ws-1",
		SessionID:   "sess-1",
		Status:      core.AssignmentActive,
	}
	workspace := core.Workspace{
		ID:         "ws-1",
		Kind:       "worktree",
		Repository: "github.com/MikeBengtson/gemba",
		Branch:     "main",
		Status:     core.WorkspaceInUse,
	}
	profile := &SessionProfile{
		SessionID:        "sess-1",
		AssignmentID:     "asn-1",
		AgentID:          "agent-1",
		Concepts:         map[ConceptTag]float64{"glob": 0.7, "planner": 0.4},
		Files:            map[string]float64{"internal/planner/targets/targets.go": 0.9},
		TokensUsed:       40_000,
		ContextWindowMax: 200_000,
		ContextPct:       0.2,
		LastBeads:        []core.WorkItemID{"gm-s47n.4.1"},
		LastActivityAt:   startedAt(5 * time.Minute),
	}

	return sess.ID, OperationalContextReaders{
		Sessions:    fakeSessionLookup{byID: map[string]*core.Session{sess.ID: sess}},
		Agents:      fakeAgentLookup{byID: map[core.AgentID]*core.AgentRef{agent.ID: agent}},
		Assignments: fakeAssignmentLookup{byID: map[string]*core.Assignment{assignment.ID: assignment}},
		Workspaces:  fakeWorkspaceLookup{byID: map[string]core.Workspace{workspace.ID: workspace}},
		Profiles:    fakeProfileLookup{byID: map[string]*SessionProfile{sess.ID: profile}},
		Now:         fixedNow,
	}
}

// ── tests ──────────────────────────────────────────────────────────

func TestReadOperationalContext_HappyPath(t *testing.T) {
	id, r := fixture()
	got, err := ReadOperationalContext(context.Background(), id, r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Session == nil || got.Session.ID != "sess-1" {
		t.Errorf("Session = %+v; want sess-1", got.Session)
	}
	if got.Agent == nil || got.Agent.ID != "agent-1" {
		t.Errorf("Agent = %+v; want agent-1", got.Agent)
	}
	if got.Assignment == nil || got.Assignment.ID != "asn-1" {
		t.Errorf("Assignment = %+v; want asn-1", got.Assignment)
	}
	if got.Workspace == nil || got.Workspace.ID != "ws-1" {
		t.Errorf("Workspace = %+v; want ws-1", got.Workspace)
	}
	if got.Profile == nil || got.Profile.ContextPct != 0.2 {
		t.Errorf("Profile = %+v; want ContextPct=0.2", got.Profile)
	}
	if got.Health == nil {
		t.Fatal("Health is nil; want a derived snapshot")
	}
	if got.Health.ContextPressure != 0.2 {
		t.Errorf("Health.ContextPressure = %v; want 0.2 (from profile.ContextPct)", got.Health.ContextPressure)
	}
	if got.Health.TimeOnTask != 20*time.Minute {
		t.Errorf("Health.TimeOnTask = %v; want 20m", got.Health.TimeOnTask)
	}
}

func TestReadOperationalContext_RequiresSessionLookup(t *testing.T) {
	_, err := ReadOperationalContext(context.Background(), "sess-1", OperationalContextReaders{})
	if !errors.Is(err, ErrSessionLookupRequired) {
		t.Errorf("err = %v; want ErrSessionLookupRequired", err)
	}
}

func TestReadOperationalContext_SessionNotFound(t *testing.T) {
	_, r := fixture()
	r.Sessions = fakeSessionLookup{byID: map[string]*core.Session{}}
	_, err := ReadOperationalContext(context.Background(), "missing", r)
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("err = %v; want ErrSessionNotFound", err)
	}
}

func TestReadOperationalContext_SessionLookupErrorPropagates(t *testing.T) {
	id, r := fixture()
	r.Sessions = fakeSessionLookup{err: errors.New("boom")}
	_, err := ReadOperationalContext(context.Background(), id, r)
	if err == nil || err.Error() != "boom" {
		t.Errorf("err = %v; want 'boom'", err)
	}
}

func TestReadOperationalContext_NilReadersDegradeGracefully(t *testing.T) {
	id, r := fixture()
	// Drop every optional reader; only Sessions remains.
	r.Agents = nil
	r.Assignments = nil
	r.Workspaces = nil
	r.Profiles = nil
	got, err := ReadOperationalContext(context.Background(), id, r)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if got.Session == nil {
		t.Error("Session should still be populated")
	}
	if got.Agent != nil || got.Assignment != nil || got.Workspace != nil || got.Profile != nil {
		t.Errorf("expected nils for skipped readers; got %+v", got)
	}
	if got.Health == nil {
		t.Error("Health should always be derived from session even without a profile")
	}
	// Without profile, ContextPressure is zero (no telemetry yet).
	if got.Health.ContextPressure != 0 {
		t.Errorf("ContextPressure = %v; want 0 (no profile)", got.Health.ContextPressure)
	}
}

func TestReadOperationalContext_AgentLookupErrorIsSwallowed(t *testing.T) {
	id, r := fixture()
	r.Agents = fakeAgentLookup{err: errors.New("agent service down")}
	got, err := ReadOperationalContext(context.Background(), id, r)
	if err != nil {
		t.Fatalf("agent lookup errors must NOT propagate; got %v", err)
	}
	if got.Agent != nil {
		t.Errorf("Agent should be nil when lookup errors; got %+v", got.Agent)
	}
	// Other components still populate.
	if got.Session == nil || got.Assignment == nil || got.Workspace == nil {
		t.Errorf("downstream components must still be present: %+v", got)
	}
}

func TestReadOperationalContext_WorkspaceRequiresAssignment(t *testing.T) {
	id, r := fixture()
	// Drop assignment; workspace should NOT be reachable even though
	// Workspaces reader is wired — the join goes through assignment.
	r.Assignments = nil
	got, err := ReadOperationalContext(context.Background(), id, r)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if got.Workspace != nil {
		t.Errorf("Workspace should be nil without Assignment; got %+v", got.Workspace)
	}
}

func TestReadOperationalContext_ProfileMissingProducesEmptyHealth(t *testing.T) {
	id, r := fixture()
	r.Profiles = fakeProfileLookup{byID: map[string]*SessionProfile{}}
	got, err := ReadOperationalContext(context.Background(), id, r)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if got.Profile != nil {
		t.Errorf("Profile should be nil when GetProfile returns nil; got %+v", got.Profile)
	}
	if got.Health == nil {
		t.Fatal("Health must still derive from session alone")
	}
	if got.Health.ContextPressure != 0 {
		t.Errorf("ContextPressure = %v; want 0 (no profile)", got.Health.ContextPressure)
	}
	if got.Health.TimeOnTask != 20*time.Minute {
		t.Errorf("TimeOnTask = %v; want 20m", got.Health.TimeOnTask)
	}
}

func TestReadOperationalContext_NowFallsBackToTimeNow(t *testing.T) {
	id, r := fixture()
	r.Now = nil // force fallback
	got, err := ReadOperationalContext(context.Background(), id, r)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if got.Health == nil {
		t.Fatal("Health is nil")
	}
	// We can't assert the exact value because Now is wall clock, but
	// it must be sensible: positive (StartedAt is in the past) and
	// not absurd.
	if got.Health.TimeOnTask <= 0 {
		t.Errorf("TimeOnTask = %v; want positive (StartedAt is in the past)", got.Health.TimeOnTask)
	}
}

func TestReadOperationalContext_AssignmentWithoutWorkspaceLeavesWorkspaceNil(t *testing.T) {
	id, r := fixture()
	// Mutate the assignment to drop its WorkspaceID.
	stripped := *r.Assignments.(fakeAssignmentLookup).byID["asn-1"]
	stripped.WorkspaceID = ""
	r.Assignments = fakeAssignmentLookup{byID: map[string]*core.Assignment{"asn-1": &stripped}}
	got, err := ReadOperationalContext(context.Background(), id, r)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if got.Assignment == nil {
		t.Fatal("Assignment should populate")
	}
	if got.Workspace != nil {
		t.Errorf("Workspace should be nil when assignment.WorkspaceID is empty; got %+v", got.Workspace)
	}
}
