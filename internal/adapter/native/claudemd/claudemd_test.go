package claudemd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApply_WritesSentinelBlockOnEmptyWorkspace(t *testing.T) {
	ws := t.TempDir()
	if err := Apply(ws, "hello world"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(ws, FileName))
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	if !strings.Contains(s, SentinelBegin) || !strings.Contains(s, SentinelEnd) {
		t.Fatalf("missing sentinels: %q", s)
	}
	if !strings.Contains(s, "hello world") {
		t.Errorf("body missing: %q", s)
	}
}

func TestApply_PreservesOperatorContentOutsideSentinels(t *testing.T) {
	ws := t.TempDir()
	original := "# operator notes\n\nhand-authored\n"
	if err := os.WriteFile(filepath.Join(ws, FileName), []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Apply(ws, "consult body"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(ws, FileName))
	if !strings.Contains(string(got), "operator notes") {
		t.Error("operator content wiped")
	}
	if !strings.Contains(string(got), "consult body") {
		t.Error("consult body missing")
	}
}

func TestApply_IsIdempotent(t *testing.T) {
	ws := t.TempDir()
	if err := Apply(ws, "body"); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(filepath.Join(ws, FileName))
	if err := Apply(ws, "body"); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(filepath.Join(ws, FileName))
	if string(first) != string(second) {
		t.Error("Apply twice with same body produced different files")
	}
}

func TestApply_ReplacesPriorBlock(t *testing.T) {
	ws := t.TempDir()
	if err := Apply(ws, "first body"); err != nil {
		t.Fatal(err)
	}
	if err := Apply(ws, "second body"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(ws, FileName))
	s := string(got)
	if strings.Contains(s, "first body") {
		t.Errorf("first body not replaced: %q", s)
	}
	if !strings.Contains(s, "second body") {
		t.Errorf("second body missing: %q", s)
	}
}

func TestRemove_StripsBlockPreservingOperatorContent(t *testing.T) {
	ws := t.TempDir()
	original := "# notes\n"
	if err := os.WriteFile(filepath.Join(ws, FileName), []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Apply(ws, "consult body"); err != nil {
		t.Fatal(err)
	}
	if err := Remove(ws); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(ws, FileName))
	if string(got) != original {
		t.Errorf("file not restored to pre-Apply state:\nwant %q\ngot  %q", original, string(got))
	}
}

func TestRemove_DeletesEmptyFileAfterStrip(t *testing.T) {
	ws := t.TempDir()
	if err := Apply(ws, "body"); err != nil {
		t.Fatal(err)
	}
	if err := Remove(ws); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(ws, FileName)); !os.IsNotExist(err) {
		t.Errorf("CLAUDE.md should be removed when empty after strip; stat err = %v", err)
	}
}

func TestRemove_NoOpOnMissingFile(t *testing.T) {
	if err := Remove(t.TempDir()); err != nil {
		t.Errorf("missing-file remove returned error: %v", err)
	}
}

func TestRemove_NoOpOnFileWithoutSentinels(t *testing.T) {
	ws := t.TempDir()
	body := "# operator notes\nno sentinels here\n"
	if err := os.WriteFile(filepath.Join(ws, FileName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Remove(ws); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(ws, FileName))
	if string(got) != body {
		t.Errorf("file mutated despite no sentinels: %q vs %q", got, body)
	}
}
