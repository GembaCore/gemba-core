package backend

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"github.com/GembaCore/gemba-core/core"
)

// tmuxRunner executes a tmux command. Swapped in tests so we can
// assert argv shape without a real tmux server.
type tmuxRunner func(ctx context.Context, stdin *strings.Reader, args ...string) ([]byte, error)

// signalToKey translates POSIX signal names into the tmux send-keys
// token that produces the equivalent terminal control sequence. tmux
// has no native "send signal" verb — every entry below is therefore a
// keystroke surrogate that the foreground process's tty interprets as
// the corresponding signal under default termios (`stty -a`).
//
//   - SIGINT  → "C-c"  (^C, default INTR)
//   - SIGTERM → "C-\\" (^\, default QUIT — best-effort surrogate; tmux
//     has no way to actually raise SIGTERM via a keystroke. Callers
//     that need a guaranteed SIGTERM should target the pane PID directly
//     via the OS, not through this adaptor.)
//   - SIGQUIT → "C-\\" (^\, default QUIT)
//   - SIGTSTP → "C-z"  (^Z, default SUSP)
//   - SIGHUP  → "C-d"  (^D, EOF — best-effort surrogate; shells exit on
//     EOF which is the closest tty-level effect a keystroke can produce
//     for SIGHUP. Same caveat as SIGTERM applies.)
var signalToKey = map[string]string{
	"SIGINT":  "C-c",
	"SIGTERM": `C-\`,
	"SIGQUIT": `C-\`,
	"SIGTSTP": "C-z",
	"SIGHUP":  "C-d",
}

// Tmux implements Backend by shelling to the `tmux` CLI. gm-native.4.
// Every method re-invokes tmux; there is no long-lived control
// connection to leak. Panes are identified by their tmux pane id
// (`%N` format), which is stable across `tmux` server restarts.
type Tmux struct {
	// binary is the resolved path to tmux. Empty uses "tmux" from
	// $PATH; the `New` constructor fills it in so every command is
	// reproducible in a logged context.
	binary string
	// sessionName is the tmux session new panes get attached to.
	// Empty means "create a new session on first SpawnPane."
	sessionName string
	// runner shells out to tmux. Tests inject a fake; production code
	// uses defaultTmuxRunner which forks/exec via exec.CommandContext.
	runner tmuxRunner
	// pipeMu guards activePipes. gm-v01.3.4 (Streamable extension).
	pipeMu sync.Mutex
	// activePipes records pane ids for which StartPipe has been
	// invoked. Used to make StartPipe idempotent and to short-circuit
	// StopPipe when no pipe was ever opened. Lazily initialized via
	// pipePaneState() so older constructors don't need to know about
	// the field.
	activePipes map[string]struct{}
}

// NewTmux constructs a Tmux backend. Returns an error when tmux is
// not on $PATH — the native adaptor should fall back to AppleScript
// or surface a clear config error rather than mount a half-working
// backend.
func NewTmux() (*Tmux, error) {
	p, err := exec.LookPath("tmux")
	if err != nil {
		return nil, fmt.Errorf("native/backend/tmux: tmux not found on PATH: %w", err)
	}
	t := &Tmux{binary: p, sessionName: "gemba", activePipes: make(map[string]struct{})}
	t.runner = t.defaultRunner
	return t, nil
}

// Name implements Backend.
func (*Tmux) Name() string { return "tmux" }

// listFormat is the tmux `-F` format string. Each line becomes one
// Pane. Keep the separator as a tab; pane titles can contain spaces
// and even colons so anything else risks collision.
const listFormat = "#{pane_id}\t#{pane_current_path}\t#{pane_current_command}\t#{pane_title}\t#{pane_pid}"

// ListPanes enumerates every pane across every tmux session the
// server can see. The `-a` flag covers sessions the caller isn't
// currently attached to.
func (t *Tmux) ListPanes(ctx context.Context) ([]Pane, error) {
	out, err := t.run(ctx, "list-panes", "-a", "-F", listFormat)
	if err != nil {
		return nil, err
	}
	return parsePanes(string(out))
}

func parsePanes(out string) ([]Pane, error) {
	var panes []Pane
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 5 {
			// Don't fail the whole list on one malformed line —
			// tmux occasionally emits partial output under load.
			continue
		}
		pid, _ := strconv.Atoi(parts[4])
		panes = append(panes, Pane{
			Kind:    KindTmux,
			ID:      parts[0],
			Cwd:     parts[1],
			Command: parts[2],
			Title:   parts[3],
			Pid:     pid,
		})
	}
	return panes, nil
}

// SpawnPane creates a new window in the adaptor's session (creating
// the session itself if needed) at spec.Cwd with spec.Env prepended
// and spec.Command as the initial process.
func (t *Tmux) SpawnPane(ctx context.Context, spec SpawnSpec) (Pane, error) {
	if spec.Cwd == "" {
		return Pane{}, fmt.Errorf("native/backend/tmux: SpawnPane requires Cwd")
	}
	if len(spec.Command) == 0 {
		return Pane{}, fmt.Errorf("native/backend/tmux: SpawnPane requires Command")
	}

	// Ensure the target session exists. `has-session` exits non-zero
	// when missing, which is the signal to `new-session`.
	if _, err := t.run(ctx, "has-session", "-t", t.sessionName); err != nil {
		if _, err := t.run(ctx, "new-session", "-d", "-s", t.sessionName, "-c", spec.Cwd); err != nil {
			return Pane{}, fmt.Errorf("native/backend/tmux: create session: %w", err)
		}
	}

	args := []string{
		"new-window",
		"-t", t.sessionName,
		"-c", spec.Cwd,
		"-P", // print the target of the new window so we can capture the pane id
		"-F", "#{pane_id}",
	}
	for k, v := range spec.Env {
		args = append(args, "-e", k+"="+v)
	}
	if spec.Title != "" {
		args = append(args, "-n", spec.Title)
	}
	// Command + args go at the end — tmux treats everything after the
	// flags as the shell-command to exec.
	args = append(args, spec.Command...)

	out, err := t.run(ctx, args...)
	if err != nil {
		return Pane{}, fmt.Errorf("native/backend/tmux: new-window: %w", err)
	}
	paneID := strings.TrimSpace(string(out))
	if paneID == "" {
		return Pane{}, fmt.Errorf("native/backend/tmux: new-window returned empty pane id")
	}

	// Fetch the pane metadata we need so SpawnPane returns a populated
	// Pane rather than just an id.
	panes, listErr := t.ListPanes(ctx)
	if listErr == nil {
		for _, p := range panes {
			if p.ID == paneID {
				return p, nil
			}
		}
	}
	// Fallback: return what we know.
	return Pane{Kind: KindTmux, ID: paneID, Cwd: spec.Cwd, Title: spec.Title}, nil
}

// SendKeys injects keystrokes. A trailing literal "Enter" is
// translated to the Enter key (tmux convention) — callers append
// "Enter" when they want the agent to receive a newline.
func (t *Tmux) SendKeys(ctx context.Context, paneID, keys string) error {
	if paneID == "" {
		return fmt.Errorf("native/backend/tmux: SendKeys requires pane id")
	}
	// Split off a trailing "Enter" literal so tmux interprets it as
	// the Return key, not the 5-char string.
	if strings.HasSuffix(keys, "Enter") {
		body := strings.TrimSuffix(keys, "Enter")
		if body != "" {
			if err := t.inputText(ctx, paneID, body); err != nil {
				return err
			}
		}
		_, err := t.run(ctx, "send-keys", "-t", paneID, "Enter")
		return err
	}
	return t.inputText(ctx, paneID, keys)
}

func (t *Tmux) inputText(ctx context.Context, paneID, text string) error {
	if strings.Contains(text, "\n") || len(text) > 256 {
		return t.pasteText(ctx, paneID, text)
	}
	_, err := t.run(ctx, "send-keys", "-t", paneID, text)
	return err
}

func (t *Tmux) pasteText(ctx context.Context, paneID, text string) error {
	buffer := "gemba-" + safeTmuxBufferName(paneID)
	if _, err := t.runWithStdin(ctx, strings.NewReader(text), "load-buffer", "-b", buffer, "-"); err != nil {
		return err
	}
	_, err := t.run(ctx, "paste-buffer", "-d", "-b", buffer, "-t", paneID)
	return err
}

// CapturePane returns the last `lines` of rendered pane output. The
// `-p` flag prints to stdout; `-J` joins wrapped lines; `-S -N`
// selects N lines back from the bottom.
func (t *Tmux) CapturePane(ctx context.Context, paneID string, lines int) (string, error) {
	if paneID == "" {
		return "", fmt.Errorf("native/backend/tmux: CapturePane requires pane id")
	}
	if lines <= 0 {
		lines = 200
	}
	out, err := t.run(ctx, "capture-pane", "-p", "-J", "-t", paneID, "-S", fmt.Sprintf("-%d", lines))
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// Kill force-closes a pane. tmux may close the window or the whole
// session if this was the last pane — that's expected behavior.
func (t *Tmux) Kill(ctx context.Context, paneID string) error {
	if paneID == "" {
		return fmt.Errorf("native/backend/tmux: Kill requires pane id")
	}
	_, err := t.run(ctx, "kill-pane", "-t", paneID)
	return err
}

// run is the single shell-out point so tests can swap it via an
// injected runner (gm-v01.3.1). Production callers use NewTmux which
// wires defaultRunner; tests construct a *Tmux directly and inject a
// closure that captures argv.
func (t *Tmux) run(ctx context.Context, args ...string) ([]byte, error) {
	return t.runWithStdin(ctx, nil, args...)
}

func (t *Tmux) runWithStdin(ctx context.Context, stdin *strings.Reader, args ...string) ([]byte, error) {
	runner := t.runner
	if runner == nil {
		runner = t.defaultRunner
	}
	return runner(ctx, stdin, args...)
}

// defaultRunner is the real fork/exec path. Kept as a method (not a
// free function) so the receiver's `binary` field is captured without
// an extra closure allocation.
func (t *Tmux) defaultRunner(ctx context.Context, stdin *strings.Reader, args ...string) ([]byte, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, t.binary, args...)
	if stdin != nil {
		cmd.Stdin = stdin
	}
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("tmux %s: %w (stderr=%q)",
			strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// SendInput is the typed dispatch counterpart to legacy SendKeys.
// gm-v01.3.1. Mode discriminates how `in.Keys` is delivered to the
// pane:
//
//   - core.InputLiteral — raw bytes; uses `send-keys -l --` for short
//     single-line payloads, falls through to pasteText (load-buffer +
//     paste-buffer -d) when the payload contains a newline or exceeds
//     256 bytes so quoting + control-char stripping stay safe.
//   - core.InputKeys    — caller-supplied named keys (Enter, C-c, Up,
//     M-x, …); tmux owns the name table so we pass through verbatim
//     via `send-keys --` (no -l).
//   - core.InputSignal  — POSIX signal name (SIGINT, SIGTERM, …) is
//     translated via the signalToKey map to a terminal control key
//     and delivered via the InputKeys path. Unknown signals return a
//     core.KindValidation error before any tmux invocation.
//
// Validation: empty paneID returns an error; empty Keys returns an
// error for every Mode (the no-op send is meaningless and would mask
// upstream bugs). All argv asserted by tmux_test.go::TestSendInput_*.
func (t *Tmux) SendInput(ctx context.Context, paneID string, in core.SessionInput) error {
	if paneID == "" {
		return core.NewAdaptorError(core.KindValidation,
			"native/backend/tmux: SendInput requires pane id")
	}
	if in.Keys == "" {
		return core.NewAdaptorError(core.KindValidation,
			"native/backend/tmux: SendInput requires non-empty Keys (mode=%q)", in.Mode)
	}
	switch in.Mode {
	case core.InputLiteral:
		return t.sendLiteral(ctx, paneID, in.Keys)
	case core.InputKeys:
		return t.sendNamedKeys(ctx, paneID, in.Keys)
	case core.InputSignal:
		key, ok := signalToKey[in.Keys]
		if !ok {
			return core.NewAdaptorError(core.KindValidation,
				"native/backend/tmux: signal %q unsupported", in.Keys)
		}
		return t.sendNamedKeys(ctx, paneID, key)
	default:
		return core.NewAdaptorError(core.KindValidation,
			"native/backend/tmux: unknown SessionInputMode %q", in.Mode)
	}
}

// sendLiteral routes byte-literal payloads. Short single-line payloads
// go through `send-keys -l --` so tmux treats every byte as a literal
// character (no key-name interpretation). Multi-line or large payloads
// route through pasteText where buffer-mode delivery preserves quoting.
func (t *Tmux) sendLiteral(ctx context.Context, paneID, text string) error {
	if strings.Contains(text, "\n") || len(text) > 256 {
		return t.pasteText(ctx, paneID, text)
	}
	_, err := t.run(ctx, "send-keys", "-t", paneID, "-l", "--", text)
	return err
}

// sendNamedKeys delivers caller-supplied tmux key names (Enter, C-c,
// Up, M-x, …) without -l so tmux's key-name table interprets them.
func (t *Tmux) sendNamedKeys(ctx context.Context, paneID, keys string) error {
	_, err := t.run(ctx, "send-keys", "-t", paneID, "--", keys)
	return err
}

func safeTmuxBufferName(id string) string {
	var b strings.Builder
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "pane"
	}
	return b.String()
}
