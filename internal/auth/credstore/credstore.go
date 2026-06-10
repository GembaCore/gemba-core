// Package credstore is the client-side credential store for the gemba CLI.
//
// `gemba login` writes a personal access token here after the server
// confirms it; `gemba whoami` and future CLI verbs read it back.
//
// Layout (gm-o9t8.1.1.2):
//
//	$XDG_CONFIG_HOME/gemba/credentials.json   (file mode 0600)
//	$XDG_CONFIG_HOME/gemba/                    (dir  mode 0700)
//
// $XDG_CONFIG_HOME defaults to $HOME/.config per the XDG Base Directory
// Specification. The credentials file is keyed by server URL so a single
// machine can hold credentials for multiple gemba servers without
// clobbering them.
//
// Atomicity: Put writes to a sibling temp file then renames into place
// so a kill -9 mid-write can't leave a half-written JSON document on
// disk.
//
// Threat model: the file mode (0600) is the only access control. A
// compromised user account can read the tokens. This matches the
// existing argon2id token-hash-file convention on the server side
// (~/.gemba/tokens/primary) and is acceptable for v1; future revisions
// may add OS keychain support.
package credstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Credential is one stored authentication record.
type Credential struct {
	// Token is the personal access token. Treated as opaque by the store.
	Token string `json:"token"`
	// Subject is the identity the server reported at login time.
	Subject string `json:"subject"`
	// StoredAt is the local timestamp the credential was persisted.
	StoredAt time.Time `json:"stored_at"`
}

// Store is the on-disk schema, multi-server aware from day one.
type Store struct {
	// Servers maps server URL → credential. Keys are the URLs the
	// operator passed to `gemba login --server`, normalized only by
	// the caller (no trim / no lowercase here — the CLI is the policy
	// layer).
	Servers map[string]Credential `json:"servers"`

	// path is where Put should write. Zero when constructed via
	// Empty() without a path; Load always populates it.
	path string
}

// DefaultPath returns the canonical credentials file path:
//
//	$XDG_CONFIG_HOME/gemba/credentials.json
//
// or, when XDG_CONFIG_HOME is unset, $HOME/.config/gemba/credentials.json.
//
// Errors only when neither XDG_CONFIG_HOME nor HOME is set.
func DefaultPath() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "gemba", "credentials.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", fmt.Errorf("credstore: resolve home directory: %w", err)
	}
	return filepath.Join(home, ".config", "gemba", "credentials.json"), nil
}

// Load reads the credentials file at path. A missing file is not an
// error — it returns an empty store bound to path so a subsequent Put
// creates the file. Malformed JSON IS an error so the operator notices
// rather than silently losing credentials.
func Load(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("credstore: path is required")
	}
	s := &Store{Servers: map[string]Credential{}, path: path}
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return s, nil
		}
		return nil, fmt.Errorf("credstore: read %s: %w", path, err)
	}
	if len(b) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(b, s); err != nil {
		return nil, fmt.Errorf("credstore: parse %s: %w", path, err)
	}
	if s.Servers == nil {
		s.Servers = map[string]Credential{}
	}
	s.path = path
	return s, nil
}

// Get returns the credential for server. The bool reports presence.
func (s *Store) Get(server string) (Credential, bool) {
	if s == nil || s.Servers == nil {
		return Credential{}, false
	}
	c, ok := s.Servers[server]
	return c, ok
}

// Servers returns the list of server URLs the store has credentials
// for. Order is unspecified.
func (s *Store) ServerList() []string {
	if s == nil {
		return nil
	}
	out := make([]string, 0, len(s.Servers))
	for k := range s.Servers {
		out = append(out, k)
	}
	return out
}

// Put records cred for server and persists the file atomically.
func (s *Store) Put(server string, cred Credential) error {
	if s == nil {
		return errors.New("credstore: nil store")
	}
	if server == "" {
		return errors.New("credstore: server is required")
	}
	if s.path == "" {
		return errors.New("credstore: store has no path; use Load to bind one")
	}
	if s.Servers == nil {
		s.Servers = map[string]Credential{}
	}
	s.Servers[server] = cred
	return s.write()
}

// Delete drops the credential for server and persists the file. A
// missing server is not an error — Delete is idempotent so callers
// don't need to Get-then-Delete.
func (s *Store) Delete(server string) error {
	if s == nil {
		return errors.New("credstore: nil store")
	}
	if s.path == "" {
		return errors.New("credstore: store has no path; use Load to bind one")
	}
	if _, ok := s.Servers[server]; !ok {
		return nil
	}
	delete(s.Servers, server)
	return s.write()
}

// write performs the atomic temp-file-rename persistence dance. The
// parent directory is created with mode 0700 if missing; the temp file
// is created with mode 0600 so a crashed process can't leave a
// world-readable artifact behind.
func (s *Store) write() error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("credstore: mkdir %s: %w", dir, err)
	}
	// Best-effort: tighten the dir mode in case MkdirAll found it with
	// looser perms (umask, pre-existing dir).
	_ = os.Chmod(dir, 0o700)

	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("credstore: marshal: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".credentials-*.json")
	if err != nil {
		return fmt.Errorf("credstore: create temp: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("credstore: chmod temp: %w", err)
	}
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("credstore: write temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("credstore: sync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("credstore: close temp: %w", err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		cleanup()
		return fmt.Errorf("credstore: rename %s -> %s: %w", tmpPath, s.path, err)
	}
	// Belt-and-suspenders: ensure the final file ends up 0600 even if
	// the rename clobbered an existing file with looser perms.
	if err := os.Chmod(s.path, 0o600); err != nil {
		return fmt.Errorf("credstore: chmod %s: %w", s.path, err)
	}
	return nil
}

// Path returns the file the store reads from / writes to. Exposed so
// the CLI can include the path in error messages.
func (s *Store) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}
