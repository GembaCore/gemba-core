package workflow

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// stubRunner pattern-matches argv to canned outputs. Tests register
// (cmdline-prefix → output) pairs; the runner picks the longest
// matching prefix so more-specific stubs win over generic ones.
type stubRunner struct {
	t     *testing.T
	stubs []stub
	calls [][]string
}

type stub struct {
	prefix []string
	out    []byte
	err    error
}

func newStubRunner(t *testing.T) *stubRunner {
	return &stubRunner{t: t}
}

func (s *stubRunner) on(prefix string, out []byte, err error) *stubRunner {
	s.stubs = append(s.stubs, stub{prefix: strings.Fields(prefix), out: out, err: err})
	return s
}

func (s *stubRunner) Run() Runner {
	return func(_ context.Context, args ...string) ([]byte, error) {
		s.calls = append(s.calls, append([]string(nil), args...))
		var (
			best     *stub
			bestSize = -1
		)
		for i := range s.stubs {
			st := &s.stubs[i]
			if matchesPrefix(args, st.prefix) && len(st.prefix) > bestSize {
				best = st
				bestSize = len(st.prefix)
			}
		}
		if best == nil {
			return nil, errors.New("stubRunner: no stub for " + strings.Join(args, " "))
		}
		return best.out, best.err
	}
}

func matchesPrefix(args, prefix []string) bool {
	if len(prefix) > len(args) {
		return false
	}
	for i := range prefix {
		if args[i] != prefix[i] {
			return false
		}
	}
	return true
}

func TestList_HappyPath(t *testing.T) {
	r := newStubRunner(t).on("formula list --json", []byte(`[
		{"name":"shiny","type":"workflow","version":1,"description":"desc","source":"/p/shiny.toml"},
		{"name":"convoy","type":"convoy","description":"c","source":"/p/c.toml"}
	]`), nil)
	c := NewClientWithRunner(r.Run())
	got, err := c.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 formulas, got %d", len(got))
	}
	if got[0].Name != "shiny" || got[0].Type != "workflow" {
		t.Fatalf("first formula: %+v", got[0])
	}
	if got[0].SourcePath != "/p/shiny.toml" {
		t.Fatalf("source_path: %+v", got[0])
	}
}

func TestList_BdError(t *testing.T) {
	r := newStubRunner(t).on("formula list --json", nil, errors.New("dolt down"))
	c := NewClientWithRunner(r.Run())
	if _, err := c.List(context.Background()); err == nil {
		t.Fatalf("want error, got nil")
	}
}

func TestShow_HappyPath_WithCookOverride(t *testing.T) {
	showJSON := []byte(`{
		"formula":"shiny","type":"workflow","version":1,
		"description":"d","source":"/p/s.toml",
		"variables":[{"name":"v","default":"x"}],
		"steps":[{"id":"a","title":"A"}]
	}`)
	cookJSON := []byte(`{
		"formula":"shiny","steps":[
			{"id":"a","title":"A"},
			{"id":"b","title":"B","needs":["a"]}
		]
	}`)
	r := newStubRunner(t).
		on("formula show shiny --json", showJSON, nil).
		on("cook shiny --json", cookJSON, nil)
	c := NewClientWithRunner(r.Run())
	f, err := c.Show(context.Background(), "shiny")
	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if len(f.Steps) != 2 {
		t.Fatalf("expected cook to override steps to 2, got %d", len(f.Steps))
	}
	if f.Variables[0].Name != "v" {
		t.Fatalf("variables: %+v", f.Variables)
	}
}

