package codeanalysis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	corecodeanalysis "github.com/MikeBengtson/gemba/internal/core/codeanalysis"
)

// stubExec returns an exec hook that fans calls into a per-
// subcommand dispatch table. Mirrors the helper in
// internal/sourceanalysis/gitnexus_test.go but kept private so the
// adapter packages stay independent.
func stubExec(handlers map[string]func(args []string) ([]byte, error)) func(context.Context, string, ...string) ([]byte, error) {
	return func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if len(args) == 0 {
			return nil, fmt.Errorf("stubExec: no subcommand")
		}
		h, ok := handlers[args[0]]
		if !ok {
			return nil, fmt.Errorf("stubExec: no handler for %q", args[0])
		}
		return h(args[1:])
	}
}

// markdownEnvelope wraps a header + rows in the JSON shape the
// gitnexus CLI emits for a successful cypher call.
func markdownEnvelope(headers []string, rows [][]string) []byte {
	var sb strings.Builder
	sb.WriteString("| ")
	sb.WriteString(strings.Join(headers, " | "))
	sb.WriteString(" |\n|")
	for range headers {
		sb.WriteString(" --- |")
	}
	sb.WriteString("\n")
	for _, r := range rows {
		sb.WriteString("| ")
		sb.WriteString(strings.Join(r, " | "))
		sb.WriteString(" |\n")
	}
	out, _ := json.Marshal(cypherEnvelope{Markdown: sb.String(), RowCount: len(rows)})
	return out
}

// listJSON renders the CLI's `list --json` output.
func listJSON(entries []listEntry) []byte {
	out, _ := json.Marshal(entries)
	return out
}

func TestAdapter_RegistryReplace(t *testing.T) {
	// Ensure the package init replaced the placeholder, not just
	// registered alongside it. Resolve must hand back a real
	// *Adapter, never the ErrNotImplemented sentinel.
	p, err := corecodeanalysis.Resolve(corecodeanalysis.BackendGitNexus)
	if err != nil {
		t.Fatalf("Resolve(gitnexus) err = %v, want nil", err)
	}
	if _, ok := p.(*Adapter); !ok {
		t.Fatalf("Resolve(gitnexus) = %T, want *Adapter", p)
	}
}

func TestAdapter_ManifestHonest(t *testing.T) {
	a := &Adapter{
		exec: stubExec(map[string]func([]string) ([]byte, error){
			"list": func(_ []string) ([]byte, error) {
				return listJSON([]listEntry{
					{Name: "gemba", Path: "/repos/gemba"},
					{Name: "gt", Path: "/repos/gt", Remote: "https://example.test/gt"},
				}), nil
			},
		}),
	}
	m, err := a.Manifest(context.Background())
	if err != nil {
		t.Fatalf("Manifest err = %v", err)
	}
	if m.Backend != corecodeanalysis.BackendGitNexus {
		t.Errorf("Backend = %q, want %q", m.Backend, corecodeanalysis.BackendGitNexus)
	}
	if m.Version == "" {
		t.Error("Version is empty; adapter MUST self-identify")
	}
	if !m.SupportsImpact || !m.SupportsSymbolContext {
		t.Errorf("Impact/SymbolContext flags should be true; got Impact=%v SymbolContext=%v",
			m.SupportsImpact, m.SupportsSymbolContext)
	}
	// Capability honesty: don't claim support for what the
	// adapter doesn't surface.
	if m.SupportsRouteMap {
		t.Error("SupportsRouteMap should be false (no caller wired)")
	}
	if m.SupportsEmbeddings {
		t.Error("SupportsEmbeddings should be false (CLI uses --embeddings opt-in)")
	}
	if len(m.IndexedRepos) != 2 {
		t.Fatalf("IndexedRepos len = %d, want 2", len(m.IndexedRepos))
	}
	if m.IndexedRepos[1].Remote != "https://example.test/gt" {
		t.Errorf("IndexedRepos[1].Remote = %q, want example.test/gt", m.IndexedRepos[1].Remote)
	}
}

