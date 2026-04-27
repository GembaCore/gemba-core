package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MikeBengtson/gemba/internal/concepts"
	"github.com/MikeBengtson/gemba/internal/enrichment"
)

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func TestBeadShow_NoEnrichmentHints(t *testing.T) {
	root := t.TempDir()
	out, _, err := runCmd(t, "bead", "show", "gm-1", "--workspace", root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "(no enrichment recorded)") {
		t.Errorf("expected hint for empty bead; got %q", out)
	}
}

func TestBeadList_EmptyHints(t *testing.T) {
	root := t.TempDir()
	out, _, err := runCmd(t, "bead", "list", "--workspace", root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "(no enriched beads yet)") {
		t.Errorf("expected list hint; got %q", out)
	}
}

func TestBeadTargetsAdd_PersistsAndLists(t *testing.T) {
	root := t.TempDir()
	out, _, err := runCmd(t, "bead", "targets", "add", "gm-1", "internal/auth/", "web/src/Topbar.tsx",
		"--workspace", root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "internal/auth") {
		t.Errorf("expected new target in output; got %q", out)
	}
	// Persisted file is readable through the store directly.
	store := enrichment.NewFileStore(root, nil)
	got, err := store.Load(context.Background(), "gm-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Targets) != 2 {
		t.Errorf("expected 2 targets, got %v", got.Targets)
	}
	// And the bead now appears in `bead list`.
	out, _, err = runCmd(t, "bead", "list", "--workspace", root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "gm-1") {
		t.Errorf("bead list missing gm-1: %q", out)
	}
}

func TestBeadTargetsRm_DropsEntry(t *testing.T) {
	root := t.TempDir()
	store := enrichment.NewFileStore(root, nil)
	_ = store.Save(context.Background(), enrichment.Enrichment{
		BeadID:  "gm-1",
		Targets: []string{"a.go", "b.go"},
	})
	if _, _, err := runCmd(t, "bead", "targets", "rm", "gm-1", "a.go", "--workspace", root); err != nil {
		t.Fatal(err)
	}
	got, _ := store.Load(context.Background(), "gm-1")
	if len(got.Targets) != 1 || got.Targets[0] != "b.go" {
		t.Errorf("rm left wrong state: %v", got.Targets)
	}
}

func TestBeadTargetsSet_ReplacesAll(t *testing.T) {
	root := t.TempDir()
	store := enrichment.NewFileStore(root, nil)
	_ = store.Save(context.Background(), enrichment.Enrichment{
		BeadID:  "gm-1",
		Targets: []string{"old.go"},
	})
	if _, _, err := runCmd(t, "bead", "targets", "set", "gm-1", "new1.go", "new2.go", "--workspace", root); err != nil {
		t.Fatal(err)
	}
	got, _ := store.Load(context.Background(), "gm-1")
	if len(got.Targets) != 2 || got.Targets[0] != "new1.go" {
		t.Errorf("set produced wrong list: %v", got.Targets)
	}
}

func TestBeadTargetsSet_EmptyClearsList(t *testing.T) {
	// `bead targets set <id>` with no globs is the explicit-clear gesture.
	root := t.TempDir()
	store := enrichment.NewFileStore(root, nil)
	_ = store.Save(context.Background(), enrichment.Enrichment{
		BeadID:  "gm-1",
		Targets: []string{"a.go"},
	})
	if _, _, err := runCmd(t, "bead", "targets", "set", "gm-1", "--workspace", root); err != nil {
		t.Fatal(err)
	}
	got, _ := store.Load(context.Background(), "gm-1")
	if len(got.Targets) != 0 {
		t.Errorf("set with no args should clear; got %v", got.Targets)
	}
}

func TestBeadConceptsAdd_NormalizesAndPersists(t *testing.T) {
	root := t.TempDir()
	if _, _, err := runCmd(t, "bead", "concepts", "add", "gm-1", "React Query", "AUTH",
		"--workspace", root); err != nil {
		t.Fatal(err)
	}
	store := enrichment.NewFileStore(root, nil)
	got, _ := store.Load(context.Background(), "gm-1")
	want := []string{"auth", "react-query"}
	if len(got.Concepts) != 2 || got.Concepts[0] != want[0] || got.Concepts[1] != want[1] {
		t.Errorf("concepts not normalized: %v, want %v", got.Concepts, want)
	}
}

