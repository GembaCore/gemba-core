// Tests for `gemba retro list` (gm-s47n.8.4).
//
// Focus on the offline (--file/stdin) path — the comparator + store
// already cover the data shape, and the dolt-URL path is just a SQL
// pool open + delegate to retro.Store.List.

package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/MikeBengtson/gemba/internal/planner"
	"github.com/MikeBengtson/gemba/internal/planner/retro"
	"github.com/MikeBengtson/gemba/internal/planner/targets"
)

func mustParseRFC(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return v
}

func sampleGrades(t *testing.T) []retro.Grade {
	t.Helper()
	t1 := mustParseRFC(t, "2026-04-26T10:00:00Z")
	t2 := mustParseRFC(t, "2026-04-26T11:00:00Z")
	t3 := mustParseRFC(t, "2026-04-26T12:00:00Z")
	low := retro.Compare(
		retro.Declared{Targets: []targets.Pattern{"src/auth.go"}},
		retro.Actual{Files: []string{"src/auth.go"}},
	)
	high := retro.Compare(
		retro.Declared{Targets: []targets.Pattern{"src/auth.go"}, Concepts: []planner.ConceptTag{"auth"}},
		retro.Actual{Files: []string{"src/handlers.go", "src/middleware.go"}, Concepts: []planner.ConceptTag{"ratelimit"}},
	)
	mid := retro.Compare(
		retro.Declared{Targets: []targets.Pattern{"src/auth.go", "src/foo.go"}},
		retro.Actual{Files: []string{"src/auth.go"}},
	)
	return []retro.Grade{
		{BeadID: "gm-low", ClosedAt: t1, SessionID: "sess-a", Diff: low},
		{BeadID: "gm-high", ClosedAt: t2, SessionID: "sess-b", Diff: high},
		{BeadID: "gm-mid", ClosedAt: t3, SessionID: "sess-a", Diff: mid},
	}
}

func runListCmd(t *testing.T, in []retro.Grade, args ...string) (string, error) {
	t.Helper()
	root := newRootCmd(BuildInfo{})
	root.SetArgs(append([]string{"retro", "list"}, args...))
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)

	body, err := json.Marshal(RetroListInput{Grades: in})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	root.SetIn(bytes.NewReader(body))

	err = root.Execute()
	return out.String(), err
}

func TestRetroList_DefaultsReadsStdinAndPrintsAll(t *testing.T) {
	got, err := runListCmd(t, sampleGrades(t))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	for _, want := range []string{"gm-low", "gm-high", "gm-mid"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in output:\n%s", want, got)
		}
	}
}

func TestRetroList_DivergenceThresholdFilters(t *testing.T) {
	got, err := runListCmd(t, sampleGrades(t), "--diverged-gt", "0.6")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	// gm-high has divergence 1.0 (disjoint sets); the others are
	// at 0 or 0.5. Only gm-high should survive a > 0.6 cut.
	if !strings.Contains(got, "gm-high") {
		t.Errorf("expected gm-high in output: %s", got)
	}
	if strings.Contains(got, "gm-low") || strings.Contains(got, "gm-mid") {
		t.Errorf("expected gm-low/gm-mid filtered out: %s", got)
	}
}

func TestRetroList_SessionFilter(t *testing.T) {
	got, err := runListCmd(t, sampleGrades(t), "--session", "sess-a")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(got, "sess-a") {
		t.Errorf("expected sess-a rows: %s", got)
	}
	if strings.Contains(got, "gm-high") {
		t.Errorf("gm-high (sess-b) should be filtered out: %s", got)
	}
}

func TestRetroList_JSONEnvelope(t *testing.T) {
	got, err := runListCmd(t, sampleGrades(t), "--json", "--diverged-gt", "0.6")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var env RetroListOut
	if err := json.Unmarshal([]byte(got), &env); err != nil {
		t.Fatalf("unmarshal: %v\noutput=%s", err, got)
	}
	if len(env.Rows) != 1 || env.Rows[0].BeadID != "gm-high" {
		t.Errorf("expected 1 row gm-high; got %+v", env.Rows)
	}
}

func TestRetroList_LimitClamps(t *testing.T) {
	got, err := runListCmd(t, sampleGrades(t), "--limit", "1", "--json")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var env RetroListOut
	if err := json.Unmarshal([]byte(got), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(env.Rows) != 1 {
		t.Errorf("expected 1 row; got %d", len(env.Rows))
	}
	// Newest first by closed_at — gm-mid is the most recent.
	if env.Rows[0].BeadID != "gm-mid" {
		t.Errorf("expected newest-first ordering (gm-mid); got %s", env.Rows[0].BeadID)
	}
}

func TestRetroList_EmptyResultPrintsHelpfulMessage(t *testing.T) {
	// gm-high has divergence 1.0, so >= 0.99 keeps it. Use 1.5 to
	// filter every grade out and exercise the helpful empty-state
	// rendering.
	got, err := runListCmd(t, sampleGrades(t), "--diverged-gt", "1.5")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(got, "no retrospective grades match") {
		t.Errorf("expected helpful empty-result message: %s", got)
	}
}

func TestRetroList_RejectsBothFileAndDoltURL(t *testing.T) {
	root := newRootCmd(BuildInfo{})
	root.SetArgs([]string{"retro", "list", "--file", "x.json", "--dolt-url", "mysql://r@h/d"})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	if err := root.Execute(); err == nil {
		t.Fatal("expected mutually-exclusive error")
	}
}

func TestRetroList_NewestFirstOrdering(t *testing.T) {
	got, err := runListCmd(t, sampleGrades(t), "--json")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var env RetroListOut
	if err := json.Unmarshal([]byte(got), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(env.Rows) != 3 {
		t.Fatalf("expected 3 rows; got %d", len(env.Rows))
	}
	// Newest first: gm-mid (12:00), gm-high (11:00), gm-low (10:00).
	wantOrder := []string{"gm-mid", "gm-high", "gm-low"}
	for i, want := range wantOrder {
		if string(env.Rows[i].BeadID) != want {
			t.Errorf("position %d: want %s got %s", i, want, env.Rows[i].BeadID)
		}
	}
}

// ── doltDSN ──────────────────────────────────────────────────────

func TestDoltDSN_BasicURL(t *testing.T) {
	got, err := doltDSN("mysql://root@127.0.0.1:3307/gemba")
	if err != nil {
		t.Fatalf("doltDSN: %v", err)
	}
	want := "root@tcp(127.0.0.1:3307)/gemba"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestDoltDSN_DefaultsUserToRoot(t *testing.T) {
	got, err := doltDSN("mysql://localhost:3307/db")
	if err != nil {
		t.Fatalf("doltDSN: %v", err)
	}
	if !strings.HasPrefix(got, "root@") {
		t.Errorf("expected root@ prefix; got %q", got)
	}
}

func TestDoltDSN_RejectsMissingDB(t *testing.T) {
	if _, err := doltDSN("mysql://root@127.0.0.1:3307"); err == nil {
		t.Fatal("expected error when /dbname missing")
	}
}
