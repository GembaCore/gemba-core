package bd_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MikeBengtson/gemba/internal/adapter/bd"
	"github.com/MikeBengtson/gemba/internal/core"
	gembatesting "github.com/MikeBengtson/gemba/testing"
)

// fakeBd is a stateful in-memory stand-in for the bd CLI. It understands
// the subset of subcommands the WorkPlane adaptor issues (list / show /
// create / update) so the conformance harness can exercise full CRUD
// round-trips without spawning processes or touching a real Dolt server.
//
// Behavior matches `bd --json` contract: show / list return an array of
// beads; create emits the materialized row; unknown ids return a
// not-found exit error. Unknown subcommands fail the test so the
// adaptor can't silently issue commands this fake hasn't modeled.
type fakeBd struct {
	mu     sync.Mutex
	t      *testing.T
	beads  map[string]*bd.Bead
	nextID int
}

func newFakeBd(t *testing.T) *fakeBd {
	t.Helper()
	return &fakeBd{
		t:     t,
		beads: map[string]*bd.Bead{},
	}
}

// run is the Runner closure the adaptor calls in place of exec.
// Parsing is intentionally flag-by-flag rather than spf13/cobra-y:
// the fake only needs to recognize the flags the adaptor emits.
func (f *fakeBd) run(_ context.Context, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(args) == 0 {
		f.t.Fatalf("fakeBd: empty args")
	}
	switch args[0] {
	case "list":
		return f.handleList(args[1:])
	case "show":
		return f.handleShow(args[1:])
	case "create":
		return f.handleCreate(args[1:])
	case "update":
		return f.handleUpdate(args[1:])
	default:
		f.t.Fatalf("fakeBd: unexpected subcommand %q (args=%v)", args[0], args)
	}
	return nil, nil
}

func (f *fakeBd) handleList(args []string) ([]byte, error) {
	statusFilter := ""
	limit := 0
	var labelFilter []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--status":
			statusFilter = args[i+1]
			i++
		case "--limit":
			n, _ := strconv.Atoi(args[i+1])
			limit = n
			i++
		case "--label":
			labelFilter = strings.Split(args[i+1], ",")
			i++
		case "--json":
			// default
		}
	}
	hasAll := func(b *bd.Bead, need []string) bool {
		for _, want := range need {
			found := false
			for _, have := range b.Labels {
				if have == want {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
		return true
	}
	out := make([]bd.Bead, 0, len(f.beads))
	for _, b := range f.beads {
		if statusFilter != "" && b.Status != statusFilter {
			continue
		}
		if len(labelFilter) > 0 && !hasAll(b, labelFilter) {
			continue
		}
		out = append(out, *b)
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return json.Marshal(out)
}

func (f *fakeBd) handleShow(args []string) ([]byte, error) {
	if len(args) < 1 {
		f.t.Fatalf("fakeBd: show missing id")
	}
	id := args[0]
	b, ok := f.beads[id]
	if !ok {
		return nil, &fakeExitError{stderr: []byte("bd: issue not found: " + id + "\n")}
	}
	return json.Marshal([]bd.Bead{*b})
}

func (f *fakeBd) handleCreate(args []string) ([]byte, error) {
	if len(args) < 1 {
		f.t.Fatalf("fakeBd: create missing title")
	}
	b := &bd.Bead{
		Title:     args[0],
		Status:    "open",
		IssueType: "task",
		Priority:  2,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--type":
			b.IssueType = args[i+1]
			i++
		case "--priority":
			n, _ := strconv.Atoi(args[i+1])
			b.Priority = n
			i++
		case "--description":
			b.Description = args[i+1]
			i++
		case "--labels":
			b.Labels = strings.Split(args[i+1], ",")
			i++
		case "--assignee":
			b.Assignee = args[i+1]
			i++
		case "--json":
			// default
		}
	}
	f.nextID++
	b.ID = fmt.Sprintf("test-%03d", f.nextID)
	f.beads[b.ID] = b
	return json.Marshal(b)
}

func (f *fakeBd) handleUpdate(args []string) ([]byte, error) {
	if len(args) < 1 {
		f.t.Fatalf("fakeBd: update missing id")
	}
	id := args[0]
	b, ok := f.beads[id]
	if !ok {
		return nil, &fakeExitError{stderr: []byte("bd: issue not found: " + id + "\n")}
	}
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--title":
			b.Title = args[i+1]
			i++
		case "--description":
			b.Description = args[i+1]
			i++
		case "--status":
			b.Status = args[i+1]
			i++
		case "--priority":
			n, _ := strconv.Atoi(args[i+1])
			b.Priority = n
			i++
		case "--assignee":
			b.Assignee = args[i+1]
			i++
		case "--set-labels":
			b.Labels = strings.Split(args[i+1], ",")
			i++
		case "--add-label":
			b.Labels = append(b.Labels, args[i+1])
			i++
		case "--json":
			// default
		}
	}
	b.UpdatedAt = time.Now().UTC()
	return json.Marshal(b)
}