func TestBeadConceptsAdd_WarnsOnUnknownTag(t *testing.T) {
	root := t.TempDir()
	// Bootstrap a vocabulary that lists "auth" only — "made-up" is unknown.
	v := &concepts.Vocabulary{}
	v.Add(concepts.Term{Name: "auth"})
	if err := concepts.SaveVocabulary(root, v); err != nil {
		t.Fatal(err)
	}
	out, errOut, err := runCmd(t, "bead", "concepts", "add", "gm-1", "made-up",
		"--workspace", root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errOut, "made-up") || !strings.Contains(errOut, "not in the vocabulary") {
		t.Errorf("expected unknown-concept warning on stderr; got stderr=%q stdout=%q", errOut, out)
	}
	// The edit STILL applies — vocabulary check is advisory.
	store := enrichment.NewFileStore(root, nil)
	got, _ := store.Load(context.Background(), "gm-1")
	if len(got.Concepts) != 1 || got.Concepts[0] != "made-up" {
		t.Errorf("concept should be saved despite warning: %v", got.Concepts)
	}
}

func TestBeadConceptsAdd_ForceSuppressesWarning(t *testing.T) {
	root := t.TempDir()
	v := &concepts.Vocabulary{}
	v.Add(concepts.Term{Name: "auth"})
	_ = concepts.SaveVocabulary(root, v)
	_, errOut, err := runCmd(t, "bead", "concepts", "add", "gm-1", "made-up",
		"--workspace", root, "--force")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(errOut, "not in the vocabulary") {
		t.Errorf("--force should suppress warning; got %q", errOut)
	}
}

func TestBeadConceptsAdd_NoVocabularyNoWarn(t *testing.T) {
	// A workspace that hasn't bootstrapped a vocabulary yet should
	// allow concept tagging silently — the warning system can't
	// usefully fire when there's nothing to compare against.
	root := t.TempDir()
	_, errOut, err := runCmd(t, "bead", "concepts", "add", "gm-1", "anything",
		"--workspace", root)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(errOut, "not in the vocabulary") {
		t.Errorf("no-vocab workspace should not warn; got %q", errOut)
	}
}

func TestBeadShow_RendersAfterEdit(t *testing.T) {
	root := t.TempDir()
	if _, _, err := runCmd(t, "bead", "targets", "add", "gm-1", "a.go", "--workspace", root); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runCmd(t, "bead", "concepts", "add", "gm-1", "auth", "--workspace", root); err != nil {
		t.Fatal(err)
	}
	out, _, err := runCmd(t, "bead", "show", "gm-1", "--workspace", root)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"a.go", "auth", "targets:", "concepts:", "operator"} {
		if !strings.Contains(out, want) {
			t.Errorf("show output missing %q; full output:\n%s", want, out)
		}
	}
}

func TestBeadExtract_HeuristicWritesEnrichment(t *testing.T) {
	root := t.TempDir()
	// Bootstrap a vocabulary so the concept side fires.
	v := &concepts.Vocabulary{}
	v.Add(concepts.Term{Name: "auth"})
	v.Add(concepts.Term{Name: "react-query"})
	if err := concepts.SaveVocabulary(root, v); err != nil {
		t.Fatal(err)
	}
	out, _, err := runCmd(t, "bead", "extract", "gm-1",
		"--workspace", root,
		"--title", "Migrate auth to react-query",
		"--body", "Touches `internal/auth/auth.go` and `web/src/App.tsx`.")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"internal/auth/auth.go", "web/src/App.tsx", "auth", "react-query"} {
		if !strings.Contains(out, want) {
			t.Errorf("extract output missing %q; got %q", want, out)
		}
	}
	store := enrichment.NewFileStore(root, nil)
	got, err := store.Load(context.Background(), "gm-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != enrichment.SourceLLM {
		t.Errorf("Source = %q, want SourceLLM (heuristic)", got.Source)
	}
}

