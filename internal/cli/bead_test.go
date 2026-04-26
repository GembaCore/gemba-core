package cli

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MikeBengtson/gemba/internal/concepts"
	"github.com/MikeBengtson/gemba/internal/enrichment"
)

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
