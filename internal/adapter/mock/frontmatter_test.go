package mock

import (
	"reflect"
	"testing"
)

func TestParseFrontmatter_EmptyInput(t *testing.T) {
	fm := ParseFrontmatter("")
	if fm.Template != "" || fm.TestID != nil || fm.Files != nil {
		t.Fatalf("empty input must yield zero values; got %+v", fm)
	}
	if fm.Extras == nil || len(fm.Extras) != 0 {
		t.Fatalf("expected non-nil empty Extras; got %+v", fm.Extras)
	}
}

func TestParseFrontmatter_NoBlock(t *testing.T) {
	fm := ParseFrontmatter("# Goal\nNo frontmatter here.")
	if fm.Template != "" {
		t.Fatalf("expected no template; got %q", fm.Template)
	}
}

func TestParseFrontmatter_FullBlock(t *testing.T) {
	desc := "---\ntemplate: write-component\ntestid: temperature-table, row-{c}\nfiles: src/TemperatureTable.tsx\n---\n\n# Goal\nImplement.\n"
	fm := ParseFrontmatter(desc)
	if fm.Template != "write-component" {
		t.Fatalf("template: %q", fm.Template)
	}
	if !reflect.DeepEqual(fm.TestID, []string{"temperature-table", "row-{c}"}) {
		t.Fatalf("testid: %v", fm.TestID)
	}
	if !reflect.DeepEqual(fm.Files, []string{"src/TemperatureTable.tsx"}) {
		t.Fatalf("files: %v", fm.Files)
	}
}

func TestParseFrontmatter_ExtrasCaptured(t *testing.T) {
	desc := "---\ntemplate: noop\npriority: high\ncustom-thing: yes\n---\nbody\n"
	fm := ParseFrontmatter(desc)
	if fm.Template != "noop" {
		t.Fatalf("template: %q", fm.Template)
	}
	if fm.Extras["priority"] != "high" {
		t.Fatalf("extras priority: %q", fm.Extras["priority"])
	}
	if fm.Extras["custom-thing"] != "yes" {
		t.Fatalf("extras custom-thing: %q", fm.Extras["custom-thing"])
	}
}

func TestParseFrontmatter_TolerantOfMalformed(t *testing.T) {
	desc := "---\n\ntemplate: build\n\nbare-line-without-colon\nfiles: a.ts, b.ts\n---\nbody\n"
	fm := ParseFrontmatter(desc)
	if fm.Template != "build" {
		t.Fatalf("template: %q", fm.Template)
	}
	if !reflect.DeepEqual(fm.Files, []string{"a.ts", "b.ts"}) {
		t.Fatalf("files: %v", fm.Files)
	}
}

func TestParseFrontmatter_CaseInsensitiveKeys(t *testing.T) {
	desc := "---\nTEMPLATE: noop\nFiles: a.ts\n---\nbody\n"
	fm := ParseFrontmatter(desc)
	if fm.Template != "noop" {
		t.Fatalf("template: %q", fm.Template)
	}
	if !reflect.DeepEqual(fm.Files, []string{"a.ts"}) {
		t.Fatalf("files: %v", fm.Files)
	}
}

func TestStripFrontmatter_RemovesLeadingBlock(t *testing.T) {
	desc := "---\ntemplate: noop\n---\n\n# Goal\nThe body."
	got := StripFrontmatter(desc)
	want := "# Goal\nThe body."
	if got != want {
		t.Fatalf("strip mismatch: got %q want %q", got, want)
	}
}

func TestStripFrontmatter_NoBlockUnchanged(t *testing.T) {
	if StripFrontmatter("# Goal") != "# Goal" {
		t.Fatal("no-block input must pass through")
	}
}

func TestStripFrontmatter_EmptyInput(t *testing.T) {
	if StripFrontmatter("") != "" {
		t.Fatal("empty input must return empty")
	}
}