func TestShow_NotFound(t *testing.T) {
	r := newStubRunner(t).on("formula show nope --json", nil, errors.New("formula not found: nope"))
	c := NewClientWithRunner(r.Run())
	_, err := c.Show(context.Background(), "nope")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestShow_EmptyName(t *testing.T) {
	c := NewClientWithRunner(newStubRunner(t).Run())
	if _, err := c.Show(context.Background(), ""); err == nil {
		t.Fatalf("want error on empty name")
	}
}

func TestListRuns_Active(t *testing.T) {
	r := newStubRunner(t).
		on("list --type molecule --json --status open", []byte(`[
			{"id":"gm-mol1","title":"Persistent","status":"open","created_at":"2026-04-01T00:00:00Z"}
		]`), nil).
		on("mol wisp list --json", []byte(`{"wisps":[
			{"id":"gm-wisp-1","title":"W1","status":"open","created_at":"2026-04-02T00:00:00Z"},
			{"id":"gm-wisp-2","title":"W2","status":"closed","created_at":"2026-04-03T00:00:00Z"}
		]}`), nil)
	c := NewClientWithRunner(r.Run())
	runs, err := c.ListRuns(context.Background(), "active")
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	// active filter: persistent mol + open wisp; closed wisp excluded
	if len(runs) != 2 {
		t.Fatalf("want 2 active runs, got %d (%+v)", len(runs), runs)
	}
	if runs[0].ID != "gm-mol1" || runs[0].Ephemeral {
		t.Fatalf("first run: %+v", runs[0])
	}
	if runs[1].ID != "gm-wisp-1" || !runs[1].Ephemeral {
		t.Fatalf("second run: %+v", runs[1])
	}
}

func TestListRuns_BadFilter(t *testing.T) {
	c := NewClientWithRunner(newStubRunner(t).Run())
	_, err := c.ListRuns(context.Background(), "bogus")
	if err == nil {
		t.Fatalf("want error on bogus filter")
	}
}

func TestGetRun_HappyPath(t *testing.T) {
	r := newStubRunner(t).on("mol show gm-wisp-x --json", []byte(`{
		"root":{"id":"gm-wisp-x","title":"mol-feat","status":"open","ephemeral":true,"created_at":"2026-04-05T00:00:00Z"},
		"issues":[
			{"id":"gm-wisp-x","title":"mol-feat","status":"open","ephemeral":true,"created_at":"2026-04-05T00:00:00Z"},
			{"id":"gm-wisp-x-step1","title":"S1","status":"closed","assignee":"agent-a"},
			{"id":"gm-wisp-x-step2","title":"S2","status":"open"}
		]
	}`), nil)
	c := NewClientWithRunner(r.Run())
	run, err := c.GetRun(context.Background(), "gm-wisp-x")
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.Progress.Total != 2 || run.Progress.Done != 1 {
		t.Fatalf("progress: %+v", run.Progress)
	}
	if !run.Ephemeral {
		t.Fatalf("expected ephemeral=true; got %+v", run)
	}
	if len(run.Steps) != 2 {
		t.Fatalf("steps: %+v", run.Steps)
	}
	if run.Steps[0].State != "done" {
		t.Fatalf("step[0].State: %+v", run.Steps[0])
	}
}

func TestGetRun_NotFound(t *testing.T) {
	r := newStubRunner(t).on("mol show ghost --json", []byte(`{"error":"molecule 'ghost' not found"}`), nil)
	c := NewClientWithRunner(r.Run())
	_, err := c.GetRun(context.Background(), "ghost")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestPour_RoundTrip(t *testing.T) {
	r := newStubRunner(t).
		on("mol pour shiny --json --var k=v", []byte(`{"id":"gm-mol-new","root":{"id":"gm-mol-new"}}`), nil).
		on("mol show gm-mol-new --json", []byte(`{
			"root":{"id":"gm-mol-new","title":"mol-shiny","status":"open","created_at":"2026-04-10T00:00:00Z"},
			"issues":[{"id":"gm-mol-new","title":"mol-shiny","status":"open"}]
		}`), nil)
	c := NewClientWithRunner(r.Run())
	run, err := c.Pour(context.Background(), "shiny", map[string]string{"k": "v"}, "")
	if err != nil {
		t.Fatalf("Pour: %v", err)
	}
	if run.ID != "gm-mol-new" || run.Ephemeral {
		t.Fatalf("run: %+v", run)
	}
}

func TestWisp_RoundTrip(t *testing.T) {
	r := newStubRunner(t).
		on("mol wisp shiny --json", []byte(`{"id":"gm-wisp-new"}`), nil).
		on("mol show gm-wisp-new --json", []byte(`{
			"root":{"id":"gm-wisp-new","title":"mol-shiny","status":"open","ephemeral":true,"created_at":"2026-04-10T00:00:00Z"},
			"issues":[{"id":"gm-wisp-new","title":"mol-shiny","status":"open","ephemeral":true}]
		}`), nil)
	c := NewClientWithRunner(r.Run())
	run, err := c.Wisp(context.Background(), "shiny", nil, "")
	if err != nil {
		t.Fatalf("Wisp: %v", err)
	}
	if !run.Ephemeral {
		t.Fatalf("expected ephemeral wisp; got %+v", run)
	}
}

func TestBurn_HappyPath(t *testing.T) {
	r := newStubRunner(t).
		on("mol show gm-wisp-x --json", []byte(`{
			"root":{"id":"gm-wisp-x","title":"x","status":"open","ephemeral":true},
			"issues":[{"id":"gm-wisp-x","title":"x","status":"open","ephemeral":true}]
		}`), nil).
		on("mol burn gm-wisp-x --force", []byte(``), nil)
	c := NewClientWithRunner(r.Run())
	if err := c.Burn(context.Background(), "gm-wisp-x"); err != nil {
		t.Fatalf("Burn: %v", err)
	}
}

func TestBurn_RefusesPersistent(t *testing.T) {
	r := newStubRunner(t).
		on("mol show gm-mol-x --json", []byte(`{
			"root":{"id":"gm-mol-x","title":"x","status":"open"},
			"issues":[{"id":"gm-mol-x","title":"x","status":"open"}]
		}`), nil)
	c := NewClientWithRunner(r.Run())
	err := c.Burn(context.Background(), "gm-mol-x")
	if !errors.Is(err, ErrNotWisp) {
		t.Fatalf("want ErrNotWisp, got %v", err)
	}
}

func TestBurn_NotFound(t *testing.T) {
	r := newStubRunner(t).
		on("mol show ghost --json", []byte(`{"error":"molecule 'ghost' not found"}`), nil)
	c := NewClientWithRunner(r.Run())
	err := c.Burn(context.Background(), "ghost")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}