func TestAdapter_ManifestBinaryMissing(t *testing.T) {
	a := &Adapter{
		exec: func(context.Context, string, ...string) ([]byte, error) {
			return nil, &exec.Error{Name: "gitnexus", Err: exec.ErrNotFound}
		},
	}
	m, err := a.Manifest(context.Background())
	if err != nil {
		t.Fatalf("Manifest err = %v, want nil (graceful degrade)", err)
	}
	if len(m.IndexedRepos) != 0 {
		t.Errorf("IndexedRepos len = %d, want 0 with missing binary", len(m.IndexedRepos))
	}
	if m.Backend != corecodeanalysis.BackendGitNexus {
		t.Errorf("Backend = %q, want self-identification even without CLI", m.Backend)
	}
}

func TestAdapter_ListReposEmpty(t *testing.T) {
	a := &Adapter{
		exec: stubExec(map[string]func([]string) ([]byte, error){
			"list": func(_ []string) ([]byte, error) { return []byte("[]"), nil },
		}),
	}
	out, err := a.ListRepos(context.Background())
	if err != nil {
		t.Fatalf("ListRepos err = %v", err)
	}
	if out != nil {
		t.Errorf("ListRepos out = %v, want nil for empty list", out)
	}
}

func TestAdapter_ListReposParses(t *testing.T) {
	a := &Adapter{
		exec: stubExec(map[string]func([]string) ([]byte, error){
			"list": func(_ []string) ([]byte, error) {
				return listJSON([]listEntry{
					{Name: "gt", Path: "/repos/gt"},
				}), nil
			},
		}),
	}
	out, err := a.ListRepos(context.Background())
	if err != nil {
		t.Fatalf("ListRepos err = %v", err)
	}
	if len(out) != 1 || out[0].Name != "gt" || out[0].Path != "/repos/gt" {
		t.Fatalf("ListRepos out = %+v, want [{gt /repos/gt}]", out)
	}
}

func TestAdapter_ReindexShellsOut(t *testing.T) {
	cases := []struct {
		name     string
		repo     corecodeanalysis.RepoRef
		flags    corecodeanalysis.ReindexFlags
		wantArgs []string
	}{
		{
			name:     "minimal",
			repo:     corecodeanalysis.RepoRef{Name: "gt"},
			wantArgs: []string{"analyze", "-r", "gt"},
		},
		{
			name:     "full+embeddings",
			repo:     corecodeanalysis.RepoRef{Name: "gt", Path: "/repos/gt"},
			flags:    corecodeanalysis.ReindexFlags{Full: true, WithEmbeddings: true},
			wantArgs: []string{"analyze", "-r", "gt", "--path", "/repos/gt", "--full", "--embeddings"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			a := &Adapter{
				exec: func(_ context.Context, _ string, args ...string) ([]byte, error) {
					got = append([]string(nil), args...)
					return []byte("ok"), nil
				},
			}
			if err := a.Reindex(context.Background(), tc.repo, tc.flags); err != nil {
				t.Fatalf("Reindex err = %v", err)
			}
			if strings.Join(got, " ") != strings.Join(tc.wantArgs, " ") {
				t.Errorf("args = %v, want %v", got, tc.wantArgs)
			}
		})
	}
}

func TestAdapter_ReindexDryRunNoCall(t *testing.T) {
	called := false
	a := &Adapter{
		exec: func(context.Context, string, ...string) ([]byte, error) {
			called = true
			return nil, nil
		},
	}
	err := a.Reindex(context.Background(),
		corecodeanalysis.RepoRef{Name: "gt"},
		corecodeanalysis.ReindexFlags{DryRun: true})
	if err != nil {
		t.Fatalf("Reindex(dryrun) err = %v", err)
	}
	if called {
		t.Error("DryRun shelled out; must short-circuit")
	}
}

func TestAdapter_ReindexEmptyRepoErrors(t *testing.T) {
	a := &Adapter{}
	err := a.Reindex(context.Background(), corecodeanalysis.RepoRef{}, corecodeanalysis.ReindexFlags{})
	if err == nil {
		t.Fatal("expected error for empty repo")
	}
}

