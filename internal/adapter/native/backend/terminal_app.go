package backend

import (
	"context"
	"fmt"
	"strings"

	"github.com/GembaCore/gemba-core/core"
)

// TerminalApp implements Backend against macOS Terminal.app via
// AppleScript (gm-native.5). Identity is "<window-id>:<tab-index>"
// because Terminal.app's dictionary lacks a stable per-tab id.
// Window ids are persistent for the life of the window; tab indices
// renumber when tabs are closed, but the GEMBA_SESSION_ID we inject
// into the shell env is what the bridge actually writes to its log,
// so the backend id only needs to be stable long enough to find the
// tab again within a single session's lifetime.
//
// Terminal.app's scripting dictionary is narrower than iTerm's:
// `contents of selected tab` returns the rendered buffer, `do script`
// runs a shell command (and spawns a new window if told to), and
// `close` dismisses a tab. There is no equivalent of iTerm's
// `unique id` — hence the composite identifier.
type TerminalApp struct{}

func NewTerminalApp() *TerminalApp { return &TerminalApp{} }

func (*TerminalApp) Name() string { return "terminal" }

// ListPanes walks every window and tab in Terminal.app.
const terminalAppListScript = `tell application "Terminal"
  set out to ""
  repeat with w in windows
    set wid to id of w as string
    set tidx to 0
    repeat with t in tabs of w
      set tidx to tidx + 1
      set cwd to ""
      try
        set cwd to (do shell script "pwd") -- best-effort; Terminal.app has no per-tab cwd accessor
      end try
      set title to (custom title of t as text)
      set out to out & wid & ":" & tidx & tab & cwd & tab & title & linefeed
    end repeat
  end repeat
  return out
end tell`

func (*TerminalApp) ListPanes(ctx context.Context) ([]Pane, error) {
	out, err := runOsascript(ctx, terminalAppListScript)
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
		panes = append(panes, Pane{
			ID:    parts[0],
			Cwd:   parts[1],
			Title: parts[2],
		})
	}
	return panes, nil
}

// SpawnPane opens a new tab in the frontmost Terminal window and
// runs the composed command. The returned Pane.ID is the
// "<wid>:<tab-index>" composite; the tab index is derived from the
// new tab's position after creation.
func (*TerminalApp) SpawnPane(ctx context.Context, spec SpawnSpec) (Pane, error) {
	if spec.Cwd == "" {
		return Pane{}, fmt.Errorf("native/backend/terminal: SpawnPane requires Cwd")
	}
	if len(spec.Command) == 0 {
		return Pane{}, fmt.Errorf("native/backend/terminal: SpawnPane requires Command")
	}
	initial := composeInitialCommand(spec)
	// `do script` with no `in window` spec opens a new window; with
	// an existing front window it appends a tab via `tell application
	// "System Events"` cmd+t keystroke. Terminal.app's dictionary
	// doesn't expose a direct "new tab" verb, so we rely on the
	// well-documented System Events fallback.
	script := fmt.Sprintf(`tell application "Terminal"
  activate
  if (count of windows) = 0 then
    do script %s
  else
    tell application "System Events" to keystroke "t" using command down
    delay 0.2
    do script %s in selected tab of front window
  end if
  set wid to id of front window as string
  set tidx to (count of tabs of front window)
  return wid & ":" & tidx
end tell`, quoteApplescriptString(initial), quoteApplescriptString(initial))
	out, err := runOsascript(ctx, script)
	if err != nil {
		return Pane{}, fmt.Errorf("native/backend/terminal: new tab: %w", err)
	}
	id := strings.TrimSpace(out)
	return Pane{ID: id, Cwd: spec.Cwd, Title: spec.Title}, nil
}

// SendKeys writes keys into the tab identified by paneID. Terminal's
// `do script in selected tab` takes a string and runs it as a shell
// command — we use this to deliver the operator's reply.
func (*TerminalApp) SendKeys(ctx context.Context, paneID, keys string) error {
	if paneID == "" {
		return fmt.Errorf("native/backend/terminal: SendKeys requires pane id")
	}
	wid, tidx, err := splitTerminalID(paneID)
	if err != nil {
		return err
	}
	// Trim the conventional trailing "Enter"; `do script` always
	// appends newline.
	keys = strings.TrimSuffix(keys, "Enter")
	script := fmt.Sprintf(`tell application "Terminal"
  do script %s in tab %d of window id %s
end tell`, quoteApplescriptString(keys), tidx, wid)
	_, err = runOsascript(ctx, script)
	return err
}

// SendInput delivers a typed SessionInput payload. gm-v01.3.1.
// Terminal.app's AppleScript surface only supports `do script` which
// appends newline, so literal/keys degrade to SendKeys; signal mode is
// unsupported (AppleScript has no terminal control-key surface).
func (ta *TerminalApp) SendInput(ctx context.Context, paneID string, in core.SessionInput) error {
	if paneID == "" {
		return core.NewAdaptorError(core.KindValidation,
			"native/backend/terminal: SendInput requires pane id")
	}
	if in.Keys == "" {
		return core.NewAdaptorError(core.KindValidation,
			"native/backend/terminal: SendInput requires non-empty Keys")
	}
	switch in.Mode {
	case core.InputLiteral, core.InputKeys:
		return ta.SendKeys(ctx, paneID, in.Keys)
	case core.InputSignal:
		return core.NewAdaptorError(core.KindUnsupported,
			"native/backend/terminal: SendInput signal mode unsupported")
	default:
		return core.NewAdaptorError(core.KindValidation,
			"native/backend/terminal: unknown SessionInputMode %q", in.Mode)
	}
}

// CapturePane returns the last `lines` of the tab's visible buffer.
// Terminal.app only exposes the rendered text, so CSI escape codes
// are already stripped.
func (*TerminalApp) CapturePane(ctx context.Context, paneID string, lines int) (string, error) {
	if paneID == "" {
		return "", fmt.Errorf("native/backend/terminal: CapturePane requires pane id")
	}
	wid, tidx, err := splitTerminalID(paneID)
	if err != nil {
		return "", err
	}
	script := fmt.Sprintf(`tell application "Terminal"
  return contents of tab %d of window id %s
end tell`, tidx, wid)
	out, err := runOsascript(ctx, script)
	if err != nil {
		return "", err
	}
	return tailLines(out, lines), nil
}

// Kill closes the tab. If it's the last tab in the window, Terminal
// closes the window too.
func (*TerminalApp) Kill(ctx context.Context, paneID string) error {
	if paneID == "" {
		return fmt.Errorf("native/backend/terminal: Kill requires pane id")
	}
	wid, tidx, err := splitTerminalID(paneID)
	if err != nil {
		return err
	}
	script := fmt.Sprintf(`tell application "Terminal"
  close tab %d of window id %s
end tell`, tidx, wid)
	_, err = runOsascript(ctx, script)
	return err
}

func splitTerminalID(id string) (windowID string, tabIndex int, err error) {
	parts := strings.SplitN(id, ":", 2)
	if len(parts) != 2 {
		return "", 0, fmt.Errorf("native/backend/terminal: invalid pane id %q (want 'WID:TABINDEX')", id)
	}
	tidx := 0
	if _, scanErr := fmt.Sscanf(parts[1], "%d", &tidx); scanErr != nil || tidx <= 0 {
		return "", 0, fmt.Errorf("native/backend/terminal: invalid tab index in %q", id)
	}
	return parts[0], tidx, nil
}
