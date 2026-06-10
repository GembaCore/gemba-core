package backend

import (
	"strings"
	"testing"
)

func TestQuoteApplescriptString(t *testing.T) {
	cases := map[string]string{
		`hello`:         `"hello"`,
		`say "hi"`:      `"say \"hi\""`,
		`path\to\file`:  `"path\\to\\file"`,
		`with "quotes"`: `"with \"quotes\""`,
	}
	for in, want := range cases {
		if got := quoteApplescriptString(in); got != want {
			t.Errorf("quoteApplescriptString(%q): want %q got %q", in, want, got)
		}
	}
}

func TestShellQuote(t *testing.T) {
	got := shellQuote(`tom's pane`)
	want := `'tom'\''s pane'`
	if got != want {
		t.Errorf("shellQuote: want %q got %q", want, got)
	}
}

func TestEnvExportLinesStable(t *testing.T) {
	// Map iteration order is nondeterministic; we just verify every
	// key appears exactly once with a correct quoted value.
	out := envExportLines(map[string]string{
		"GEMBA_SESSION_ID": "tmux:%12",
		"GEMBA_AGENT_TYPE": "claude",
	})
	if !strings.Contains(out, `export GEMBA_SESSION_ID='tmux:%12'; `) {
		t.Errorf("session id export missing or wrong: %q", out)
	}
	if !strings.Contains(out, `export GEMBA_AGENT_TYPE='claude'; `) {
		t.Errorf("agent type export missing or wrong: %q", out)
	}
}

func TestComposeInitialCommand(t *testing.T) {
	got := composeInitialCommand(SpawnSpec{
		Cwd:     "/tmp/worktree",
		Env:     map[string]string{"GEMBA_SESSION_ID": "s1"},
		Command: []string{"claude", "--model", "claude-opus-4-7"},
	})
	for _, sub := range []string{
		`cd '/tmp/worktree'`,
		`export GEMBA_SESSION_ID='s1'`,
		`'claude' '--model' 'claude-opus-4-7'`,
	} {
		if !strings.Contains(got, sub) {
			t.Errorf("composeInitialCommand missing %q in %q", sub, got)
		}
	}
	if !strings.Contains(got, " && ") {
		t.Errorf("composeInitialCommand must chain with &&: %q", got)
	}
}

func TestSplitTerminalID(t *testing.T) {
	wid, idx, err := splitTerminalID("12345:3")
	if err != nil {
		t.Fatal(err)
	}
	if wid != "12345" || idx != 3 {
		t.Errorf("splitTerminalID: got (%q, %d)", wid, idx)
	}
}

func TestSplitTerminalIDRejectsMalformed(t *testing.T) {
	cases := []string{"", "nope", "12345", "12345:abc", "12345:0"}
	for _, c := range cases {
		if _, _, err := splitTerminalID(c); err == nil {
			t.Errorf("splitTerminalID(%q): want error", c)
		}
	}
}

func TestTailLines(t *testing.T) {
	if got := tailLines("a\nb\nc\nd\ne", 3); got != "c\nd\ne" {
		t.Errorf("tailLines(..., 3) = %q", got)
	}
	if got := tailLines("a\nb", 5); got != "a\nb" {
		t.Errorf("tailLines n>len should be identity: %q", got)
	}
	if got := tailLines("a\nb", 0); got != "a\nb" {
		t.Errorf("tailLines n=0 should be identity: %q", got)
	}
}
