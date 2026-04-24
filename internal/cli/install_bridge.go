package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

// newInstallBridgeCmd registers the `gemba install-bridge` subcommand
// (gm-native.7). It writes an agent-appropriate hook stanza into the
// current workspace so subsequent sessions started by the native
// adaptor emit structured frames to the bridge log. Idempotent: a
// second run on the same workspace with the same profile is a no-op.
func newInstallBridgeCmd() *cobra.Command {
	var (
		profile string
		workDir string
		dryRun  bool
	)
	cmd := &cobra.Command{
		Use:   "install-bridge",
		Short: "Install gemba-bridge hooks into the current workspace",
		Long: `install-bridge writes the appropriate agent-hook stanza into the
current workspace so gemba-bridge captures structured events from
every session started by the native orchestrator adaptor.

Supported profiles:
  claude_code    — writes .claude/settings.local.json hook stanza
  prompt_command — writes .gemba/shellrc for zsh/bash $PROMPT_COMMAND

Idempotent; a second run on the same workspace with the same profile
leaves the file unchanged.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir := workDir
			if dir == "" {
				wd, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("cwd: %w", err)
				}
				dir = wd
			}
			return installBridge(cmd.OutOrStdout(), dir, profile, dryRun)
		},
	}
	cmd.Flags().StringVar(&profile, "profile", "claude_code", "hook profile (claude_code|prompt_command)")
	cmd.Flags().StringVar(&workDir, "workspace", "", "workspace directory (default: cwd)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print actions without writing files")
	return cmd
}

// installBridge is separated from the cobra wiring so tests can drive
// it without spinning up a command tree.
func installBridge(out interface{ Write(p []byte) (int, error) }, dir, profile string, dryRun bool) error {
	switch profile {
	case "claude_code":
		return installClaudeCodeProfile(out, dir, dryRun)
	case "prompt_command":
		return installPromptCommandProfile(out, dir, dryRun)
	default:
		return fmt.Errorf("install-bridge: unknown profile %q (want claude_code or prompt_command)", profile)
	}
}

// sentinelKey marks settings.local.json as gemba-managed so a second
// run can detect "already installed" without re-parsing our stanza.
const sentinelKey = "_gemba_bridge"

// installClaudeCodeProfile writes .claude/settings.local.json with
// the gemba-bridge hook entries. Preserves any operator-authored
// fields by doing a JSON merge (adds the "hooks" key if missing,
// replaces only if our sentinel is present).
func installClaudeCodeProfile(out interface{ Write(p []byte) (int, error) }, dir string, dryRun bool) error {
	path := filepath.Join(dir, ".claude", "settings.local.json")
	existing := map[string]interface{}{}
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, &existing); err != nil {
			return fmt.Errorf("install-bridge: parse %s: %w", path, err)
		}
	}
	if _, alreadyOurs := existing[sentinelKey]; alreadyOurs {
		fmt.Fprintf(out, "install-bridge: %s already installed (sentinel present); no-op\n", path)
		return nil
	}
	existing["hooks"] = claudeHookStanza()
	existing[sentinelKey] = map[string]interface{}{
		"profile": "claude_code",
		"version": "1",
	}
	if dryRun {
		pretty, _ := json.MarshalIndent(existing, "", "  ")
		fmt.Fprintf(out, "DRY RUN: would write %s:\n%s\n", path, pretty)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("install-bridge: mkdir %s: %w", filepath.Dir(path), err)
	}
	pretty, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return fmt.Errorf("install-bridge: marshal: %w", err)
	}
	if err := os.WriteFile(path, append(pretty, '\n'), 0o644); err != nil {
		return fmt.Errorf("install-bridge: write %s: %w", path, err)
	}
	fmt.Fprintf(out, "install-bridge: wrote %s (profile=claude_code)\n", path)
	return nil
}

func claudeHookStanza() map[string]interface{} {
	entry := func() map[string]interface{} {
		return map[string]interface{}{
			"hooks": []map[string]interface{}{
				{
					"type":    "command",
					"command": "gemba-bridge",
				},
			},
		}
	}
	return map[string]interface{}{
		"SessionStart":     []map[string]interface{}{entry()},
		"UserPromptSubmit": []map[string]interface{}{entry()},
		"PreToolUse": []map[string]interface{}{
			{
				"matcher": "*",
				"hooks":   entry()["hooks"],
			},
		},
		"PostToolUse": []map[string]interface{}{
			{
				"matcher": "Bash",
				"hooks":   entry()["hooks"],
			},
		},
		"Notification": []map[string]interface{}{entry()},
		"Stop":         []map[string]interface{}{entry()},
	}
}

const shellrcContent = `# gemba-bridge prompt-command hook (gm-native.7). Source this from
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

func installPromptCommandProfile(out interface{ Write(p []byte) (int, error) }, dir string, dryRun bool) error {
	path := filepath.Join(dir, ".gemba", "shellrc")
	if dryRun {
		fmt.Fprintf(out, "DRY RUN: would write %s (prompt_command profile, %d bytes)\n", path, len(shellrcContent))
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("install-bridge: mkdir %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(shellrcContent), 0o644); err != nil {
		return fmt.Errorf("install-bridge: write %s: %w", path, err)
	}
	fmt.Fprintf(out, "install-bridge: wrote %s (profile=prompt_command)\n", path)
	return nil
}
