package backend

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

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
	return &Tmux{binary: p, sessionName: "gemba"}, nil
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
	args := []string{"send-keys", "-t", paneID}
	// Split off a trailing "Enter" literal so tmux interprets it as
	// the Return key, not the 5-char string.
	if strings.HasSuffix(keys, "Enter") {
		body := strings.TrimSuffix(keys, "Enter")
		if body != "" {
			args = append(args, body)
		}
		args = append(args, "Enter")
	} else {
		args = append(args, keys)
	}
	_, err := t.run(ctx, args...)
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
// injected exec.Cmd factory later (gm-native.9 startup path).
func (t *Tmux) run(ctx context.Context, args ...string) ([]byte, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, t.binary, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("tmux %s: %w (stderr=%q)",
			strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}