func TestAdapter_ReindexExecError(t *testing.T) {
	a := &Adapter{
		exec: func(context.Context, string, ...string) ([]byte, error) {
			return nil, errors.New("boom")
		},
	}
	err := a.Reindex(context.Background(),
		corecodeanalysis.RepoRef{Name: "gt"}, corecodeanalysis.ReindexFlags{})
	if err == nil {
		t.Fatal("expected error from failing exec")
	}
}

func TestAdapter_ModulesParses(t *testing.T) {
	a := &Adapter{
		exec: stubExec(map[string]func([]string) ([]byte, error){
			"cypher": func(_ []string) ([]byte, error) {
				return markdownEnvelope(
					[]string{"m.name", "m.path", "m.summary", "m.loc", "m.fileCount"},
					[][]string{
						{"walk", "internal/walk", "Gemba walk core", "1234", "12"},
						{"persona", "internal/persona", "Persona runtime", "5678", "47"},
						{"", "skipped", "row dropped because no name", "0", "0"},
					},
				), nil
			},
		}),
	}
	mods, err := a.Modules(context.Background(),
		corecodeanalysis.RepoRef{Name: "gemba", Path: "/repos/gemba"})
	if err != nil {
		t.Fatalf("Modules err = %v", err)
	}
	if len(mods) != 2 {
		t.Fatalf("Modules len = %d, want 2 (empty-name row dropped)", len(mods))
	}
	if mods[0].Name != "walk" || mods[0].Path != "internal/walk" || mods[0].LOC != 1234 || mods[0].FileCount != 12 {
		t.Errorf("mods[0] = %+v", mods[0])
	}
	// Coverage unknown ⇒ -1 per the value-type contract.
	if mods[0].TestCoverage != -1 {
		t.Errorf("TestCoverage = %v, want -1 (unknown)", mods[0].TestCoverage)
	}
}

func TestAdapter_ModulesRequiresName(t *testing.T) {
	a := &Adapter{}
	_, err := a.Modules(context.Background(), corecodeanalysis.RepoRef{Path: "/repos/x"})
	if err == nil {
		t.Fatal("expected error when repo.Name empty")
	}
}

func TestAdapter_HealthCombinesQueries(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".gitnexus"), 0o755); err != nil {
		t.Fatal(err)
	}
	indexedAt := time.Now().Add(-1 * time.Hour).UTC().Truncate(time.Second)
	body := fmt.Sprintf(`{"indexedAt":"%s"}`, indexedAt.Format(time.RFC3339))
	if err := os.WriteFile(filepath.Join(dir, ".gitnexus", "meta.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	// Each cypher query is dispatched by content match — the
	// adapter sends three distinct queries (cycles, undocumented,
	// god-classes); reply with shape-appropriate rows.
	cypherCallCount := 0
	a := &Adapter{
		exec: stubExec(map[string]func([]string) ([]byte, error){
			"cypher": func(args []string) ([]byte, error) {
				cypherCallCount++
				query := args[len(args)-1]
				switch {
				case strings.Contains(query, "cycles"):
					return markdownEnvelope([]string{"cycles"}, [][]string{{"3"}}), nil
				case strings.Contains(query, "isExported = true"):
					return markdownEnvelope(
						[]string{"n.name", "n.filePath", "n.line"},
						[][]string{
							{"PublicAPI", "pkg/api.go", "42"},
						},
					), nil
				case strings.Contains(query, "fanIn"):
					return markdownEnvelope(
						[]string{"t.name", "t.filePath", "t.line"},
						[][]string{
							{"GodCore", "pkg/core.go", "10"},
						},
					), nil
				}
				return []byte("[]"), nil
			},
		}),
	}
	report, err := a.Health(context.Background(),
		corecodeanalysis.RepoRef{Name: "gemba", Path: dir})
	if err != nil {
		t.Fatalf("Health err = %v", err)
	}
	if cypherCallCount != 3 {
		t.Errorf("cypher calls = %d, want 3 (cycles + undoc + god-classes)", cypherCallCount)
	}
	if report.CycleCount != 3 {
		t.Errorf("CycleCount = %d, want 3", report.CycleCount)
	}
	if len(report.UndocumentedAPIs) != 1 || report.UndocumentedAPIs[0].Name != "PublicAPI" {
		t.Errorf("UndocumentedAPIs = %+v", report.UndocumentedAPIs)
	}
	if len(report.GodClasses) != 1 || report.GodClasses[0].Name != "GodCore" {
		t.Errorf("GodClasses = %+v", report.GodClasses)
	}
	if report.StaleAfter == 0 {
		t.Error("StaleAfter should be set so callers can age the report")
	}
	// FetchedAt should track meta.json indexedAt (not now()).
	if !report.FetchedAt.Equal(indexedAt) {
		t.Errorf("FetchedAt = %v, want meta indexedAt %v",
			report.FetchedAt.UTC(), indexedAt)
	}
}

func TestAdapter_HealthStaleNotes(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".gitnexus"), 0o755); err != nil {
		t.Fatal(err)
	}
	indexedAt := time.Now().Add(-48 * time.Hour).UTC()
	body := fmt.Sprintf(`{"indexedAt":"%s"}`, indexedAt.Format(time.RFC3339))
	if err := os.WriteFile(filepath.Join(dir, ".gitnexus", "meta.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	a := &Adapter{
		StaleAfter: 12 * time.Hour,
		exec: stubExec(map[string]func([]string) ([]byte, error){
			"cypher": func(_ []string) ([]byte, error) { return []byte("[]"), nil },
		}),
	}
	report, err := a.Health(context.Background(),
		corecodeanalysis.RepoRef{Name: "gemba", Path: dir})
	if err != nil {
		t.Fatalf("Health err = %v", err)
	}
	found := false
	for _, n := range report.Notes {
		if strings.Contains(n, "reindex recommended") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected stale-index Notes entry; got %v", report.Notes)
	}
	if !report.IsStale(time.Now()) {
		t.Error("IsStale should be true for 48h-old index with 12h window")
	}
}