// fakeExitError lets the fake report a non-zero bd exit with a stderr
// payload the adaptor's isNotFoundError scan can key off. We don't use
// *exec.ExitError because its fields are unexported; a plain error
// whose Error() contains "not found" is equivalent for the adaptor's
// string-scan path.
type fakeExitError struct {
	stderr []byte
}

func (e *fakeExitError) Error() string { return string(e.stderr) }

func TestBeadsWorkPlaneConformance(t *testing.T) {
	fake := newFakeBd(t)
	impl := bd.NewWorkPlaneWithRunner(fake.run, "")
	gembatesting.RunWorkPlaneConformance(t, impl, &gembatesting.WorkPlaneFixture{
		KnownMissingID: core.WorkItemID("gemba/gemba/gm-does-not-exist"),
		// Seed a bead with every core edge type so Group C can assert
		// the adaptor hydrates core Relationships from bd's native
		// dependencies/dependents arrays. Extension edges are exercised
		// separately in TestBeadsEdgeMappingRoundTripsAllSevenKinds.
		SeedWorkItemWithEdges: func(wp core.WorkPlane) (core.WorkItemID, []core.Relationship, error) {
			fake.mu.Lock()
			fake.beads["gm-edges-subject"] = &bd.Bead{
				ID:        "gm-edges-subject",
				Title:     "edge-round-trip subject",
				Status:    "open",
				Priority:  2,
				IssueType: "task",
				Dependencies: []bd.BeadEdge{
					{IssueID: "gm-edges-subject", DependsOnID: "gm-edges-blocker", Type: "blocks"},
					{IssueID: "gm-edges-subject", DependsOnID: "gm-edges-parent", Type: "parent-child"},
				},
				Dependents: []bd.BeadEdge{
					{ID: "gm-edges-sibling", DependencyType: "relates_to"},
				},
			}
			fake.mu.Unlock()
			id := core.WorkItemID("gemba/gemba/gm-edges-subject")
			want := []core.Relationship{
				{Kind: core.RelBlocks, From: "gemba/gemba/gm-edges-blocker", To: id},
				{Kind: core.RelParentChild, From: "gemba/gemba/gm-edges-parent", To: id},
				{Kind: core.RelRelatesTo, From: id, To: "gemba/gemba/gm-edges-sibling"},
			}
			return id, want, nil
		},
	})
}

