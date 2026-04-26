package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MikeBengtson/gemba/internal/concepts"
)

// runCmd executes the root command with the supplied argv, returning
// stdout / stderr captures and the executor error. Tests don't go
// through cli.Execute so they can intercept i/o and avoid os.Exit.
func runCmd(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	root := newRootCmd(BuildInfo{Version: "test"})
	var out, errb bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errb)
	root.SetArgs(args)
	err := root.ExecuteContext(context.Background())
	return out.String(), errb.String(), err
}

func TestConceptsBootstrap_DryRunReportsCounts(t *testing.T) {
	root := t.TempDir()
	mustWriteCLI(t, filepath.Join(root, "internal/auth/auth.go"), "package auth\n")
	mustWriteCLI(t, filepath.Join(root, "web/src/App.tsx"), `<Route path="/board" />`)

	out, _, err := runCmd(t, "concepts", "bootstrap",
		"--workspace", root, "--dry-run", "--max", "10")
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if !strings.Contains(out, "[dry-run]") {
		t.Errorf("expected dry-run marker; got %q", out)
	}
	if !strings.Contains(out, "auth") || !strings.Contains(out, "board") {
		t.Errorf("expected bootstrapped terms in output; got %q", out)
	}
	// Vocabulary file must NOT exist after a dry-run.
	if _, err := os.Stat(filepath.Join(root, ".gemba/concepts/vocabulary.json")); !os.IsNotExist(err) {
		t.Errorf("dry-run must not write vocabulary.json (err=%v)", err)
	}
}

func TestConceptsBootstrap_PersistsByDefault(t *testing.T) {
	root := t.TempDir()
	mustWriteCLI(t, filepath.Join(root, "internal/auth/auth.go"), "package auth\n")
	if _, _, err := runCmd(t, "concepts", "bootstrap", "--workspace", root); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	v, err := concepts.LoadVocabulary(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := v.Find("auth"); !ok {
		t.Errorf("expected auth term persisted; got %+v", v.Terms)
	}
}

func TestConceptsList_EmptyWorkspaceHints(t *testing.T) {
	root := t.TempDir()
	out, _, err := runCmd(t, "concepts", "list", "--workspace", root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "no terms") {
		t.Errorf("empty list should hint at bootstrap; got %q", out)
	}
}

func TestConceptsList_PrintsAliasesAndRetired(t *testing.T) {
	root := t.TempDir()
	v := &concepts.Vocabulary{}
	v.Add(concepts.Term{Name: "react-query", Aliases: []string{"rq"}})
	v.Retire("react-query")
	if err := concepts.SaveVocabulary(root, v); err != nil {
		t.Fatal(err)
	}
	// Default skips retired.
	out, _, _ := runCmd(t, "concepts", "list", "--workspace", root)
	if strings.Contains(out, "react-query") {
		t.Errorf("active list should hide retired term; got %q", out)
	}
	// --all surfaces it.
	out, _, _ = runCmd(t, "concepts", "list", "--workspace", root, "--all")
	if !strings.Contains(out, "[RETIRED]") {
		t.Errorf("--all should surface RETIRED marker; got %q", out)
	}
	if !strings.Contains(out, "rq") {
		t.Errorf("--all should print aliases; got %q", out)
	}
}

func TestConceptsApprove_FlipsVocabAndStampsLog(t *testing.T) {
	root := t.TempDir()
	v := &concepts.Vocabulary{}
	v.Add(concepts.Term{Name: "rq"})
	v.Add(concepts.Term{Name: "react-query"})
	if err := concepts.SaveVocabulary(root, v); err != nil {
		t.Fatal(err)
	}
	list := &concepts.SuggestionList{}
	list.Add(concepts.Suggestion{
		ID: "s-1", Kind: concepts.KindMerge,
		From: "rq", To: "react-query",
		Status: concepts.StatusPending,
	})
	if err := concepts.SaveSuggestions(root, list); err != nil {
		t.Fatal(err)
	}

	out, _, err := runCmd(t, "concepts", "approve", "s-1",
		"--workspace", root, "--by", "operator@example.com")
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if !strings.Contains(out, "approved s-1") {
		t.Errorf("approve output missing confirmation: %q", out)
	}
	v, _ = concepts.LoadVocabulary(root)
	// Find resolves "rq" via the alias chain to react-query — that's
	// the right runtime semantic. Verify the retired-flag landed on
	// the original term by iterating directly.
	var rqTerm *concepts.Term
	for i := range v.Terms {
		if v.Terms[i].Name == "rq" {
			rqTerm = &v.Terms[i]
			break
		}
	}
	if rqTerm == nil || !rqTerm.Retired {
		t.Errorf("rq should be retired after approve; got %+v", v.Terms)
	}
	entries, _ := concepts.ReadDecisions(root)
	if len(entries) != 1 || entries[0].By != "operator@example.com" {
		t.Errorf("decisions log not stamped: %+v", entries)
	}
}

func TestConceptsReject_StampsLog(t *testing.T) {
	root := t.TempDir()
	list := &concepts.SuggestionList{}
	list.Add(concepts.Suggestion{
		ID: "s-2", Kind: concepts.KindDelete, From: "stale",
		Status: concepts.StatusPending,
	})
	if err := concepts.SaveSuggestions(root, list); err != nil {
		t.Fatal(err)
	}
	out, _, err := runCmd(t, "concepts", "reject", "s-2",
		"--workspace", root, "--reason", "still-needed")
	if err != nil {
		t.Fatalf("reject: %v", err)
	}
	if !strings.Contains(out, "rejected s-2") {
		t.Errorf("missing confirmation: %q", out)
	}
	entries, _ := concepts.ReadDecisions(root)
	if len(entries) != 1 || entries[0].Reason != "still-needed" {
		t.Errorf("reject log entry malformed: %+v", entries)
	}
}

func TestConceptsReview_StatusFilter(t *testing.T) {
	root := t.TempDir()
	list := &concepts.SuggestionList{
		Suggestions: []concepts.Suggestion{
			{ID: "p", Kind: concepts.KindMerge, From: "a", To: "b", Status: concepts.StatusPending, Reason: "p-reason"},
			{ID: "a", Kind: concepts.KindMerge, From: "c", To: "d", Status: concepts.StatusApproved, Reason: "a-reason"},
		},
	}
	if err := concepts.SaveSuggestions(root, list); err != nil {
		t.Fatal(err)
	}
	out, _, _ := runCmd(t, "concepts", "review", "--workspace", root, "--status", "pending")
	if !strings.Contains(out, "p\t") {
		t.Errorf("pending review missing entry: %q", out)
	}
	if strings.Contains(out, "a\t") {
		t.Errorf("pending filter leaked approved entry: %q", out)
	}
	out, _, _ = runCmd(t, "concepts", "review", "--workspace", root, "--status", "approved")
	if !strings.Contains(out, "a\t") {
		t.Errorf("approved filter missing entry: %q", out)
	}
}

func TestConceptsDrift_NoStoreNoOps(t *testing.T) {
	root := t.TempDir()
	out, _, err := runCmd(t, "concepts", "drift", "--workspace", root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "no BeadConceptStore wired") {
		t.Errorf("expected no-store hint; got %q", out)
	}
}

func mustWriteCLI(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
