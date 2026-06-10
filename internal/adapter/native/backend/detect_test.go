package backend

import (
	"errors"
	"strings"
	"testing"
)

func TestDetectRespectsTMUX(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-502/default,12345,0")
	t.Setenv("TERM_PROGRAM", "iTerm.app")
	if got := Detect(); got != KindTmux {
		t.Errorf("TMUX set: want KindTmux, got %q", got)
	}
}

func TestDetectITerm(t *testing.T) {
	t.Setenv("TMUX", "")
	t.Setenv("TERM_PROGRAM", "iTerm.app")
	if got := Detect(); got != KindITerm {
		t.Errorf("want KindITerm, got %q", got)
	}
}

func TestDetectTerminal(t *testing.T) {
	t.Setenv("TMUX", "")
	t.Setenv("TERM_PROGRAM", "Apple_Terminal")
	if got := Detect(); got != KindTerminal {
		t.Errorf("want KindTerminal, got %q", got)
	}
}

func TestDetectUnknownWhenBareEnv(t *testing.T) {
	t.Setenv("TMUX", "")
	t.Setenv("TERM_PROGRAM", "")
	if got := Detect(); got != KindUnknown {
		t.Errorf("bare env: want KindUnknown, got %q", got)
	}
}

func TestResolveKindExplicitOverride(t *testing.T) {
	// Explicit override wins regardless of env.
	t.Setenv("TMUX", "")
	t.Setenv("TERM_PROGRAM", "")
	got, err := ResolveKind(KindITerm)
	if err != nil {
		t.Fatalf("ResolveKind(iterm): %v", err)
	}
	if got != KindITerm {
		t.Errorf("want KindITerm, got %q", got)
	}
}

func TestResolveKindAutoFailsWithoutSignals(t *testing.T) {
	t.Setenv("TMUX", "")
	t.Setenv("TERM_PROGRAM", "")
	_, err := ResolveKind(KindAuto)
	if err == nil {
		t.Fatal("want error on undetectable auto, got nil")
	}
	if !strings.Contains(err.Error(), "auto-detect") {
		t.Errorf("error should mention auto-detect: %v", err)
	}
}

func TestResolveKindRejectsUnknownOverride(t *testing.T) {
	_, err := ResolveKind(Kind("screen"))
	if err == nil || !errors.Is(err, err) /* sanity */ {
		t.Fatalf("want error for unknown kind, got %v", err)
	}
}