func TestBeadsToWorkItemRoundTripPreservesExtensions(t *testing.T) {
	// A Beads-specific roster of fields the core contract has no slot
	// for. Round-trip through the extension channel MUST preserve every
	// one — that's the DoD's second bullet ("Round-trip preserves all
	// Beads-specific fields via the extension channel").
	started := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	src := bd.Bead{
		ID:          "gm-abc",
		Title:       "hello",
		Description: "body",
		Status:      "in_progress",
		Priority:    1,
		IssueType:   "decision",
		Assignee:    "gemba/polecats/onyx",
		Owner:       "mike.bengtson@gmail.com",
		CreatedAt:   time.Now().UTC(),
		CreatedBy:   "mike",
		UpdatedAt:   time.Now().UTC(),
		StartedAt:   &started,
		Labels:      []string{"area:core", "tier:sonnet"},
		Notes:       "some notes",
		Parent:      "gm-root",
		Dependencies: []bd.BeadEdge{
			{IssueID: "gm-abc", DependsOnID: "gm-xyz", Type: "blocks"},
		},
	}
	fake := newFakeBd(t)
	fake.beads[src.ID] = &src
	impl := bd.NewWorkPlaneWithRunner(fake.run, "")

	wi, err := impl.GetWorkItem(context.Background(), core.WorkItemID("gemba/gemba/gm-abc"))
	if err != nil {
		t.Fatalf("GetWorkItem: %v", err)
	}

	// Core fields projected cleanly.
	if wi.Title != "hello" {
		t.Errorf("Title=%q want hello", wi.Title)
	}
	if wi.Kind != "decision" {
		t.Errorf("Kind=%q want decision", wi.Kind)
	}
	if wi.Status != "in_progress" {
		t.Errorf("Status=%q want in_progress", wi.Status)
	}
	if wi.StateCategory != core.StateStarted {
		t.Errorf("StateCategory=%q want %q", wi.StateCategory, core.StateStarted)
	}
	if wi.Priority == nil || *wi.Priority != 1 {
		t.Errorf("Priority=%v want 1", wi.Priority)
	}

	// Extension channel carries every Beads-specific field.
	if v, _ := wi.Custom["beads:issue_type"].(string); v != "decision" {
		t.Errorf("Custom[beads:issue_type]=%q want decision", v)
	}
	if v, _ := wi.Custom["beads:notes"].(string); v != "some notes" {
		t.Errorf("Custom[beads:notes]=%q want %q", v, "some notes")
	}
	if v, _ := wi.Custom["beads:parent"].(string); v != "gm-root" {
		t.Errorf("Custom[beads:parent]=%q want gm-root", v)
	}
	if v, _ := wi.Custom["beads:created_by"].(string); v != "mike" {
		t.Errorf("Custom[beads:created_by]=%q want mike", v)
	}
	if _, ok := wi.Custom["beads:started_at"]; !ok {
		t.Error("Custom[beads:started_at] missing")
	}
	if _, ok := wi.Custom["beads:dependencies"]; !ok {
		t.Error("Custom[beads:dependencies] missing — edge round-trip is the DoD for gm-e6.2 but must survive here too")
	}
}

func TestBeadsDescribeManifestIsValid(t *testing.T) {
	impl := bd.NewWorkPlaneWithRunner(newFakeBd(t).run, "")
	m, err := impl.Describe(context.Background())
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("manifest.Validate: %v", err)
	}
	if m.AdaptorName != "beads" {
		t.Errorf("AdaptorName=%q want beads", m.AdaptorName)
	}
	if m.ProtocolVersion != core.ProtocolVersion {
		t.Errorf("ProtocolVersion=%q want %q", m.ProtocolVersion, core.ProtocolVersion)
	}
	// beads descriptions ARE markdown (bd's own edit surface assumes
	// it). Default must carry that declaration so the SPA renders
	// headings / lists / code fences instead of flat preformatted text.
	if m.DescriptionFormat != core.DescriptionFormatMarkdown {
		t.Errorf("DescriptionFormat=%q want %q",
			m.DescriptionFormat, core.DescriptionFormatMarkdown)
	}
}

