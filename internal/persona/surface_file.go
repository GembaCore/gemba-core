package persona

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// SurfaceFileMode is the on-disk permission for a written surface
// file. The file may encode AdditionalReads pointing at sensitive
// paths (e.g. ~/.aws/credentials); 0o600 keeps it owner-only so a
// shared host doesn't leak the operator's allow-list to other users.
const SurfaceFileMode = os.FileMode(0o600)

// SurfaceFilePath returns the absolute path the dispatcher writes
// and the bridge reads (gm-v8vr.1). Path shape:
//
//	$HOME/.gemba/surfaces/<SafeSessionID(sessionID)>.json
//
// Both producer (this package) and consumer
// (cmd/gemba-bridge/preuse.go's loadSurface) MUST agree on the path
// AND the SafeSessionID rule. The bridge keeps a private safeSessionID
// copy that mirrors [SafeSessionID] byte-for-byte; the
// TestSafeSessionIDMatchesBridgeRule cross-check pins them.
func SurfaceFilePath(sessionID string) (string, error) {
	if sessionID == "" {
		return "", errors.New("persona: SurfaceFilePath requires non-empty sessionID")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("persona: resolve home dir: %w", err)
	}
	return filepath.Join(home, ".gemba", "surfaces", SafeSessionID(sessionID)+".json"), nil
}

// WriteSurfaceFile encodes s as JSON and atomically writes it to
// SurfaceFilePath(sessionID). The directory is created if missing.
//
// Atomic write: marshal → write to <path>.tmp → rename. A crash
// between write and rename leaves the bridge with the previous file
// (or no file) instead of a half-written one — `loadSurface` would
// JSON-decode a partial file as failure and the bridge would fall
// back to the deny-outside-cwd default, defeating the point.
func WriteSurfaceFile(sessionID string, s Surface) error {
	path, err := SurfaceFilePath(sessionID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("persona: mkdir surfaces dir: %w", err)
	}
	body, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("persona: marshal surface: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, SurfaceFileMode); err != nil {
		return fmt.Errorf("persona: write surface tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		// Best-effort cleanup of the tmp leftover; rename failure
		// already returns the original error to the caller.
		_ = os.Remove(tmp)
		return fmt.Errorf("persona: rename surface tmp: %w", err)
	}
	return nil
}

// RemoveSurfaceFile unlinks the surface file for sessionID. Missing
// file is a no-op so EndSession can call this unconditionally.
func RemoveSurfaceFile(sessionID string) error {
	path, err := SurfaceFilePath(sessionID)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("persona: remove surface file: %w", err)
	}
	return nil
}

// SafeSessionID sanitizes a session id into a filename-safe slug.
// The rule is the same one cmd/gemba-bridge/main.go's safeSessionID
// uses: ASCII letters / digits / `-._` pass through verbatim;
// everything else (including `/` and `:` from
// "<backend>:<bead>:<nanos>" session ids) becomes `_`.
//
// Both the producer (here) and consumer (bridge) MUST sanitize the
// same way — TestSafeSessionIDMatchesBridgeRule pins parity. If the
// bridge's rule changes, mirror the change here in the same commit.
func SafeSessionID(id string) string {
	out := make([]byte, 0, len(id))
	for i := 0; i < len(id); i++ {
		c := id[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
			out = append(out, c)
		case c == '-', c == '.', c == '_':
			out = append(out, c)
		default:
			out = append(out, '_')
		}
	}
	return string(out)
}
