package backend

import (
	"reflect"
	"testing"
)

// parsePanes is the only pure function in the tmux backend worth unit
// testing without shelling to real tmux. Integration-level tests
// against a real tmux server belong behind a -integration build tag.

func TestParsePanes(t *testing.T) {
	sample := "%5\t/home/mike/repo\tzsh\tmy title\t12345\n" +
		"%6\t/tmp\tclaude\t\t67890\n"
	got, err := parsePanes(sample)
	if err != nil {
		t.Fatalf("parsePanes: %v", err)
	}
	want := []Pane{
		{ID: "%5", Cwd: "/home/mike/repo", Command: "zsh", Title: "my title", Pid: 12345},
		{ID: "%6", Cwd: "/tmp", Command: "claude", Title: "", Pid: 67890},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parsePanes mismatch:\n got  %+v\n want %+v", got, want)
	}
}

func TestParsePanesIgnoresMalformedLines(t *testing.T) {
	sample := "incomplete-line\n%5\t/tmp\tbash\t\t0\n"
	got, _ := parsePanes(sample)
	if len(got) != 1 {
		t.Fatalf("want 1 pane (malformed line skipped), got %d", len(got))
	}
	if got[0].ID != "%5" {
		t.Errorf("unexpected pane: %+v", got[0])
	}
}

func TestParsePanesEmptyInput(t *testing.T) {
	got, err := parsePanes("")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("want nil slice, got %+v", got)
	}
}