// gm-ekr / gm-zdx: the bd adaptor projects the R1-R8 agentic-data-
// plane shape declared in the gmp-phc audit. Any drift here is a
// conformance gap — the audit bead files a follow-up when one lands.
func TestBeadsManifestProjectsR1ThroughR8(t *testing.T) {
	impl := bd.NewWorkPlaneWithRunner(newFakeBd(t).run, "")
	m, err := impl.Describe(context.Background())
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}

	if m.SchemaEnforcement != core.SchemaNative {
		t.Errorf("R1 schema_enforcement: got %q, want %q",
			m.SchemaEnforcement, core.SchemaNative)
	}

	hasQL := map[core.QueryLanguage]bool{}
	for _, q := range m.QueryLanguages {
		hasQL[q] = true
	}
	for _, want := range []core.QueryLanguage{core.QueryFilterOnly, core.QuerySQLSubset} {
		if !hasQL[want] {
			t.Errorf("R2 query_languages missing %q; got %v", want, m.QueryLanguages)
		}
	}

	if !m.DependencyGraphNative {
		t.Error("R3 dependency_graph_native should be true for Beads")
	}
	if !m.ReadySetQuery {
		t.Error("R3 ready_set_query should be true for Beads (bd ready)")
	}

	hasVT := map[core.VersioningTransport]bool{}
	for _, v := range m.VersioningTransport {
		hasVT[v] = true
	}
	for _, want := range []core.VersioningTransport{
		core.VersioningGit, core.VersioningDolt, core.VersioningJSONL,
	} {
		if !hasVT[want] {
			t.Errorf("R4 versioning_transport missing %q; got %v",
				want, m.VersioningTransport)
		}
	}

	if m.ConcurrencyModel != core.ConcurrencyDoltMerge {
		t.Errorf("R5 concurrency_model: got %q, want %q",
			m.ConcurrencyModel, core.ConcurrencyDoltMerge)
	}
	if !m.AgentSessionDecoupling {
		t.Error("R6 agent_session_decoupling must be true")
	}
	if m.AgentNativeAPI != core.AgentAPICLI {
		t.Errorf("R7 agent_native_api: got %q, want %q",
			m.AgentNativeAPI, core.AgentAPICLI)
	}

	hasHook := map[core.OrchestratorHook]bool{}
	for _, h := range m.OrchestratorHooks {
		hasHook[h] = true
	}
	for _, want := range []core.OrchestratorHook{
		core.HookReadySetSubscribe, core.HookClaimAtomic,
		core.HookEscalationIngest, core.HookWorkCompleteAck,
	} {
		if !hasHook[want] {
			t.Errorf("R8 orchestrator_hooks missing %q; got %v",
				want, m.OrchestratorHooks)
		}
	}

	// End-to-end: the manifest must clear the minimum bar.
	if ok, reasons := m.MinimumBar(); !ok {
		t.Errorf("Beads manifest below bar: %v", reasons)
	}
}

func TestBeadsGetWorkItemNotFoundIsTagged(t *testing.T) {
	impl := bd.NewWorkPlaneWithRunner(newFakeBd(t).run, "")
	_, err := impl.GetWorkItem(context.Background(), core.WorkItemID("gemba/gemba/gm-missing"))
	if err == nil {
		t.Fatal("expected error for missing bead")
	}
	if ae := core.AsAdaptorError(err); ae == nil || ae.Kind != core.KindSessionNotFound {
		t.Errorf("expected session_not_found AdaptorError, got %T (%v)", err, err)
	}
	if !errors.Is(err, core.ErrNotFound) {
		t.Errorf("errors.Is(err, core.ErrNotFound) = false; legacy sentinel must still match")
	}
}

