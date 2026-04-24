package backend

import (
	"fmt"
	"os"
)

// Kind names a backend implementation. Auto-detect prefers tmux when
// the environment already hosts a tmux server, iTerm2 when launched
// from iTerm2 proper, Terminal.app as the fallback on macOS. Linux
// without tmux gets KindUnknown (no GUI terminal backend ships yet).
type Kind string

const (
	KindAuto     Kind = "auto"
	KindTmux     Kind = "tmux"
	KindITerm    Kind = "iterm"
	KindTerminal Kind = "terminal"
	KindUnknown  Kind = "unknown"
)

// Detect inspects the running process's environment and picks the
// best backend. Order matters: TMUX wins because it survives app
// focus changes (the operator may be in iTerm *running* tmux).
//
// The env var conventions checked here are well-documented:
//   - TMUX is set by tmux itself for every child process.
//   - TERM_PROGRAM is set by iTerm2 and Terminal.app (the values
//     differ: "iTerm.app" vs "Apple_Terminal").
func Detect() Kind {
	if os.Getenv("TMUX") != "" {
		return KindTmux
	}
	switch os.Getenv("TERM_PROGRAM") {
	case "iTerm.app":
		return KindITerm
	case "Apple_Terminal":
		return KindTerminal
	}
	return KindUnknown
}

// ResolveKind applies operator override; KindAuto triggers Detect.
// Returns an error when the override names an unknown kind so the
// server fails fast instead of mounting a backend-less adaptor.
func ResolveKind(override Kind) (Kind, error) {
	switch override {
	case "", KindAuto:
		k := Detect()
		if k == KindUnknown {
			return KindUnknown, fmt.Errorf("native/backend: could not auto-detect a terminal backend " +
				"(TMUX and TERM_PROGRAM are both unset); set --terminal=tmux|iterm|terminal explicitly")
		}
		return k, nil
	case KindTmux, KindITerm, KindTerminal:
		return override, nil
	default:
		return KindUnknown, fmt.Errorf("native/backend: unknown terminal kind %q", override)
	}
}

// Select constructs the Backend for the resolved Kind. Used by the
// native adaptor so it never directly names a backend constructor.
func Select(kind Kind) (Backend, error) {
	switch kind {
	case KindTmux:
		return NewTmux()
	case KindITerm:
		return NewITerm(), nil
	case KindTerminal:
		return NewTerminalApp(), nil
	default:
		return nil, fmt.Errorf("native/backend: cannot construct backend for kind %q", kind)
	}
}
