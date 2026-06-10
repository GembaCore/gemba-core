// Package hook implements the PreToolUse hook handlers that reject writes to
// tasks.md / todo.md in ASDD-strict projects.
//
// Design seam for gm-v0sp.21 (analyzer): when a write is rejected, the would-be
// payload is preserved on stdin (the caller can re-read it) and surfaced via
// the GEMBA_REJECTED_PAYLOAD env var hand-off so a future
// `gemba spec analyze --from-rejected` can route the payload into the
// bead-structure analyzer rather than throwing it away.
package hook

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/GembaCore/gemba-core/internal/spec/constitution"
)

// Decision is the result of evaluating a PreToolUse payload.
type Decision struct {
	Block   bool
	Reason  string
	Payload string // raw payload, available for analyzer hand-off
}

// ExitAllow is the hook exit code for "allow" (Claude Code default).
const ExitAllow = 0

// ExitBlock is the hook exit code for "hard block" — Claude Code surfaces
// stderr to the user when the hook exits with 2.
const ExitBlock = 2

const rejectionMessage = `ASDD mode: tasks.md is deprecated. Use 'gemba bead create' for ad-hoc work, or edit specs/<slug>/spec.md and run 'gemba spec reconcile'.
Future: 'gemba spec analyze --from-rejected' will route this payload into a bead-structure analyzer (gm-v0sp.21).`

// payload is the subset of Claude Code's PreToolUse payload we care about.
type payload struct {
	ToolName  string          `json:"tool_name"`
	ToolInput json.RawMessage `json:"tool_input"`
}

type writeInput struct {
	FilePath string `json:"file_path"`
}

type bashInput struct {
	Command string `json:"command"`
}

var bashTasksRe = regexp.MustCompile(`(?i)(>>?|\btee\b|\bsed\s+-i\b|\bcat\s+>>?\b)\s*[^|;]*?(tasks|todo)\.md`)

// EvaluateWriteEdit decides whether a Write/Edit/NotebookEdit payload should
// be blocked. The payload is raw JSON read from stdin.
func EvaluateWriteEdit(raw []byte, c constitution.Constitution) Decision {
	if !c.SpecStrictNoTasksMDEffective() {
		return Decision{Payload: string(raw)}
	}
	var p payload
	if err := json.Unmarshal(raw, &p); err != nil {
		return Decision{Payload: string(raw)}
	}
	var in writeInput
	_ = json.Unmarshal(p.ToolInput, &in)
	base := strings.ToLower(filepath.Base(in.FilePath))
	if base == "tasks.md" || base == "todo.md" {
		return Decision{Block: true, Reason: rejectionMessage, Payload: string(raw)}
	}
	return Decision{Payload: string(raw)}
}

// EvaluateBash decides whether a Bash payload should be blocked.
func EvaluateBash(raw []byte, c constitution.Constitution) Decision {
	if !c.SpecStrictNoTasksMDEffective() {
		return Decision{Payload: string(raw)}
	}
	var p payload
	if err := json.Unmarshal(raw, &p); err != nil {
		return Decision{Payload: string(raw)}
	}
	var in bashInput
	_ = json.Unmarshal(p.ToolInput, &in)
	if bashTasksRe.MatchString(in.Command) {
		return Decision{Block: true, Reason: rejectionMessage, Payload: string(raw)}
	}
	return Decision{Payload: string(raw)}
}

// Run dispatches the given mode against stdin and writes results to
// stderr/stdout. Returns the exit code to surface to the OS.
//
// mode is one of: "write" (Write/Edit/NotebookEdit) or "bash".
func Run(mode string, stdin io.Reader, stderr io.Writer, c constitution.Constitution) int {
	raw, err := io.ReadAll(stdin)
	if err != nil {
		fmt.Fprintf(stderr, "gemba spec hook: read stdin: %v\n", err)
		return ExitAllow
	}
	var d Decision
	switch mode {
	case "write":
		d = EvaluateWriteEdit(raw, c)
	case "bash":
		d = EvaluateBash(raw, c)
	default:
		return ExitAllow
	}
	if d.Block {
		// Hand the raw payload off via env so the future analyzer
		// (gm-v0sp.21) can ingest it without re-reading stdin.
		_ = os.Setenv("GEMBA_REJECTED_PAYLOAD", d.Payload)
		fmt.Fprintln(stderr, d.Reason)
		return ExitBlock
	}
	return ExitAllow
}