// TestBeadsEdgeMappingRoundTripsAllSevenKinds covers gm-e6.2's DoD: a
// bead carrying every one of Beads's 7 native edge types (3 core + 4
// extensions) survives a load through Gemba with every edge still
// addressable — core edges on WorkItem.Relationships, extensions on
// Custom["beads:dependencies"]/["beads:dependents"] in their raw shape
// for the SPA's beads/ extension renderer.
func TestBeadsEdgeMappingRoundTripsAllSevenKinds(t *testing.T) {
	fake := newFakeBd(t)
	source := &bd.Bead{
		ID:        "gm-seven",
		Title:     "all seven edge kinds",
		Status:    "open",
		Priority:  2,
		IssueType: "task",
		Dependencies: []bd.BeadEdge{
			// 3 core edges inbound.
			{IssueID: "gm-seven", DependsOnID: "gm-block", Type: "blocks"},
			{IssueID: "gm-seven", DependsOnID: "gm-epic", Type: "parent-child"},
			{IssueID: "gm-seven", DependsOnID: "gm-peer", Type: "relates_to"},
			// 4 extension edges inbound. They MUST NOT land on
			// Relationships (core does not know these kinds) and MUST
			// survive on Custom in their original shape.
			{IssueID: "gm-seven", DependsOnID: "gm-origin", Type: "discovered-from"},
			{IssueID: "gm-seven", DependsOnID: "gm-gate", Type: "waits_for"},
			{IssueID: "gm-seven", DependsOnID: "gm-thread", Type: "replies_to"},
			{IssueID: "gm-seven", DependsOnID: "gm-cond", Type: "conditional_blocks"},
		},
	}
	fake.beads[source.ID] = source

	impl := bd.NewWorkPlaneWithRunner(fake.run, "")
	wi, err := impl.GetWorkItem(context.Background(), core.WorkItemID("gemba/gemba/gm-seven"))
	if err != nil {
		t.Fatalf("GetWorkItem: %v", err)
	}

	// 3 core edges populated on Relationships with the right direction
	// (dependencies flow other -> self for every kind).
	self := core.WorkItemID("gemba/gemba/gm-seven")
	wantCore := []core.Relationship{
		{Kind: core.RelBlocks, From: "gemba/gemba/gm-block", To: self},
		{Kind: core.RelParentChild, From: "gemba/gemba/gm-epic", To: self},
		{Kind: core.RelRelatesTo, From: "gemba/gemba/gm-peer", To: self},
	}
	gotCore := make(map[core.Relationship]int, len(wi.Relationships))
	for _, r := range wi.Relationships {
		gotCore[r]++
	}
	for _, r := range wantCore {
		if gotCore[r] == 0 {
			t.Errorf("missing core Relationship %+v in %+v", r, wi.Relationships)
		}
		gotCore[r]--
	}
	// Any leftover entries mean we accidentally surfaced extension edges
	// (or some other artifact) on Relationships. That would be a DD-9
	// violation: core only knows three kinds.
	for r, n := range gotCore {
		if n > 0 {
			t.Errorf("unexpected core Relationship %+v (extension edges must NOT appear on Relationships)", r)
		}
	}

	// All 7 raw edges ride through on Custom["beads:dependencies"] so the
	// beads/ UI extension can render the 4 extension kinds without a
	// second query. Type-assert with the compiled adaptor's BeadEdge
	// type — the Custom slot holds the raw value, not a JSON decode.
	raw, ok := wi.Custom["beads:dependencies"].([]bd.BeadEdge)
	if !ok {
		t.Fatalf("Custom[beads:dependencies] missing or wrong type: %T", wi.Custom["beads:dependencies"])
	}
	if len(raw) != 7 {
		t.Errorf("Custom[beads:dependencies]: got %d edges, want 7 (all 7 native edges must survive)", len(raw))
	}
	seenTypes := make(map[string]bool, 7)
	for _, e := range raw {
		seenTypes[strings.ReplaceAll(e.Type, "-", "_")] = true
	}
	for _, k := range []string{
		"blocks", "parent_child", "relates_to",
		"discovered_from", "waits_for", "replies_to", "conditional_blocks",
	} {
		if !seenTypes[k] {
			t.Errorf("raw dependency type %q not preserved on Custom", k)
		}
	}
}

// TestBeadsManifestDeclaresFourEdgeExtensions anchors the Outputs
// bullet from gm-e6.2: the manifest's EdgeExtensions list names exactly
// the four Beads-native extension kinds the UI renders when the beads/
// extension bundle is loaded.
func TestBeadsManifestDeclaresFourEdgeExtensions(t *testing.T) {
	impl := bd.NewWorkPlaneWithRunner(newFakeBd(t).run, "")
	m, err := impl.Describe(context.Background())
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	want := map[string]bool{
		"beads:discovered_from":    false,
		"beads:waits_for":          false,
		"beads:replies_to":         false,
		"beads:conditional_blocks": false,
	}
	for _, ext := range m.EdgeExtensions {
		if _, ok := want[ext.Name]; ok {
			want[ext.Name] = true
			if !ext.Directed {
				t.Errorf("EdgeExtension %q: Directed=false, want true", ext.Name)
			}
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("EdgeExtension %q missing from manifest", name)
		}
	}
}

