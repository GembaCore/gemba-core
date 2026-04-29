// Tests for `gemba agent profile` (gm-v5z2.2). Focus on flag
// validation + ranking helpers; the SQL boundary is exercised in
// internal/agentprofile/store_test.go.

package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/GembaCore/gemba-core/internal/planner"
)

func runAgentCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := newRootCmd(BuildInfo{})
	root.SetArgs(append([]string{"agent"}, args...))
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	err := root.Execute()
	return out.String(), err
}

func TestAgentProfile_RequiresArgOrAll(t *testing.T) {
	_, err := runAgentCmd(t, "profile", "--dolt-url", "mysql://r@127.0.0.1:3307/db")
	if err == nil {
		t.Fatal("expected error when neither agent-id nor --all is given")
	}
	if !strings.Contains(err.Error(), "agent-id") && !strings.Contains(err.Error(), "all") {
		t.Errorf("error should call out the missing arg/flag: %v", err)
	}
}

func TestAgentProfile_RejectsMissingDoltURL(t *testing.T) {
	_, err := runAgentCmd(t, "profile", "mike4")
	if err == nil {
		t.Fatal("expected error when --dolt-url is missing")
	}
	if !strings.Contains(err.Error(), "dolt-url") {
		t.Errorf("error should call out --dolt-url: %v", err)
	}
}

// ── topConceptsRanked / topFilesRanked ──────────────────────────

func TestTopConceptsRanked_SortsDescByWeightThenAlpha(t *testing.T) {
	in := map[planner.ConceptTag]float64{
		"alpha":   0.5,
		"bravo":   0.9,
		"charlie": 0.5,
		"delta":   0.1,
	}
	got := topConceptsRanked(in, 0)
	want := []planner.ConceptTag{"bravo", "alpha", "charlie", "delta"}
	if len(got) != len(want) {
		t.Fatalf("len: got %d want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].tag != w {
			t.Errorf("position %d: got %s want %s", i, got[i].tag, w)
		}
	}
}

func TestTopConceptsRanked_RespectsTopN(t *testing.T) {
	in := map[planner.ConceptTag]float64{
		"a": 1, "b": 2, "c": 3, "d": 4, "e": 5,
	}
	got := topConceptsRanked(in, 2)
	if len(got) != 2 {
		t.Errorf("topN=2: got %d entries", len(got))
	}
	// Top two are e (5) and d (4).
	if got[0].tag != "e" || got[1].tag != "d" {
		t.Errorf("ordering: %+v", got)
	}
}

func TestTopFilesRanked_SortsDescByWeightThenAlpha(t *testing.T) {
	in := map[string]float64{
		"src/auth.go":  0.5,
		"src/grid.go":  0.9,
		"src/users.go": 0.5,
	}
	got := topFilesRanked(in, 0)
	want := []string{"src/grid.go", "src/auth.go", "src/users.go"}
	for i, w := range want {
		if got[i].path != w {
			t.Errorf("position %d: got %s want %s", i, got[i].path, w)
		}
	}
}
