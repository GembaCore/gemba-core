package sourceanalysis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// stubExec returns an exec hook that fans calls into a per-subcommand
// dispatch table. Each handler receives the args (sans the binary)
// and returns the bytes the CLI would have printed.
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

func TestGitNexus_PassesContract_WithStubbedBackend(t *testing.T) {
	// A stubbed backend that returns empty rows for every cypher
	// call mirrors the "binary present, query returns no matches"
	// case — RunContract's universal assertions still hold.
	g := &GitNexus{
		RepoName: "gt",
		RepoPath: t.TempDir(),
		exec: stubExec(map[string]func([]string) ([]byte, error){
			"cypher": func(_ []string) ([]byte, error) {
				return []byte("[]"), nil
			},
		}),
	}
	RunContract(t, g, Target{Repository: "gt", Path: "main.go"})
}

func TestGitNexus_BinaryMissingReturnsErrUnavailable(t *testing.T) {
	g := &GitNexus{
		RepoName: "gt",
		exec: func(context.Context, string, ...string) ([]byte, error) {
			return nil, &exec.Error{Name: "gitnexus", Err: exec.ErrNotFound}
		},
	}
	ctx := context.Background()
	if _, err := g.Dependents(ctx, Target{Path: "x.go"}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Dependents err = %v, want ErrUnavailable", err)
	}
	if _, err := g.Dependencies(ctx, Target{Path: "x.go"}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Dependencies err = %v, want ErrUnavailable", err)
	}
	if _, err := g.PublicContractChanges(ctx, Diff{Files: []string{"x.go"}}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("PublicContractChanges err = %v, want ErrUnavailable", err)
	}
}

func TestGitNexus_DependentsParsesAndDedupes(t *testing.T) {
	g := &GitNexus{
		RepoName: "gt",
		exec: stubExec(map[string]func([]string) ([]byte, error){
			"cypher": func(_ []string) ([]byte, error) {
				return markdownEnvelope(
					[]string{"other.filePath"},
					[][]string{
						{"web/src/App.tsx"},
						{"web/src/components/Sidebar.tsx"},
						{"web/src/App.tsx"}, // dupe — collapsed
						{""},                // empty — dropped
					},
				), nil
			},
		}),
	}
	got, err := g.Dependents(context.Background(), Target{Repository: "gt", Path: "web/src/pages/BoardPage.tsx"})
	if err != nil {
		t.Fatalf("Dependents err = %v", err)
	}
	paths := make([]string, len(got))
	for i, t := range got {
		if t.Repository != "gt" {
			t2 := t
			t2.Repository = "gt"
			_ = t2
		}
		paths[i] = got[i].Path
	}
	sort.Strings(paths)
	want := []string{"web/src/App.tsx", "web/src/components/Sidebar.tsx"}
	if len(paths) != len(want) {
		t.Fatalf("Dependents paths = %v, want %v", paths, want)
	}
	for i := range paths {
		if paths[i] != want[i] {
			t.Errorf("Dependents paths[%d] = %q, want %q", i, paths[i], want[i])
		}
	}
}

func TestGitNexus_DependentsEmptyTargetReturnsNil(t *testing.T) {
	called := false
	g := &GitNexus{
		exec: stubExec(map[string]func([]string) ([]byte, error){
			"cypher": func(_ []string) ([]byte, error) {
				called = true
				return []byte("[]"), nil
			},
		}),
	}
	out, err := g.Dependents(context.Background(), Target{})
	if err != nil {
		t.Fatalf("Dependents err = %v", err)
	}
	if out != nil {
		t.Errorf("Dependents out = %v, want nil for empty target", out)
	}
	if called {
		t.Error("Dependents shelled out for an empty target — should short-circuit")
	}
}