func TestBeadsCreateWorkItemThenListContainsIt(t *testing.T) {
	impl := bd.NewWorkPlaneWithRunner(newFakeBd(t).run, "")
	ctx := context.Background()
	created, err := impl.CreateWorkItem(ctx, core.WorkItem{
		Title:  "a task",
		Kind:   "task",
		Status: "open",
	})
	if err != nil {
		t.Fatalf("CreateWorkItem: %v", err)
	}
	if created.ID == "" {
		t.Fatal("created.ID empty; adaptor must return backend-assigned id")
	}

	items, err := impl.ListWorkItems(ctx, core.WorkItemFilter{})
	if err != nil {
		t.Fatalf("ListWorkItems: %v", err)
	}
	found := false
	for _, it := range items {
		if it.ID == created.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("ListWorkItems did not return just-created id %q (got %d items)", created.ID, len(items))
	}
}

// TestBeadsMilestoneLabelProjectsToKindMilestone asserts the read-path
// projection for the gm-root.3 Milestone convention: a bead of bd
// type "epic" carrying the `type:milestone` label surfaces as
// Kind=KindMilestone on core.WorkItem, while the underlying bd
// issue_type stays addressable on Custom for the beads/ UI extension.
func TestBeadsMilestoneLabelProjectsToKindMilestone(t *testing.T) {
	fake := newFakeBd(t)
	fake.beads["gm-ms-1"] = &bd.Bead{
		ID:        "gm-ms-1",
		Title:     "Milestone alpha",
		Status:    "open",
		Priority:  1,
		IssueType: "epic",
		Labels:    []string{"type:milestone", "area:core"},
	}
	fake.beads["gm-ep-1"] = &bd.Bead{
		ID:        "gm-ep-1",
		Title:     "Plain epic",
		Status:    "open",
		Priority:  2,
		IssueType: "epic",
		Labels:    []string{"area:core"},
	}
	impl := bd.NewWorkPlaneWithRunner(fake.run, "")

	ms, err := impl.GetWorkItem(context.Background(), core.WorkItemID("gemba/gemba/gm-ms-1"))
	if err != nil {
		t.Fatalf("GetWorkItem(milestone): %v", err)
	}
	if ms.Kind != core.KindMilestone {
		t.Errorf("milestone Kind=%q want %q", ms.Kind, core.KindMilestone)
	}
	// Original bd issue_type survives on the extension channel so the
	// beads/ SPA renderer can still show the "epic" chip.
	if v, _ := ms.Custom["beads:issue_type"].(string); v != "epic" {
		t.Errorf("Custom[beads:issue_type]=%q want epic", v)
	}

	ep, err := impl.GetWorkItem(context.Background(), core.WorkItemID("gemba/gemba/gm-ep-1"))
	if err != nil {
		t.Fatalf("GetWorkItem(epic): %v", err)
	}
	if ep.Kind != "epic" {
		t.Errorf("plain epic Kind=%q want epic (label-absent must NOT promote)", ep.Kind)
	}
}

