package backend

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/GembaCore/gemba-core/core"
)

// stubTmux returns a Tmux with an injected runner. `calls` captures
// every (stdin, argv) invocation so tests can assert wire shape.
func stubTmux(t *testing.T) (*Tmux, *[]stubCall) {
	t.Helper()
	var calls []stubCall
	tx := &Tmux{
		binary:      "/fake/tmux",
		sessionName: "gemba",
		runner: func(_ context.Context, stdin *strings.Reader, args ...string) ([]byte, error) {
			var s string
			if stdin != nil {
				b, _ := io.ReadAll(stdin)
				s = string(b)
			}
			calls = append(calls, stubCall{stdin: s, argv: append([]string(nil), args...)})
			return nil, nil
		},
	}
	return tx, &calls
}

type stubCall struct {
	stdin string
	argv  []string
}

// last returns the final argv recorded; useful for assertions on the
// tail of a (load-buffer, paste-buffer) sequence.
func last(calls []stubCall) []string {
	if len(calls) == 0 {
		return nil
	}
	return calls[len(calls)-1].argv
}

func argvContains(argv []string, sub ...string) bool {
	if len(sub) == 0 || len(argv) < len(sub) {
		return false
	}
	for i := 0; i+len(sub) <= len(argv); i++ {
		ok := true
		for j, s := range sub {
			if argv[i+j] != s {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

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
		{Kind: KindTmux, ID: "%5", Cwd: "/home/mike/repo", Command: "zsh", Title: "my title", Pid: 12345},
		{Kind: KindTmux, ID: "%6", Cwd: "/tmp", Command: "claude", Title: "", Pid: 67890},
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

func TestSendInput_LiteralShort(t *testing.T) {
	tx, calls := stubTmux(t)
	if err := tx.SendInput(context.Background(), "%0", core.SessionInput{Mode: core.InputLiteral, Keys: "hello"}); err != nil {
		t.Fatalf("SendInput: %v", err)
	}
	if !argvContains(last(*calls), "send-keys", "-t", "%0", "-l", "--", "hello") {
		t.Fatalf("argv mismatch: %v", last(*calls))
	}
}

func TestSendInput_LiteralWithNewlineUsesPaste(t *testing.T) {
	tx, calls := stubTmux(t)
	payload := "line1\nline2"
	if err := tx.SendInput(context.Background(), "%0", core.SessionInput{Mode: core.InputLiteral, Keys: payload}); err != nil {
		t.Fatalf("SendInput: %v", err)
	}
	sawLoad := false
	sawPaste := false
	for _, c := range *calls {
		if len(c.argv) >= 1 && c.argv[0] == "load-buffer" {
			sawLoad = true
			if c.stdin != payload {
				t.Errorf("load-buffer stdin mismatch: got %q want %q", c.stdin, payload)
			}
		}
		if len(c.argv) >= 1 && c.argv[0] == "paste-buffer" {
			sawPaste = true
		}
	}
	if !sawLoad || !sawPaste {
		t.Fatalf("expected load-buffer + paste-buffer; got calls=%+v", *calls)
	}
}

func TestSendInput_LiteralUnicodeVerbatim(t *testing.T) {
	tx, calls := stubTmux(t)
	payload := "héllo🚀"
	if err := tx.SendInput(context.Background(), "%0", core.SessionInput{Mode: core.InputLiteral, Keys: payload}); err != nil {
		t.Fatalf("SendInput: %v", err)
	}
	argv := last(*calls)
	if argv[len(argv)-1] != payload {
		t.Fatalf("unicode payload not verbatim: %v", argv)
	}
	if !argvContains(argv, "send-keys", "-t", "%0", "-l", "--", payload) {
		t.Fatalf("argv mismatch: %v", argv)
	}
}

func TestSendInput_KeysEnter(t *testing.T) {
	tx, calls := stubTmux(t)
	if err := tx.SendInput(context.Background(), "%0", core.SessionInput{Mode: core.InputKeys, Keys: "Enter"}); err != nil {
		t.Fatalf("SendInput: %v", err)
	}
	argv := last(*calls)
	if !argvContains(argv, "send-keys", "-t", "%0", "--", "Enter") {
		t.Fatalf("argv mismatch: %v", argv)
	}
	for _, a := range argv {
		if a == "-l" {
			t.Fatalf("InputKeys must NOT pass -l: %v", argv)
		}
	}
}

func TestSendInput_KeysControlC(t *testing.T) {
	tx, calls := stubTmux(t)
	if err := tx.SendInput(context.Background(), "%0", core.SessionInput{Mode: core.InputKeys, Keys: "C-c"}); err != nil {
		t.Fatalf("SendInput: %v", err)
	}
	if !argvContains(last(*calls), "send-keys", "-t", "%0", "--", "C-c") {
		t.Fatalf("argv mismatch: %v", last(*calls))
	}
}

func TestSendInput_SignalSIGINT(t *testing.T) {
	tx, calls := stubTmux(t)
	if err := tx.SendInput(context.Background(), "%0", core.SessionInput{Mode: core.InputSignal, Keys: "SIGINT"}); err != nil {
		t.Fatalf("SendInput: %v", err)
	}
	argv := last(*calls)
	if argv[len(argv)-1] != "C-c" || argv[len(argv)-2] != "--" {
		t.Fatalf("SIGINT must end in `-- C-c`: %v", argv)
	}
}

func TestSendInput_SignalSIGTERM(t *testing.T) {
	tx, calls := stubTmux(t)
	if err := tx.SendInput(context.Background(), "%0", core.SessionInput{Mode: core.InputSignal, Keys: "SIGTERM"}); err != nil {
		t.Fatalf("SendInput: %v", err)
	}
	argv := last(*calls)
	if argv[len(argv)-1] != `C-\` || argv[len(argv)-2] != "--" {
		t.Fatalf("SIGTERM must end in `-- C-\\`: %v", argv)
	}
}

func TestSendInput_SignalBogusValidation(t *testing.T) {
	tx, calls := stubTmux(t)
	err := tx.SendInput(context.Background(), "%0", core.SessionInput{Mode: core.InputSignal, Keys: "BOGUS"})
	if err == nil {
		t.Fatal("expected error for BOGUS signal")
	}
	var ae *core.AdaptorError
	if !errors.As(err, &ae) || ae.Kind != core.KindValidation {
		t.Fatalf("want KindValidation, got %v (%T)", err, err)
	}
	if len(*calls) != 0 {
		t.Fatalf("runner must NOT be invoked on validation failure; got %+v", *calls)
	}
}

func TestSendInput_EmptyPaneIDValidation(t *testing.T) {
	tx, calls := stubTmux(t)
	err := tx.SendInput(context.Background(), "", core.SessionInput{Mode: core.InputLiteral, Keys: "x"})
	if err == nil {
		t.Fatal("expected error for empty pane id")
	}
	if len(*calls) != 0 {
		t.Fatalf("runner must NOT be invoked when paneID empty; got %+v", *calls)
	}
}

func TestSendInput_EmptyKeysValidation(t *testing.T) {
	for _, mode := range []core.SessionInputMode{core.InputLiteral, core.InputKeys, core.InputSignal} {
		tx, calls := stubTmux(t)
		err := tx.SendInput(context.Background(), "%0", core.SessionInput{Mode: mode, Keys: ""})
		if err == nil {
			t.Fatalf("mode %q: expected validation error on empty keys", mode)
		}
		if len(*calls) != 0 {
			t.Fatalf("mode %q: runner must NOT be invoked; got %+v", mode, *calls)
		}
	}
}