func TestBeadExtract_DryRunDoesNotPersist(t *testing.T) {
	root := t.TempDir()
	out, _, err := runCmd(t, "bead", "extract", "gm-1",
		"--workspace", root, "--dry-run",
		"--body", "edits `internal/x/x.go`")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "(dry-run") {
		t.Errorf("dry-run marker missing: %q", out)
	}
	store := enrichment.NewFileStore(root, nil)
	if _, err := store.Load(context.Background(), "gm-1"); !errors.Is(err, enrichment.ErrNotFound) {
		t.Errorf("dry-run must not persist; Load got %v", err)
	}
}

func TestBeadExtract_MergeUnionsWithExistingOperatorEnrichment(t *testing.T) {
	root := t.TempDir()
	store := enrichment.NewFileStore(root, nil)
	// Operator already pinned a target the heuristic wouldn't find.
	_ = store.Save(context.Background(), enrichment.Enrichment{
		BeadID:   "gm-1",
		Targets:  []string{"docs/keep-me.md"},
		Concepts: []string{"keep-me"},
		Source:   enrichment.SourceOperator,
	})
	if _, _, err := runCmd(t, "bead", "extract", "gm-1",
		"--workspace", root, "--merge",
		"--body", "edits `internal/auth/auth.go`"); err != nil {
		t.Fatal(err)
	}
	got, _ := store.Load(context.Background(), "gm-1")
	if !contains(got.Targets, "docs/keep-me.md") {
		t.Errorf("merge dropped operator target: %v", got.Targets)
	}
	if !contains(got.Targets, "internal/auth/auth.go") {
		t.Errorf("merge missed extractor target: %v", got.Targets)
	}
	if got.Source != enrichment.SourceOperator {
		t.Errorf("merge should preserve operator Source; got %q", got.Source)
	}
}

func TestBeadExtract_NoMergeReplaces(t *testing.T) {
	root := t.TempDir()
	store := enrichment.NewFileStore(root, nil)
	_ = store.Save(context.Background(), enrichment.Enrichment{
		BeadID:  "gm-1",
		Targets: []string{"old.go"},
	})
	if _, _, err := runCmd(t, "bead", "extract", "gm-1",
		"--workspace", root,
		"--body", "edits `internal/new.go`"); err != nil {
		t.Fatal(err)
	}
	got, _ := store.Load(context.Background(), "gm-1")
	if contains(got.Targets, "old.go") {
		t.Errorf("default mode should replace; old target survived: %v", got.Targets)
	}
}

