package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApply_KitAndIdempotent(t *testing.T) {
	dir := t.TempDir()
	opts := Options{
		Slug:               "001-foo",
		WriteSlashCommands: true,
		WriteHooks:         true,
		WriteConstitution:  true,
		UpdateClaudeMD:     true,
	}
	r, err := Apply(dir, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Created) < 4 {
		t.Fatalf("expected at least 4 created paths, got %v", r.Created)
	}
	// Verify slash command got slug-substituted.
	body, err := os.ReadFile(filepath.Join(dir, ".claude", "commands", "tasks.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "001-foo") {
		t.Fatalf("slug not substituted: %s", body)
	}
	if strings.Contains(string(body), "{{slug}}") {
		t.Fatalf("unsubstituted placeholder: %s", body)
	}
	// Re-run is a no-op.
	r2, err := Apply(dir, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(r2.Created) != 0 || len(r2.Updated) != 0 {
		t.Fatalf("expected idempotent, got created=%v updated=%v", r2.Created, r2.Updated)
	}
}

func TestApply_AppendClaudeMD(t *testing.T) {
	dir := t.TempDir()
	existing := "# Project\n\nSome existing content.\n"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(dir, Options{UpdateClaudeMD: true}); err != nil {
		t.Fatal(err)
	}
	out, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(out)
	if !strings.Contains(body, "Some existing content.") {
		t.Fatal("must preserve existing content")
	}
	if !strings.Contains(body, "ASDD") {
		t.Fatal("must append ASDD stanza")
	}
	// Idempotent second run.
	if _, err := Apply(dir, Options{UpdateClaudeMD: true}); err != nil {
		t.Fatal(err)
	}
	out2, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if string(out2) != body {
		t.Fatal("append should be idempotent")
	}
}
