package backend

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// runOsascript executes an AppleScript snippet via `osascript -e` and
// returns stdout. AppleScript is the shared transport for iTerm2
// and Terminal.app — both expose a scripting dictionary; the
// differences are in the object model, not the invocation.
//
// Separated from the concrete backends so every osascript call goes
// through one place where timeouts, logging, and error shape stay
// consistent.
func runOsascript(ctx context.Context, script string) (string, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "osascript", "-e", script)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("osascript: %w (stderr=%q)", err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimRight(stdout.String(), "\n"), nil
}

// quoteApplescriptString wraps a string for safe interpolation into
// an AppleScript snippet. Only double-quotes and backslashes need
// escaping; AppleScript uses the usual C-style doubling.
func quoteApplescriptString(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + r.Replace(s) + `"`
}

// envExportLines converts a map of env vars into a semicolon-joined
// `export K=V` sequence suitable for prepending to a shell command.
// AppleScript's `write text` doesn't let us set process env directly,
// so we write it in-shell before the agent binary starts.
func envExportLines(env map[string]string) string {
	if len(env) == 0 {
		return ""
	}
	var parts []string
	for k, v := range env {
		parts = append(parts, fmt.Sprintf("export %s=%s", k, shellQuote(v)))
	}
	return strings.Join(parts, "; ") + "; "
}

// shellQuote wraps a value in single quotes, escaping any embedded
// single quote via the '\” idiom. The GEMBA_SESSION_ID values we
// produce don't contain quotes today, but never trust that assumption.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// composeInitialCommand builds the single-line shell command that
// AppleScript pipes into a freshly-spawned tab. We chain env exports
// + the caller's command with `&&` so a bad env quote fails visibly
// rather than silently launching without GEMBA_SESSION_ID.
func composeInitialCommand(spec SpawnSpec) string {
	var parts []string
	if spec.Cwd != "" {
		parts = append(parts, "cd "+shellQuote(spec.Cwd))
	}
	if exports := envExportLines(spec.Env); exports != "" {
		parts = append(parts, strings.TrimSuffix(exports, "; "))
	}
	if len(spec.Command) > 0 {
		cmdLine := make([]string, len(spec.Command))
		for i, a := range spec.Command {
			cmdLine[i] = shellQuote(a)
		}
		parts = append(parts, strings.Join(cmdLine, " "))
	}
	return strings.Join(parts, " && ")
}