func TestBeadExtract_BodyFileWorks(t *testing.T) {
	root := t.TempDir()
	bodyPath := filepath.Join(root, "body.md")
	if err := os.WriteFile(bodyPath,
		[]byte("touches `internal/x/x.go`"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runCmd(t, "bead", "extract", "gm-1",
		"--workspace", root, "--body-file", bodyPath); err != nil {
		t.Fatal(err)
	}
	store := enrichment.NewFileStore(root, nil)
	got, _ := store.Load(context.Background(), "gm-1")
	if !contains(got.Targets, "internal/x/x.go") {
		t.Errorf("body-file path not extracted: %v", got.Targets)
	}
}

func TestBeadBackfill_DryRunDoesNotPersist(t *testing.T) {
	root := t.TempDir()
	bdBin := writeFakeBd(t, `[
		{"id":"gm-1","title":"auth flow","description":"touches `+"`internal/auth/auth.go`"+`"}
	]`)
	out, _, err := runCmd(t, "bead", "backfill",
		"--workspace", root,
		"--bd-bin", bdBin,
		"--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "[dry-run]") {
		t.Errorf("missing dry-run marker: %q", out)
	}
	if !strings.Contains(out, "considered: 1") || !strings.Contains(out, "extracted:  1") {
		t.Errorf("unexpected report: %q", out)
	}
	store := enrichment.NewFileStore(root, nil)
	if _, err := store.Load(context.Background(), "gm-1"); !errors.Is(err, enrichment.ErrNotFound) {
		t.Errorf("dry-run must not persist; got %v", err)
	}
}

func TestBeadBackfill_PersistsAndStampsSourceBackfill(t *testing.T) {
	root := t.TempDir()
	bdBin := writeFakeBd(t, `[
		{"id":"gm-1","title":"a","description":"edits `+"`internal/x/x.go`"+`"}
	]`)
	if _, _, err := runCmd(t, "bead", "backfill",
		"--workspace", root,
		"--bd-bin", bdBin); err != nil {
		t.Fatal(err)
	}
	store := enrichment.NewFileStore(root, nil)
	got, err := store.Load(context.Background(), "gm-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != enrichment.SourceBackfill {
		t.Errorf("Source = %q, want SourceBackfill", got.Source)
	}
	if !contains(got.Targets, "internal/x/x.go") {
		t.Errorf("expected target persisted: %v", got.Targets)
	}
}

func TestBeadBackfill_SkipExistingPreservesOperatorPin(t *testing.T) {
	root := t.TempDir()
	store := enrichment.NewFileStore(root, nil)
	_ = store.Save(context.Background(), enrichment.Enrichment{
		BeadID:  "gm-1",
		Targets: []string{"keep.go"},
		Source:  enrichment.SourceOperator,
	})
	bdBin := writeFakeBd(t, `[
		{"id":"gm-1","title":"a","description":"edits `+"`internal/new.go`"+`"},
		{"id":"gm-2","title":"b","description":"edits `+"`internal/x/x.go`"+`"}
	]`)
	out, _, err := runCmd(t, "bead", "backfill",
		"--workspace", root,
		"--bd-bin", bdBin)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "skipped:    1") {
		t.Errorf("expected skipped:1 for the operator-pinned bead: %q", out)
	}
	got1, _ := store.Load(context.Background(), "gm-1")
	if !contains(got1.Targets, "keep.go") || contains(got1.Targets, "internal/new.go") {
		t.Errorf("operator pin clobbered: %+v", got1)
	}
	got2, _ := store.Load(context.Background(), "gm-2")
	if !contains(got2.Targets, "internal/x/x.go") {
		t.Errorf("untracked bead should still be backfilled: %+v", got2)
	}
}

func TestBeadBackfill_FilterRegexNarrows(t *testing.T) {
	root := t.TempDir()
	bdBin := writeFakeBd(t, `[
		{"id":"gm-s47n-aaa","title":"a","description":"edits `+"`internal/aaa.go`"+`"},
		{"id":"gm-other-bbb","title":"b","description":"edits `+"`internal/bbb.go`"+`"}
	]`)
	out, _, err := runCmd(t, "bead", "backfill",
		"--workspace", root,
		"--bd-bin", bdBin,
		"--filter", "^gm-s47n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "extracted:  1") || !strings.Contains(out, "skipped:    1") {
		t.Errorf("filter report wrong: %q", out)
	}
	store := enrichment.NewFileStore(root, nil)
	if _, err := store.Load(context.Background(), "gm-other-bbb"); !errors.Is(err, enrichment.ErrNotFound) {
		t.Errorf("filtered bead should be untouched; got %v", err)
	}
}

// writeFakeBd drops a tiny shell script that prints the supplied
// JSON whenever invoked. Returns the path; the test passes it to
// `bead backfill --bd-bin` so the loop runs without a real bd
// binary or workspace.
func writeFakeBd(t *testing.T, jsonOut string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "bd")
	script := "#!/bin/sh\ncat <<'EOF'\n" + jsonOut + "\nEOF\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestBeadShow_AcceptsSlashedID(t *testing.T) {
	// Workspace-prefixed bd ids must round-trip through the file
	// path safe-id encoding.
	root := t.TempDir()
	id := "gemba/gemba/gm-1"
	if _, _, err := runCmd(t, "bead", "targets", "add", id, "a.go", "--workspace", root); err != nil {
		t.Fatal(err)
	}
	out, _, err := runCmd(t, "bead", "show", id, "--workspace", root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "gemba/gemba/gm-1") {
		t.Errorf("show should round-trip slashed id: %q", out)
	}
	// Confirm the storage path itself uses the safe-id encoding.
	matches, _ := filepath.Glob(filepath.Join(root, ".gemba/enrichment/gemba__gemba__gm-1.json"))
	if len(matches) != 1 {
		t.Errorf("expected safe-id encoded file on disk; matches=%v", matches)
	}
}

// ── dispatch_status (gm-v5z2.1) ─────────────────────────────────