// TestBeadsListWorkItemsFilterKindsMilestone exercises gm-root.3.5's
// DoD end-to-end: filtering ListWorkItems by Kinds=[KindMilestone]
// returns only beads carrying the type:milestone label. The adaptor
// pushes --label type:milestone down to bd; fakeBd's handleList
// honors --label, so a milestone-only filter must not return the
// non-milestone bead even from an unfiltered store.
func TestBeadsListWorkItemsFilterKindsMilestone(t *testing.T) {
	fake := newFakeBd(t)
	fake.beads["gm-ms-1"] = &bd.Bead{
		ID: "gm-ms-1", Title: "M1", Status: "open", Priority: 1,
		IssueType: "epic", Labels: []string{"type:milestone"},
	}
	fake.beads["gm-ms-2"] = &bd.Bead{
		ID: "gm-ms-2", Title: "M2", Status: "open", Priority: 1,
		IssueType: "epic", Labels: []string{"type:milestone", "area:spa"},
	}
	fake.beads["gm-tk-1"] = &bd.Bead{
		ID: "gm-tk-1", Title: "T1", Status: "open", Priority: 2,
		IssueType: "task",
	}
	fake.beads["gm-ep-1"] = &bd.Bead{
		ID: "gm-ep-1", Title: "E1", Status: "open", Priority: 2,
		IssueType: "epic",
	}
	impl := bd.NewWorkPlaneWithRunner(fake.run, "")

	got, err := impl.ListWorkItems(context.Background(), core.WorkItemFilter{
		Kinds: []string{core.KindMilestone},
	})
	if err != nil {
		t.Fatalf("ListWorkItems: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("milestone filter: got %d items, want 2 (%+v)", len(got), got)
	}
	for _, wi := range got {
		if wi.Kind != core.KindMilestone {
			t.Errorf("returned item Kind=%q want %q (id=%s)", wi.Kind, core.KindMilestone, wi.ID)
		}
	}
}

// TestBeadsListWorkItemsFilterKindsMixed covers the multi-kind case:
// bd has no "kind set" flag, so the adaptor falls through to
// client-side matchesFilter. Requesting {milestone, task} must return
// both kinds and exclude the bare epic.
func TestBeadsListWorkItemsFilterKindsMixed(t *testing.T) {
	fake := newFakeBd(t)
	fake.beads["gm-ms-1"] = &bd.Bead{
		ID: "gm-ms-1", Title: "M1", Status: "open", Priority: 1,
		IssueType: "epic", Labels: []string{"type:milestone"},
	}
	fake.beads["gm-tk-1"] = &bd.Bead{
		ID: "gm-tk-1", Title: "T1", Status: "open", Priority: 2,
		IssueType: "task",
	}
	fake.beads["gm-ep-1"] = &bd.Bead{
		ID: "gm-ep-1", Title: "E1", Status: "open", Priority: 2,
		IssueType: "epic",
	}
	impl := bd.NewWorkPlaneWithRunner(fake.run, "")

	got, err := impl.ListWorkItems(context.Background(), core.WorkItemFilter{
		Kinds: []string{core.KindMilestone, "task"},
	})
	if err != nil {
		t.Fatalf("ListWorkItems: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("mixed filter: got %d items, want 2 (milestone+task)", len(got))
	}
	seen := map[string]bool{}
	for _, wi := range got {
		seen[wi.Kind] = true
	}
	if !seen[core.KindMilestone] || !seen["task"] {
		t.Errorf("mixed filter: seen=%v; want both milestone and task", seen)
	}
	if seen["epic"] {
		t.Errorf("mixed filter returned plain epic — must be excluded")
	}
}

// TestBeadsListWorkItemsFilterKindsEmpty is the regression guard: an
// empty Kinds slice must NOT narrow the result set (zero-value fields
// on WorkItemFilter mean "no filter on that field" per the core
// contract), so every seeded bead is returned.
func TestBeadsListWorkItemsFilterKindsEmpty(t *testing.T) {
	fake := newFakeBd(t)
	fake.beads["gm-ms-1"] = &bd.Bead{
		ID: "gm-ms-1", Title: "M1", Status: "open", Priority: 1,
		IssueType: "epic", Labels: []string{"type:milestone"},
	}
	fake.beads["gm-tk-1"] = &bd.Bead{
		ID: "gm-tk-1", Title: "T1", Status: "open", Priority: 2,
		IssueType: "task",
	}
	impl := bd.NewWorkPlaneWithRunner(fake.run, "")

	got, err := impl.ListWorkItems(context.Background(), core.WorkItemFilter{})
	if err != nil {
		t.Fatalf("ListWorkItems: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("empty filter: got %d items, want 2 (Kinds nil must not narrow)", len(got))
	}
}

// gm-root.3.4: CreateWorkItem with Kind=KindMilestone translates to
// `bd create -t epic -l type:milestone …`. The read path (tested
// above) then round-trips the label back onto Kind=KindMilestone,
// so the end-to-end surface is stable.
func TestCreateWorkItemMilestoneEncodesAsEpicWithLabel(t *testing.T) {
	fake := newFakeBd(t)
	impl := bd.NewWorkPlaneWithRunner(fake.run, "")
	ctx := context.Background()

	created, err := impl.CreateWorkItem(ctx, core.WorkItem{
		Title:  "MVP ships",
		Kind:   core.KindMilestone,
		Status: "open",
		Labels: []string{"area:core"},
	})
	if err != nil {
		t.Fatalf("CreateWorkItem: %v", err)
	}

	// The adaptor must report the milestone Kind on the result of the
	// create so callers don't have to re-fetch.
	if created.Kind != core.KindMilestone {
		t.Errorf("created.Kind=%q want %q", created.Kind, core.KindMilestone)
	}

	// Inspect what the fake received: native type must be "epic", and
	// the milestone label must be present alongside caller labels.
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.beads) != 1 {
		t.Fatalf("fake.beads: want 1, got %d", len(fake.beads))
	}
	var stored *bd.Bead
	for _, b := range fake.beads {
		stored = b
	}
	if stored.IssueType != "epic" {
		t.Errorf("native IssueType=%q want \"epic\" (milestones encode as epic)", stored.IssueType)
	}
	wantLabels := map[string]bool{"area:core": false, "type:milestone": false}
	for _, l := range stored.Labels {
		if _, ok := wantLabels[l]; ok {
			wantLabels[l] = true
		}
	}
	for lbl, found := range wantLabels {
		if !found {
			t.Errorf("stored labels missing %q; got %v", lbl, stored.Labels)
		}
	}
}

// gm-root.3.4: re-creating a milestone whose caller-supplied labels
// already include type:milestone must not duplicate the marker.
func TestCreateWorkItemMilestoneIdempotentLabel(t *testing.T) {
	fake := newFakeBd(t)
	impl := bd.NewWorkPlaneWithRunner(fake.run, "")

	if _, err := impl.CreateWorkItem(context.Background(), core.WorkItem{
		Title:  "Already flagged",
		Kind:   core.KindMilestone,
		Labels: []string{"type:milestone", "area:core"},
	}); err != nil {
		t.Fatalf("CreateWorkItem: %v", err)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	var stored *bd.Bead
	for _, b := range fake.beads {
		stored = b
	}
	count := 0
	for _, l := range stored.Labels {
		if l == "type:milestone" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("type:milestone appeared %d times; must be 1 (idempotent)", count)
	}
}

// gm-root.3.4 round-trip: create a milestone via the write path, then
// read it back via ListWorkItems and confirm Kind survived.
func TestCreateMilestoneRoundTripsThroughList(t *testing.T) {
	fake := newFakeBd(t)
	impl := bd.NewWorkPlaneWithRunner(fake.run, "")
	ctx := context.Background()

	created, err := impl.CreateWorkItem(ctx, core.WorkItem{
		Title: "M2 ships",
		Kind:  core.KindMilestone,
	})
	if err != nil {
		t.Fatalf("CreateWorkItem: %v", err)
	}

	items, err := impl.ListWorkItems(ctx, core.WorkItemFilter{})
	if err != nil {
		t.Fatalf("ListWorkItems: %v", err)
	}
	var got *core.WorkItem
	for i := range items {
		if items[i].ID == created.ID {
			got = &items[i]
			break
		}
	}
	if got == nil {
		t.Fatalf("created milestone %q not found in list", created.ID)
	}
	if got.Kind != core.KindMilestone {
		t.Errorf("round-trip Kind=%q want %q (label projection broken)",
			got.Kind, core.KindMilestone)
	}
}
