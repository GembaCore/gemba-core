package auth

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DefaultTokenPath returns the on-disk location where the primary token hash
// lives: ~/.gemba/tokens/primary. Returns an error if the user's home
// directory cannot be resolved.
func DefaultTokenPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".gemba", "tokens", "primary"), nil
}

// ReadHash loads the argon2id PHC string from path. Returns ("", nil) if the
// file does not exist, so callers can distinguish "no token yet" from
// genuine I/O failure.
func ReadHash(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("read token hash: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

// WriteHash writes the PHC-encoded hash to path, creating parent directories
// with 0700 and the file itself with 0600. Existing file is replaced
// atomically so a concurrent reader never sees a truncated hash.
func WriteHash(path, hash string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create token dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".primary.*")
	if err != nil {
		return fmt.Errorf("create temp token file: %w", err)
	}
	tmpName := tmp.Name()
	// Best-effort cleanup if we bail before rename.
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp token file: %w", err)
	}
	if _, err := tmp.WriteString(hash + "\n"); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write token hash: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close token hash: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename token hash: %w", err)
	}
	return nil
}
