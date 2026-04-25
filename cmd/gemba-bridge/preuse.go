// gm-eazw — PreToolUse enforcement (Layer 2 of gm-v8vr defense in
// depth). Reads the per-session surface from
// ~/.gemba/surfaces/<safe_session_id>.json and rejects tool calls
// whose path argument falls outside it. The decision is enforced by
// emitting Claude Code's PreToolUse "deny" response on stdout — the
// agent then surfaces a structured error to the model rather than
// firing the tool call.
//
// Layers above and below:
//
//   Layer 1 (gm-r5vz): the preamble names the surface to the model
//                      so it doesn't ask in the first place.
//   Layer 2 (this file): the bridge intercepts and denies anything
//                        that slipped through.
//   Layer 3 (gm-utik):   the container backend mounts only the
//                        surface, making out-of-bounds invisible.
//
// Failure modes are deliberately fail-open by default for tools the
// bridge doesn't recognize (the resolver wasn't designed for every
// edge case the model might invent), but fail-CLOSED for the path-
// shaped tools we DO know — Read/Edit/Write/Glob/Grep — and best-
// effort for Bash.

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/MikeBengtson/gemba/internal/persona"
)

// preToolUsePayload mirrors the relevant fields of Claude Code's
// PreToolUse hook input. Unknown fields are tolerated — Claude has
// shipped multiple revisions of this shape.
type preToolUsePayload struct {
	HookEventName string                 `json:"hook_event_name"`
	ToolName      string                 `json:"tool_name"`
	ToolInput     map[string]any         `json:"tool_input"`
	Cwd           string                 `json:"cwd"`
	SessionID     string                 `json:"session_id"`
	Extras        map[string]any         `json:"-"`
}

// claudeDenyResponse is the JSON shape Claude Code reads off stdout
// to know the tool call was blocked. Per Claude's PreToolUse hook
// contract: hookSpecificOutput.permissionDecision = "deny" plus a
// reason the agent surfaces to the model.
type claudeDenyResponse struct {
	HookSpecificOutput struct {
		HookEventName            string `json:"hookEventName"`
		PermissionDecision       string `json:"permissionDecision"`
		PermissionDecisionReason string `json:"permissionDecisionReason"`
	} `json:"hookSpecificOutput"`
}

// enforcePreToolUse decides whether a PreToolUse frame should be
// allowed. Returns (allow, reason). On allow, the bridge proceeds
// with its normal log-and-exit-0 path; on deny, the caller writes
// a Claude deny response to stdout.
//
// The function is total: it always returns a decision. A surface-
// load failure or unparseable payload defaults to allow (fail-open
// for the unknown), but every recognized path-bearing tool with an
// out-of-surface arg fails closed.
func enforcePreToolUse(payload []byte, sessionID string) (allow bool, reason string) {
	var p preToolUsePayload
	if err := json.Unmarshal(payload, &p); err != nil {
		// Malformed payload — let it through. The downstream agent
		// will hit its own validator. We don't want to block tool
		// calls because of a bridge JSON quirk.
		return true, ""
	}
	if p.ToolName == "" {
		return true, ""
	}

	surface, ok := loadSurface(sessionID)
	if !ok {
		// gm-eazw fallback (per bead): "deny outside cwd" — but for
		// that we need a cwd. Use the payload's cwd; if absent, fall
		// back to os.Getwd. If even that fails, fail-open.
		cwd := p.Cwd
		if cwd == "" {
			if c, err := os.Getwd(); err == nil {
				cwd = c
			}
		}
		if cwd == "" {
			return true, ""
		}
		surface = persona.Surface{Cwd: cwd}
		fmt.Fprintln(os.Stderr,
			"gemba-bridge: surface file missing for session, falling back to deny-outside-cwd")
	}

	envFn := os.Getenv

	// Tool-by-tool path inspection. Claude's tool_input shape varies
	// per tool; we know the load-bearing ones explicitly.
	switch p.ToolName {
	case "Read":
		path := stringField(p.ToolInput, "file_path")
		if path == "" {
			return true, ""
		}
		ok, why := surface.AllowsRead(path, envFn)
		return ok, why
	case "Edit", "Write", "MultiEdit", "NotebookEdit":
		path := stringField(p.ToolInput, "file_path")
		if path == "" {
			path = stringField(p.ToolInput, "notebook_path")
		}
		if path == "" {
			return true, ""
		}
		ok, why := surface.AllowsWrite(path, envFn)
		return ok, why
	case "Glob":
		// Glob takes an optional `path` (search root). Without one
		// it scans cwd, which is always allowed.
		path := stringField(p.ToolInput, "path")
		if path == "" {
			return true, ""
		}
		ok, why := surface.AllowsRead(path, envFn)
		return ok, why
	case "Grep":
		path := stringField(p.ToolInput, "path")
		if path == "" {
			return true, ""
		}
		ok, why := surface.AllowsRead(path, envFn)
		return ok, why
	case "Bash":
		cmd := stringField(p.ToolInput, "command")
		return inspectBash(cmd, surface, envFn)
	default:
		// Unknown tool — fail-open. Future tools (mcp__*, custom
		// agents) come and go; a bridge that breaks on every new
		// tool name would itself be a bug.
		return true, ""
	}
}

