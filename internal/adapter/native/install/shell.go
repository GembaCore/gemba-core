// shell-only installer (gm-native.18). Drops a sentinel-marked
// .gemba/shellrc that source'd from a shell rc captures bd
// invocations as PromptCommand frames for the bridge. Idempotent —
// re-running checks the sentinel.

package install

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const ShellSentinel = "# gemba-bridge:v2"

const shellrcContent = `# gemba-bridge:v2
# gemba-bridge prompt-command hook (gm-native.7 + .18). Source this from
# your shell's rc (bash, zsh) when you want bd-invocation correlation
# for a shell-only native session. Harmless outside a native session
# because GEMBA_SESSION_ID is unset.
__gemba_bridge_precmd() {
  if [ -n "${GEMBA_SESSION_ID:-}" ] && command -v gemba-bridge >/dev/null 2>&1; then
    # Pass the last command + exit status as a structured frame. We
    # stringify to JSON inline to avoid a jq dependency.
    printf '{"cmd":%q,"rc":%d}\n' "$(fc -ln -1 2>/dev/null || true)" "$?" |
      GEMBA_HOOK_EVENT=PromptCommand gemba-bridge >/dev/null 2>&1 || true
  fi
}
case "${SHELL##*/}" in
  zsh)
    precmd_functions+=(__gemba_bridge_precmd)
    ;;
  bash)
    PROMPT_COMMAND="__gemba_bridge_precmd; ${PROMPT_COMMAND:-}"
    ;;
esac
`

type shellInstaller struct{}

func NewShellOnly() Installer { return shellInstaller{} }

func (shellInstaller) Name() string { return "shell_only" }

func (shellInstaller) Install(_ context.Context, opts Options) (Report, error) {
	if opts.Dir == "" {
		return Report{Agent: "shell_only"}, fmt.Errorf("install/shell_only: Options.Dir required")
	}
	rep := Report{Agent: "shell_only"}
	path := filepath.Join(opts.Dir, ".gemba", "shellrc")
	current, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
		if opts.DryRun {
			rep.Actions = append(rep.Actions, Action{Path: path, Kind: "created", Reason: "dry-run"})
			return rep, nil
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return rep, fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(shellrcContent), 0o644); err != nil {
			return rep, fmt.Errorf("write %s: %w", path, err)
		}
		rep.Actions = append(rep.Actions, Action{Path: path, Kind: "created", Reason: "fresh install"})
		return rep, nil
	case err != nil:
		return rep, fmt.Errorf("read %s: %w", path, err)
	}
	// File present — check for our sentinel. If absent, the operator
	// owns this file and we leave it alone.
	if !strings.Contains(string(current), ShellSentinel) {
		rep.Actions = append(rep.Actions, Action{Path: path, Kind: "preserved",
			Reason: "no gemba sentinel; operator-owned shellrc"})
		return rep, nil
	}
	// Sentinel present — idempotent overwrite (we own the contents).
	if string(current) == shellrcContent {
		rep.Actions = append(rep.Actions, Action{Path: path, Kind: "skipped",
			Reason: "sentinel present + content current"})
		return rep, nil
	}
	if opts.DryRun {
		rep.Actions = append(rep.Actions, Action{Path: path, Kind: "merged", Reason: "dry-run"})
		return rep, nil
	}
	if err := os.WriteFile(path, []byte(shellrcContent), 0o644); err != nil {
		return rep, fmt.Errorf("write %s: %w", path, err)
	}
	rep.Actions = append(rep.Actions, Action{Path: path, Kind: "merged",
		Reason: "sentinel present; refreshed managed content"})
	return rep, nil
}

func init() {
	Register(NewShellOnly())
}