func TestGitNexus_PublicContractChanges_OverReportsExportedSymbols(t *testing.T) {
	g := &GitNexus{
		RepoName: "gt",
		exec: stubExec(map[string]func([]string) ([]byte, error){
			"cypher": func(args []string) ([]byte, error) {
				// The query MUST list the diff files inline; verify
				// the binding stitched them in so a future regression
				// (forgetting to quote, only sending one file) trips
				// the test.
				q := args[len(args)-1]
				if !strings.Contains(q, "'web/src/lib/boardPresets.ts'") {
					return nil, fmt.Errorf("query missing file: %s", q)
				}
				return markdownEnvelope(
					[]string{"n.filePath", "n.name", "n.label"},
					[][]string{
						{"web/src/lib/boardPresets.ts", "BoardPreset", "Components"},
						{"web/src/lib/boardPresets.ts", "isKnownPreset", ""},
						{"web/src/lib/boardPresets.ts", "", "Layers"}, // empty name dropped
					},
				), nil
			},
		}),
	}
	syms, err := g.PublicContractChanges(context.Background(), Diff{
		Repository: "gt",
		Files:      []string{"web/src/lib/boardPresets.ts"},
	})
	if err != nil {
		t.Fatalf("PublicContractChanges err = %v", err)
	}
	if len(syms) != 2 {
		t.Fatalf("got %d symbols, want 2 (rows with empty name dropped)", len(syms))
	}
	if syms[0].Name != "BoardPreset" || syms[0].Kind != "Components" {
		t.Errorf("syms[0] = %+v, want BoardPreset/Components", syms[0])
	}
	if syms[0].Target.Repository != "gt" {
		t.Errorf("syms[0].Target.Repository = %q, want gt", syms[0].Target.Repository)
	}
}

func TestGitNexus_PublicContractChanges_EmptyDiffReturnsNil(t *testing.T) {
	g := &GitNexus{
		exec: func(context.Context, string, ...string) ([]byte, error) {
			t.Fatal("exec called for empty diff")
			return nil, nil
		},
	}
	out, err := g.PublicContractChanges(context.Background(), Diff{})
	if err != nil {
		t.Fatalf("PublicContractChanges err = %v", err)
	}
	if out != nil {
		t.Errorf("PublicContractChanges out = %v, want nil for empty diff", out)
	}
}

func TestGitNexus_DescribeReadsMetaJSON(t *testing.T) {
	dir := t.TempDir()
	metaDir := filepath.Join(dir, ".gitnexus")
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	indexedAt := time.Date(2026, 4, 22, 22, 56, 54, 0, time.UTC)
	body := fmt.Sprintf(`{"repoPath":"%s","lastCommit":"abc123","indexedAt":"%s"}`,
		dir, indexedAt.Format(time.RFC3339))
	if err := os.WriteFile(filepath.Join(metaDir, "meta.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	g := &GitNexus{RepoName: "gt", RepoPath: dir}
	caps, err := g.Describe(context.Background())
	if err != nil {
		t.Fatalf("Describe err = %v", err)
	}
	if !caps.Available {
		t.Errorf("Available = false, want true with valid meta.json")
	}
	if caps.Backend != "gitnexus" {
		t.Errorf("Backend = %q, want gitnexus", caps.Backend)
	}
	if !caps.IndexUpdatedAt.Equal(indexedAt) {
		t.Errorf("IndexUpdatedAt = %v, want %v", caps.IndexUpdatedAt, indexedAt)
	}
}

func TestGitNexus_DescribeMissingMetaIsAvailableFalse(t *testing.T) {
	g := &GitNexus{RepoName: "gt", RepoPath: t.TempDir()}
	caps, err := g.Describe(context.Background())
	if err != nil {
		t.Fatalf("Describe returned error: %v", err)
	}
	if caps.Available {
		t.Error("Available = true with no meta.json on disk")
	}
	if caps.Reason == "" {
		t.Error("Reason is empty; missing-index case must self-explain")
	}
	if caps.Backend != "gitnexus" {
		t.Errorf("Backend = %q, want gitnexus", caps.Backend)
	}
}

func TestGitNexus_DescribeNeverReturnsErrUnavailable(t *testing.T) {
	// Even on a totally unconfigured binding (no RepoPath) Describe
	// must self-explain via Capabilities.Reason rather than
	// signaling unavailability via the error sentinel.
	g := &GitNexus{}
	caps, err := g.Describe(context.Background())
	if errors.Is(err, ErrUnavailable) {
		t.Fatal("Describe returned ErrUnavailable; contract forbids that")
	}
	if err != nil {
		t.Fatalf("Describe err = %v", err)
	}
	if caps.Available {
		t.Error("Available = true with empty config")
	}
}

func TestParseMarkdownTable_RoundTrips(t *testing.T) {
	md := "| a | b |\n| --- | --- |\n| 1 | 2 |\n| 3 |  |\n"
	headers, rows := parseMarkdownTable(md)
	if len(headers) != 2 || headers[0] != "a" || headers[1] != "b" {
		t.Fatalf("headers = %v", headers)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %v", rows)
	}
	if rows[1][1] != "" {
		t.Errorf("expected empty cell preserved; got %q", rows[1][1])
	}
}