func TestBeadStatusSet_PersistsAndShows(t *testing.T) {
	root := t.TempDir()
	out, _, err := runCmd(t, "bead", "status", "set", "gm-1", "awaiting-design", "--workspace", root)
	if err != nil {
		t.Fatalf("status set: %v", err)
	}
	if !strings.Contains(out, "dispatch_status: awaiting-design") {
		t.Errorf("expected dispatch_status in output; got %q", out)
	}

	// Show should reflect the persisted value.
	out, _, err = runCmd(t, "bead", "show", "gm-1", "--workspace", root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "dispatch_status: awaiting-design") {
		t.Errorf("show missed status: %q", out)
	}
}

func TestBeadStatusSet_RejectsUnknownValue(t *testing.T) {
	root := t.TempDir()
	_, _, err := runCmd(t, "bead", "status", "set", "gm-1", "blocked", "--workspace", root)
	if err == nil {
		t.Fatal("expected error on unknown status")
	}
	if !strings.Contains(err.Error(), "ready") {
		t.Errorf("error should list valid set: %v", err)
	}
}

func TestBeadStatusClear_RemovesValue(t *testing.T) {
	root := t.TempDir()
	store := enrichment.NewFileStore(root, nil)
	_ = store.Save(context.Background(), enrichment.Enrichment{
		BeadID:         "gm-1",
		DispatchStatus: enrichment.DispatchNotNow,
	})
	if _, _, err := runCmd(t, "bead", "status", "clear", "gm-1", "--workspace", root); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load(context.Background(), "gm-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.DispatchStatus != "" {
		t.Errorf("expected cleared; got %q", got.DispatchStatus)
	}
}

// ── estimated_size (gm-v5z2.1) ──────────────────────────────────

func TestBeadSizeSet_PersistsAndShows(t *testing.T) {
	root := t.TempDir()
	out, _, err := runCmd(t, "bead", "size", "set", "gm-1", "large", "--workspace", root)
	if err != nil {
		t.Fatalf("size set: %v", err)
	}
	if !strings.Contains(out, "estimated_size: large") {
		t.Errorf("expected size in output; got %q", out)
	}
}

func TestBeadSizeSet_RejectsUnknownBucket(t *testing.T) {
	root := t.TempDir()
	_, _, err := runCmd(t, "bead", "size", "set", "gm-1", "xl", "--workspace", root)
	if err == nil {
		t.Fatal("expected error on unknown bucket")
	}
}

func TestBeadSizeEstimate_DryRunDoesNotPersist(t *testing.T) {
	root := t.TempDir()
	body := strings.Repeat("Detailed body. ", 20) + "\n\n- [ ] one\n- [ ] two\n"
	out, _, err := runCmd(t, "bead", "size", "estimate", "gm-1",
		"--workspace", root, "--body", body, "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "(dry-run") {
		t.Errorf("expected dry-run notice; got %q", out)
	}
	store := enrichment.NewFileStore(root, nil)
	_, err = store.Load(context.Background(), "gm-1")
	if !errors.Is(err, enrichment.ErrNotFound) {
		t.Errorf("dry-run should not persist; load = %v", err)
	}
}

func TestBeadSizeEstimate_PersistsBucketFromBody(t *testing.T) {
	root := t.TempDir()
	body := "tiny one-line bead body"
	if _, _, err := runCmd(t, "bead", "size", "estimate", "gm-1",
		"--workspace", root, "--body", body); err != nil {
		t.Fatal(err)
	}
	store := enrichment.NewFileStore(root, nil)
	got, err := store.Load(context.Background(), "gm-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.EstimatedSize != enrichment.SizeSmall {
		t.Errorf("got %q, want small", got.EstimatedSize)
	}
}

func TestBeadSizeEstimate_ReadsBodyFromFile(t *testing.T) {
	root := t.TempDir()
	bodyPath := filepath.Join(root, "body.md")
	if err := os.WriteFile(bodyPath, []byte("brief description"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runCmd(t, "bead", "size", "estimate", "gm-1",
		"--workspace", root, "--body-file", bodyPath); err != nil {
		t.Fatal(err)
	}
	store := enrichment.NewFileStore(root, nil)
	got, _ := store.Load(context.Background(), "gm-1")
	if got.EstimatedSize == "" {
		t.Errorf("expected size to be set; got %+v", got)
	}
}

// concepts package import is already used elsewhere — keep unused-ref check happy.
var _ = concepts.Vocabulary{}
