package backend

import (
	"context"
	"fmt"
	"strings"
)

// ITerm implements Backend against iTerm2 via AppleScript (gm-native.5).
// The identity unit is iTerm's `session id` — unique across windows
// and tabs, stable across the app's lifetime (resets on quit).
//
// iTerm2's scripting dictionary is far richer than Terminal.app's;
// some methods that are one-line in iTerm require multi-step dances
// in Terminal.app. We keep both backends at the same API surface for
// the adaptor by using iTerm's richer affordances where possible.
type ITerm struct{}

// NewITerm constructs an iTerm2 backend. No probe — the backend may
// be selected before iTerm is running; AppleScript calls will launch
// iTerm on demand.
func NewITerm() *ITerm { return &ITerm{} }

// Name implements Backend.
func (*ITerm) Name() string { return "iterm" }

// ListPanes asks iTerm2 for every session across every window + tab.
// The AppleScript here is the multi-window, multi-tab flattening —
// return tab-separated "id<TAB>cwd<TAB>name<TAB>tty" so parsePanes
// can share format with the tmux backend.
const itermListScript = `tell application "iTerm2"
  set out to ""
  repeat with w in windows
    repeat with t in tabs of w
      repeat with s in sessions of t
        set out to out & (unique id of s) & tab & (working directory of s) & tab & (name of s) & tab & (tty of s) & linefeed
      end repeat
    end repeat
  end repeat
  return out
end tell`

func (*ITerm) ListPanes(ctx context.Context) ([]Pane, error) {
	out, err := runOsascript(ctx, itermListScript)
	if err != nil {
		return nil, err
	}
	var panes []Pane
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 3 {
			continue
		}
		p := Pane{
			ID:    parts[0],
			Cwd:   parts[1],
			Title: parts[2],
		}
		// command column is the tty path on iTerm — informational,
		// not the foreground command. Leaving Command empty signals
		// to the adaptor "unknown".
		panes = append(panes, p)
	}
	return panes, nil
}

// SpawnPane creates a new tab in the frontmost iTerm window and
// writes the composed initial command into it. If no window exists
// iTerm will open one on demand.
func (*ITerm) SpawnPane(ctx context.Context, spec SpawnSpec) (Pane, error) {
	if spec.Cwd == "" {
		return Pane{}, fmt.Errorf("native/backend/iterm: SpawnPane requires Cwd")
	}
	if len(spec.Command) == 0 {
		return Pane{}, fmt.Errorf("native/backend/iterm: SpawnPane requires Command")
	}
	initial := composeInitialCommand(spec)
	script := fmt.Sprintf(`tell application "iTerm2"
  activate
  if (count of windows) = 0 then
    create window with default profile
  end if
  tell current window
    set newTab to (create tab with default profile)
    tell current session of newTab
      write text %s
      return unique id of it
    end tell
  end tell
end tell`, quoteApplescriptString(initial))
	out, err := runOsascript(ctx, script)
	if err != nil {
		return Pane{}, fmt.Errorf("native/backend/iterm: new tab: %w", err)
	}
	id := strings.TrimSpace(out)
	return Pane{ID: id, Cwd: spec.Cwd, Title: spec.Title}, nil
}

// SendKeys writes keys into the session identified by paneID. We use
// `write text` which always appends a newline; callers that don't
// want a newline must use the tmux backend (iTerm's AppleScript
// interface doesn't expose raw key injection without newline).
func (*ITerm) SendKeys(ctx context.Context, paneID, keys string) error {
	if paneID == "" {
		return fmt.Errorf("native/backend/iterm: SendKeys requires pane id")
	}
	// Strip the conventional trailing "Enter" sentinel since write
	// text already appends newline.
	keys = strings.TrimSuffix(keys, "Enter")
	script := fmt.Sprintf(`tell application "iTerm2"
  tell session id %s
    write text %s
  end tell
end tell`, quoteApplescriptString(paneID), quoteApplescriptString(keys))
	_, err := runOsascript(ctx, script)
	return err
}

// CapturePane returns the last `lines` of the session's contents.
// iTerm2's AppleScript exposes the full buffer via `contents`; we
// slice from the tail.
func (*ITerm) CapturePane(ctx context.Context, paneID string, lines int) (string, error) {
	if paneID == "" {
		return "", fmt.Errorf("native/backend/iterm: CapturePane requires pane id")
	}
	script := fmt.Sprintf(`tell application "iTerm2"
  tell session id %s
    return contents
  end tell
end tell`, quoteApplescriptString(paneID))
	out, err := runOsascript(ctx, script)
	if err != nil {
		return "", err
	}
	if lines <= 0 {
		return out, nil
	}
	return tailLines(out, lines), nil
}

// Kill closes the iTerm session. iTerm closes the tab automatically
// when the session exits.
func (*ITerm) Kill(ctx context.Context, paneID string) error {
	if paneID == "" {
		return fmt.Errorf("native/backend/iterm: Kill requires pane id")
	}
	script := fmt.Sprintf(`tell application "iTerm2"
  tell session id %s to close
end tell`, quoteApplescriptString(paneID))
	_, err := runOsascript(ctx, script)
	return err
}

// tailLines returns the last `n` newline-delimited lines of s. Shared
// with the Terminal.app backend via this file.
func tailLines(s string, n int) string {
	if n <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}
