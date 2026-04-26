// Package claudemd is the leaf utility that owns sentinel-bracketed
// writes to a workspace's CLAUDE.md. Both internal/adapter/native/
// preamble (bead-driven preamble) and internal/persona (consult-
// driven preamble via NativeSpawn) call into it; living at a leaf
// keeps the persona ↔ preamble dependency from cycling.
//
// Contract:
//
//   - Apply replaces (or appends, if no prior block) the gemba-
//     managed block bracketed by SentinelBegin / SentinelEnd.
//     Operator-authored content outside the sentinels is preserved
//     byte-for-byte.
//   - Remove strips the sentinel block on session end so CLAUDE.md
//     returns to its pre-spawn shape. A missing file or missing
//     sentinel pair is a no-op.
//   - Apply is idempotent: two calls with the same body leave the
//     file byte-identical.
package claudemd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// SentinelBegin marks the start of the gemba-managed block.
	// Anything before this marker (and after SentinelEnd) is
	// operator-owned and never touched.
	SentinelBegin = "<!-- gemba:preamble:begin -->"
	// SentinelEnd closes the block.
	SentinelEnd = "<!-- gemba:preamble:end -->"
)

// FileName is the on-disk filename relative to the workspace dir.
// Exposed so callers can probe / log without re-deriving it.
const FileName = "CLAUDE.md"

// Apply writes body inside a sentinel-bracketed block in
// <workspace>/CLAUDE.md. Existing content outside the sentinels is
// preserved. The directory is created if missing.
func Apply(workspace, body string) error {
	path := filepath.Join(workspace, FileName)
	existing, _ := os.ReadFile(path) // missing file → empty
	updated := replaceBlock(string(existing), body)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("claudemd: mkdir %s: %w", filepath.Dir(path), err)
	}
	return os.WriteFile(path, []byte(updated), 0o644)
}

// Remove strips the sentinel block. Missing file or missing
// sentinels are no-ops. When the strip leaves the file empty the
// file itself is removed so the workspace returns to a pre-spawn
// shape (no stub CLAUDE.md left behind).
func Remove(workspace string) error {
	path := filepath.Join(workspace, FileName)
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("claudemd: read %s: %w", path, err)
	}
	cleaned := stripBlock(string(b))
	if cleaned == string(b) {
		return nil
	}
	if strings.TrimSpace(cleaned) == "" {
		return os.Remove(path)
	}
	return os.WriteFile(path, []byte(cleaned), 0o644)
}

func replaceBlock(existing, body string) string {
	block := SentinelBegin + "\n" + strings.TrimRight(body, "\n") + "\n" + SentinelEnd + "\n"
	if !strings.Contains(existing, SentinelBegin) {
		if existing != "" && !strings.HasSuffix(existing, "\n") {
			existing += "\n"
		}
		return existing + block
	}
	return stripBlock(existing) + block
}

func stripBlock(s string) string {
	start := strings.Index(s, SentinelBegin)
	if start < 0 {
		return s
	}
	end := strings.Index(s, SentinelEnd)
	if end < 0 {
		return s
	}
	end += len(SentinelEnd)
	if end < len(s) && s[end] == '\n' {
		end++
	}
	return s[:start] + s[end:]
}