// inspectBash is a v1 shell-string scanner. It pulls path args out of
// common forms (cd, cat, ls, grep, find, awk, sed, less, head, tail,
// rm, mv, cp, touch, mkdir) and rejects on the first out-of-surface
// hit. This is deliberately not a full shell parser — gm-eazw's bead
// notes call out a robust shell-AST inspector as a follow-up.
func inspectBash(cmd string, surface persona.Surface, envFn func(string) string) (bool, string) {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return true, ""
	}
	tokens := tokenizeBash(cmd)
	if len(tokens) == 0 {
		return true, ""
	}
	// Walk tokens. The first non-flag token after a recognized verb
	// is treated as a path arg. Some verbs write (rm, mv, cp's dest,
	// touch, mkdir, sed -i); the rest read.
	for i, tok := range tokens {
		verb := strings.TrimPrefix(tok, "/usr/bin/")
		verb = filepath.Base(verb)
		intent, ok := bashVerbIntent[verb]
		if !ok {
			continue
		}
		path, found := nextPathArg(tokens[i+1:])
		if !found || path == "" {
			continue
		}
		switch intent {
		case "read":
			if ok, why := surface.AllowsRead(path, envFn); !ok {
				return false, "Bash: " + why
			}
		case "write":
			if ok, why := surface.AllowsWrite(path, envFn); !ok {
				return false, "Bash: " + why
			}
		}
	}
	return true, ""
}

// bashVerbIntent maps recognized command names to read/write intent.
// Anything not listed is skipped (fail-open for the unknown).
var bashVerbIntent = map[string]string{
	"cat": "read", "less": "read", "more": "read",
	"head": "read", "tail": "read",
	"ls": "read", "stat": "read", "file": "read",
	"grep": "read", "egrep": "read", "rg": "read", "ack": "read",
	"find": "read", "fd": "read",
	"awk": "read",
	"diff": "read",
	"cd": "read", // cd is "may we cwd here" — read is the right gate
	"cp":   "write", // dest matters more than src; accept first arg as proxy
	"mv":   "write",
	"rm":   "write",
	"touch": "write",
	"mkdir": "write",
	"sed":   "write", // -i mutates; non-i reads — treat as write conservatively
}

// nextPathArg returns the first non-flag token that looks like a
// path. Returns ("", false) if every token is a flag.
func nextPathArg(tokens []string) (string, bool) {
	for _, t := range tokens {
		if strings.HasPrefix(t, "-") {
			continue
		}
		// Skip shell metas the tokenizer kept around.
		if t == "&&" || t == "||" || t == ";" || t == "|" {
			continue
		}
		return t, true
	}
	return "", false
}

// tokenizeBash is a minimal whitespace-aware splitter that respects
// single + double quotes. Sufficient for surfacing path args from
// the verbs we recognize; not a substitute for a real shell AST.
func tokenizeBash(s string) []string {
	var out []string
	var cur strings.Builder
	quote := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
				continue
			}
			cur.WriteByte(c)
		case c == '"' || c == '\'':
			quote = c
		case c == ' ' || c == '\t':
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteByte(c)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

func stringField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

// loadSurface reads ~/.gemba/surfaces/<safe_session_id>.json. Returns
// (zero, false) on any error — caller falls back to the
// deny-outside-cwd default. The dispatcher / spawn driver writes
// this file at session-start; gm-utik / gm-r5vz round out the
// producer side.
func loadSurface(sessionID string) (persona.Surface, bool) {
	if sessionID == "" {
		return persona.Surface{}, false
	}
	path, err := surfaceFilePath(sessionID)
	if err != nil {
		return persona.Surface{}, false
	}
	fh, err := os.Open(path)
	if err != nil {
		return persona.Surface{}, false
	}
	defer fh.Close()
	raw, err := io.ReadAll(io.LimitReader(fh, 1<<20))
	if err != nil {
		return persona.Surface{}, false
	}
	var s persona.Surface
	if err := json.Unmarshal(raw, &s); err != nil {
		return persona.Surface{}, false
	}
	return s, true
}

// surfaceFilePath mirrors sessionLogPath but in ~/.gemba/surfaces/.
// Same safe-session-id rule — the producer (dispatcher) and consumer
// (this bridge) MUST use the same shape.
func surfaceFilePath(sessionID string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".gemba", "surfaces", safeSessionID(sessionID)+".json"), nil
}

// emitDeny writes Claude Code's PreToolUse deny response to w. The
// caller exits the process after; the agent reads stdout and surfaces
// the reason to the model as a tool error.
func emitDeny(w io.Writer, reason string) error {
	var resp claudeDenyResponse
	resp.HookSpecificOutput.HookEventName = "PreToolUse"
	resp.HookSpecificOutput.PermissionDecision = "deny"
	resp.HookSpecificOutput.PermissionDecisionReason = reason
	enc := json.NewEncoder(w)
	return enc.Encode(resp)
}