func TestAdapter_HealthRequiresName(t *testing.T) {
	a := &Adapter{}
	_, err := a.Health(context.Background(), corecodeanalysis.RepoRef{Path: "/x"})
	if err == nil {
		t.Fatal("expected error when repo.Name empty")
	}
}

func TestAdapter_ImpactUpstreamDefault(t *testing.T) {
	var receivedQuery string
	a := &Adapter{
		exec: stubExec(map[string]func([]string) ([]byte, error){
			"cypher": func(args []string) ([]byte, error) {
				receivedQuery = args[len(args)-1]
				return markdownEnvelope(
					[]string{"other.name", "other.filePath", "other.line"},
					[][]string{
						{"Caller1", "pkg/a.go", "10"},
						{"Caller2", "pkg/b.go", "20"},
					},
				), nil
			},
		}),
	}
	report, err := a.Impact(context.Background(),
		corecodeanalysis.RepoRef{Name: "gemba"}, "TargetFunc", "")
	if err != nil {
		t.Fatalf("Impact err = %v", err)
	}
	// Empty direction defaults to upstream.
	if report.Direction != corecodeanalysis.ImpactUpstream {
		t.Errorf("Direction = %q, want upstream (default)", report.Direction)
	}
	// Upstream query: callers MATCH (other)-[]->(t).
	if !strings.Contains(receivedQuery, "(other)-[]->(t)") {
		t.Errorf("upstream query missing edge pattern: %s", receivedQuery)
	}
	if !strings.Contains(receivedQuery, "'TargetFunc'") {
		t.Errorf("query missing target literal: %s", receivedQuery)
	}
	if len(report.Affected) != 2 {
		t.Fatalf("Affected len = %d, want 2", len(report.Affected))
	}
	if report.Affected[0].Name != "Caller1" || report.Affected[0].File != "pkg/a.go" || report.Affected[0].Line != 10 {
		t.Errorf("Affected[0] = %+v", report.Affected[0])
	}
	if report.Risk == "" {
		t.Error("Risk unclassified — adapter MUST set a coarse bucket")
	}
}

