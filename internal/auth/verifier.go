package auth

import (
	"crypto/subtle"
	"os"
	"sync"
	"time"
)

// Verifier decides whether a presented bearer token is the expected
// credential. Implementations MUST use constant-time comparison and must not
// log the presented token.
type Verifier interface {
	Verify(presented string) bool
}

// PlainVerifier compares a presented token against a fixed plaintext
// reference using constant-time comparison. It exists so tests (and the
// legacy --auth=token flow that accepted a preset --auth-token value) can
// exercise the middleware without going through the hashed-file path.
type PlainVerifier struct {
	expected []byte
}

// NewPlainVerifier returns a Verifier backed by a fixed token.
func NewPlainVerifier(token string) *PlainVerifier {
	return &PlainVerifier{expected: []byte(token)}
}

// Verify returns true if presented matches the configured token.
func (p *PlainVerifier) Verify(presented string) bool {
	return subtle.ConstantTimeCompare([]byte(presented), p.expected) == 1
}

// HashVerifier checks the presented token against an argon2id hash loaded
// from a file. It re-reads the file when its mtime changes so that
// `gemba auth token rotate` takes effect without restarting the server.
//
// Rotate-while-running contract:
//   - Verify() stats the file on every call; a changed mtime triggers a
//     re-read of the hash before the comparison.
//   - If the file is deleted, subsequent Verify() calls return false.
type HashVerifier struct {
	path string

	mu    sync.RWMutex
	hash  string
	mtime time.Time
}

// NewHashVerifier returns a Verifier that reads a PHC argon2id hash from
// path on demand.
func NewHashVerifier(path string) *HashVerifier {
	return &HashVerifier{path: path}
}

// Verify returns true when the stored hash matches presented under argon2id.
// Returns false (never logging the presented token) if the hash file is
// missing, unreadable, malformed, or the hash does not match.
func (h *HashVerifier) Verify(presented string) bool {
	h.reloadIfChanged()
	h.mu.RLock()
	hash := h.hash
	h.mu.RUnlock()
	if hash == "" {
		return false
	}
	ok, err := VerifyHash(presented, hash)
	if err != nil {
		return false
	}
	return ok
}

func (h *HashVerifier) reloadIfChanged() {
	info, err := os.Stat(h.path)
	if err != nil {
		h.mu.Lock()
		h.hash = ""
		h.mtime = time.Time{}
		h.mu.Unlock()
		return
	}
	h.mu.RLock()
	unchanged := info.ModTime().Equal(h.mtime) && h.hash != ""
	h.mu.RUnlock()
	if unchanged {
		return
	}
	hash, err := ReadHash(h.path)
	if err != nil || hash == "" {
		h.mu.Lock()
		h.hash = ""
		h.mtime = info.ModTime()
		h.mu.Unlock()
		return
	}
	h.mu.Lock()
	h.hash = hash
	h.mtime = info.ModTime()
	h.mu.Unlock()
}