func TestAdapter_ImpactDownstreamShape(t *testing.T) {
	var receivedQuery string
	a := &Adapter{
		exec: stubExec(map[string]func([]string) ([]byte, error){
			"cypher": func(args []string) ([]byte, error) {
				receivedQuery = args[len(args)-1]
				return []byte("[]"), nil
			},
		}),
	}
	_, err := a.Impact(context.Background(),
		corecodeanalysis.RepoRef{Name: "gemba"}, "TargetFunc", corecodeanalysis.ImpactDownstream)
	if err != nil {
		t.Fatalf("Impact err = %v", err)
	}
	if !strings.Contains(receivedQuery, "(t)-[]->(other)") {
		t.Errorf("downstream query missing edge pattern: %s", receivedQuery)
	}
}

func TestAdapter_ImpactBothShape(t *testing.T) {
	var receivedQuery string
	a := &Adapter{
		exec: stubExec(map[string]func([]string) ([]byte, error){
			"cypher": func(args []string) ([]byte, error) {
				receivedQuery = args[len(args)-1]
				return []byte("[]"), nil
			},
		}),
	}
	_, err := a.Impact(context.Background(),
		corecodeanalysis.RepoRef{Name: "gemba"}, "TargetFunc", corecodeanalysis.ImpactBoth)
	if err != nil {
		t.Fatalf("Impact err = %v", err)
	}
	if !strings.Contains(receivedQuery, "(t)-[]-(other)") {
		t.Errorf("both-direction query missing edge pattern: %s", receivedQuery)
	}
}

func TestAdapter_ImpactRequiresTargetAndRepo(t *testing.T) {
	a := &Adapter{}
	if _, err := a.Impact(context.Background(),
		corecodeanalysis.RepoRef{}, "X", corecodeanalysis.ImpactUpstream); err == nil {
		t.Error("expected error for empty repo")
	}
	if _, err := a.Impact(context.Background(),
		corecodeanalysis.RepoRef{Name: "gemba"}, "", corecodeanalysis.ImpactUpstream); err == nil {
		t.Error("expected error for empty target")
	}
}

func TestAdapter_ImpactCypherEscapesQuote(t *testing.T) {
	var receivedQuery string
	a := &Adapter{
		exec: stubExec(map[string]func([]string) ([]byte, error){
			"cypher": func(args []string) ([]byte, error) {
				receivedQuery = args[len(args)-1]
				return []byte("[]"), nil
			},
		}),
	}
	_, err := a.Impact(context.Background(),
		corecodeanalysis.RepoRef{Name: "gemba"}, "Foo's Func", corecodeanalysis.ImpactUpstream)
	if err != nil {
		t.Fatalf("Impact err = %v", err)
	}
	// Single quote MUST be escaped so it doesn't break the
	// quoted Cypher literal.
	if !strings.Contains(receivedQuery, `'Foo\'s Func'`) {
		t.Errorf("quote not escaped in query: %s", receivedQuery)
	}
}

func TestClassifyRisk(t *testing.T) {
	cases := []struct {
		n    int
		want corecodeanalysis.RiskLevel
	}{
		{0, corecodeanalysis.RiskLow},
		{3, corecodeanalysis.RiskLow},
		{15, corecodeanalysis.RiskMedium},
		{40, corecodeanalysis.RiskHigh},
		{200, corecodeanalysis.RiskCritical},
	}
	for _, tc := range cases {
		if got := classifyRisk(tc.n); got != tc.want {
			t.Errorf("classifyRisk(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}

func TestAtoiSafe(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"42", 42},
		{"  7 ", 7},
		{"-3", -3},
		{"", 0},
		{"abc", 0},
		{"3.14", 0},
	}
	for _, tc := range cases {
		if got := atoiSafe(tc.in); got != tc.want {
			t.Errorf("atoiSafe(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestParseMarkdownTable(t *testing.T) {
	md := "| a | b |\n| --- | --- |\n| 1 | 2 |\n| 3 |  |\n"
	headers, rows := parseMarkdownTable(md)
	if len(headers) != 2 || headers[0] != "a" {
		t.Fatalf("headers = %v", headers)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %v", rows)
	}
	if rows[1][1] != "" {
		t.Errorf("expected empty cell preserved; got %q", rows[1][1])
	}
}
